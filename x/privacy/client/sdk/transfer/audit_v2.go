package transfer

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	privacycircuit "github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
)

// AuditV2DisclosureConfig contains only the disclosure planes that remain on
// the v2 wire.  In particular it has no legacy audit public key or ciphertext:
// the audit envelope is constructed by audit.Prepared after the final PI is
// fixed.
type AuditV2DisclosureConfig struct {
	UserPrivacyPolicy              uint32
	UserDisclosureMode             privacytypes.UserDisclosureMode
	UserDisclosureTargetPubKey     *crypto_tedwards.PointAffine
	UserDisclosureTargetPubKeyBz   []byte
	DisableSelfViewDisclosure      bool
	SelfViewDisclosureTargetPubKey *crypto_tedwards.PointAffine
}

// PrepareAuditV2Transfer reuses normal note selection, Merkle paths, output
// recovery and disclosure blindings without constructing a legacy audit
// ciphertext.  It deliberately returns no serializable v2 preparation: the
// audit encryption witness remains in audit.Prepared until proving.
func PrepareAuditV2Transfer(
	ctx context.Context,
	merklePaths MerklePathProvider,
	snapshot privacyaudit.Snapshot,
	creator string,
	expiresAtUnix int64,
	input PrepareJoinSplitInput,
	denom string,
	disclosure AuditV2DisclosureConfig,
	sign func(*big.Int) ([]byte, error),
) (*privacyaudit.Prepared, witness.Witness, error) {
	if sign == nil {
		return nil, nil, fmt.Errorf("audit transfer owner signer is required")
	}
	normal, err := PrepareJoinSplitTransfer(ctx, merklePaths, input)
	if err != nil {
		return nil, nil, err
	}
	userBlinding, fullBlinding, err := generateFixedTransferDisclosureBlindings(normal.RecipientNote.Randomness, disclosure.UserPrivacyPolicy)
	if err != nil {
		return nil, nil, fmt.Errorf("transfer disclosure blindings: %w", err)
	}
	disclosureInput := DisclosureBuildInput{
		OutputCommitment: normal.OutputCommitments[0], TransferDenom: denom, FromNote: normal.FromNote, RecipientNote: normal.RecipientNote,
		UserDisclosureBlinding: userBlinding, FullDisclosureBlinding: fullBlinding,
	}
	user, err := BuildUserDisclosureData(disclosureInput, disclosure.UserPrivacyPolicy, disclosure.UserDisclosureMode, disclosure.UserDisclosureTargetPubKey)
	if err != nil {
		return nil, nil, err
	}
	fullDigest, err := fixedTransferDigest(disclosureInput, privacytypes.DisclosureFullMarkerV1)
	if err != nil {
		return nil, nil, err
	}
	var selfView *DisclosureData
	if !disclosure.DisableSelfViewDisclosure {
		selfView, err = BuildSelfViewDisclosureData(disclosureInput, disclosure.SelfViewDisclosureTargetPubKey)
		if err != nil {
			return nil, nil, err
		}
	}
	ciphertexts, viewTags, err := EncryptOutputNotesWithViewTags(normal.RecipientNote, normal.ChangeNote, normal.OutputCommitments)
	if err != nil {
		return nil, nil, err
	}
	outputs := []*privacyv2.OutputEffect{
		{Commitment: bytes.Clone(normal.OutputCommitments[0]), Ciphertext: bytes.Clone(ciphertexts[0]), ViewTag: bytes.Clone(viewTags[0]),
			UserPrivacyPolicy: disclosure.UserPrivacyPolicy, UserDisclosureMode: uint32(disclosure.UserDisclosureMode),
			UserDisclosureDigest: digestBytes(user), UserDisclosureTargetPubkey: encodedDisclosureTargetBytes(disclosure.UserDisclosureTargetPubKey, disclosure.UserDisclosureTargetPubKeyBz),
			UserDisclosurePayload: cipherTextBytes(user), SelfFullDisclosureDigest: bytes.Clone(fullDigest), SelfViewDisclosurePayload: cipherTextBytes(selfView)},
		{Commitment: bytes.Clone(normal.OutputCommitments[1]), Ciphertext: bytes.Clone(ciphertexts[1]), ViewTag: bytes.Clone(viewTags[1])},
	}
	plain, err := auditPlainFromPreparedNormal(normal)
	if err != nil {
		return nil, nil, err
	}
	prepared, err := PrepareAuditV2(privacyaudit.PrepareInput{
		Snapshot: snapshot, Kind: auditfield.KindTransfer2x2, Creator: creator, ExpiresAtUnix: expiresAtUnix,
		Root: normal.CommonRoot, Inputs: normal.InputNullifiers, Outputs: outputs, Plaintext: plain,
	})
	if err != nil {
		return nil, nil, err
	}
	normal.Assignment.UserPrivacyPolicy = new(big.Int).SetUint64(uint64(disclosure.UserPrivacyPolicy))
	normal.Assignment.UserDisclosureDigest = new(big.Int).SetBytes(digestBytes(user))
	normal.Assignment.FullDisclosureDigest = new(big.Int).SetBytes(fullDigest)
	userBlindingBytes := userBlinding.Bytes()
	fullBlindingBytes := fullBlinding.Bytes()
	normal.Assignment.UserDisclosureBlinding = new(big.Int).SetBytes(userBlindingBytes[:])
	normal.Assignment.FullDisclosureBlinding = new(big.Int).SetBytes(fullBlindingBytes[:])
	full, err := BuildAuditV2Witness(prepared, normal, outputs, sign)
	if err != nil {
		prepared.Clear()
		return nil, nil, err
	}
	return prepared, full, nil
}

func auditPlainFromPreparedNormal(normal *PreparedJoinSplitTransfer) ([]auditfield.Field32, error) {
	if normal == nil {
		return nil, fmt.Errorf("normal transfer preparation is required")
	}
	asset, err := auditFieldFromValue(normal.FromNote.AssetID)
	if err != nil {
		return nil, err
	}
	plain := []auditfield.Field32{asset}
	for _, note := range normal.InputNotes {
		commitment, err := note.CommitmentV1()
		if err != nil {
			return nil, err
		}
		commitmentBytes := commitment.Bytes()
		field, err := auditfield.ParseField32(commitmentBytes[:])
		if err != nil {
			return nil, err
		}
		plain = append(plain, field)
	}
	for _, note := range []privacytypes.SecretNoteV1{normal.RecipientNote, normal.ChangeNote} {
		for _, value := range []privacycrypto.FieldValue{privacycrypto.FieldValueFromUint64(note.Amount), note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY, note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY} {
			field, err := auditFieldFromValue(value)
			if err != nil {
				return nil, err
			}
			plain = append(plain, field)
		}
	}
	return plain, nil
}

// PrepareAuditV2 creates the two-input/two-output v2 PI/envelope and final
// owner-intent boundary. Call OwnerIntent before signing and proving.
func PrepareAuditV2(input privacyaudit.PrepareInput) (*privacyaudit.Prepared, error) {
	if input.Kind != auditfield.KindTransfer2x2 || len(input.Inputs) != 2 || len(input.Outputs) != 2 || input.Recipient != "" || input.Amount != "" || len(input.Principal) != 0 {
		return nil, fmt.Errorf("transfer v2 requires exactly two inputs and outputs with no transparent principal")
	}
	return privacyaudit.Prepare(input)
}

// PrepareAuditV2FromPreparedPayload is the normal-flow bridge for a v2
// transfer. It consumes the existing selected notes, paths and encrypted
// payload as the only witness source; it does not synthesize an initial note
// or a parallel prover payload.
func PrepareAuditV2FromPreparedPayload(snapshot privacyaudit.Snapshot, payload *PreparedTransferPayload) (*privacyaudit.Prepared, error) {
	if payload == nil {
		return nil, fmt.Errorf("prepared transfer payload is required")
	}
	if err := ValidatePreparedTransferPayloadMetadata(*payload); err != nil {
		return nil, err
	}
	effect, err := payload.transferEffectMessage(nil)
	if err != nil {
		return nil, err
	}
	outputs, err := AuditV2OutputEffectsFromPreparedPayload(*payload)
	if err != nil {
		return nil, err
	}
	plain, err := auditPlainFromPreparedPayload(*payload)
	if err != nil {
		return nil, err
	}
	return PrepareAuditV2(privacyaudit.PrepareInput{
		Snapshot: snapshot, Kind: auditfield.KindTransfer2x2, Creator: payload.Creator, ExpiresAtUnix: payload.ExpiresAtUnix,
		Root: effect.Root, Inputs: effect.Nullifiers, Outputs: outputs, Plaintext: plain,
	})
}

func BuildAuditV2Message(prepared *privacyaudit.Prepared, proof []byte) (*privacyv2.MsgTransfer, error) {
	message, err := prepared.BuildMessage(proof)
	if err != nil {
		return nil, err
	}
	transfer, ok := message.(*privacyv2.MsgTransfer)
	if !ok {
		return nil, fmt.Errorf("prepared message is not a v2 transfer")
	}
	return transfer, nil
}

// BuildAuditV2Witness reuses a normal two-note selection/output preparation.
// The final owner signature is produced only after the audit envelope/root is
// fixed, replacing the old payload intent in the audit-field relation.
func BuildAuditV2Witness(prepared *privacyaudit.Prepared, legacy *PreparedJoinSplitTransfer, outputs []*privacyv2.OutputEffect, sign func(*big.Int) ([]byte, error)) (witness.Witness, error) {
	if prepared == nil || prepared.Kind() != auditfield.KindTransfer2x2 || legacy == nil || len(outputs) != 2 || outputs[0] == nil || outputs[1] == nil || sign == nil {
		return nil, fmt.Errorf("transfer audit preparation, two outputs and signer are required")
	}
	return buildAuditV2Witness(prepared, &legacy.Assignment, outputs, sign)
}

// BuildAuditV2WitnessFromPreparedPayload is the normal transfer entrypoint
// for the audit-field circuit. It reuses the persisted selection, merkle
// paths, encrypted outputs and disclosure blindings produced by the existing
// transfer payload flow. The legacy signature in that payload is deliberately
// not reused: the final audit envelope has a fresh nonce/root, so the owner
// signature is obtained only after that PI has been fixed.
func BuildAuditV2WitnessFromPreparedPayload(prepared *privacyaudit.Prepared, payload *PreparedTransferPayload, sign func(*big.Int) ([]byte, error)) (witness.Witness, error) {
	if prepared == nil || prepared.Kind() != auditfield.KindTransfer2x2 || payload == nil || sign == nil {
		return nil, fmt.Errorf("transfer audit preparation, payload and signer are required")
	}
	outputs, err := AuditV2OutputEffectsFromPreparedPayload(*payload)
	if err != nil {
		return nil, err
	}
	if !equalAuditV2OutputEffects(prepared.OutputEffects(), outputs) {
		return nil, fmt.Errorf("prepared transfer outputs do not match final audit PI")
	}
	legacy, err := buildJoinSplitAssignmentFromPreparedTransferPayload(*payload)
	if err != nil {
		return nil, err
	}
	return buildAuditV2Witness(prepared, legacy, outputs, sign)
}

// AuditV2OutputEffectsFromPreparedPayload translates the already-encrypted
// normal-flow output message to its v2 representation. Disclosure fields are
// carried on output zero in both the 2x2 relation and v2 wire schema.
func AuditV2OutputEffectsFromPreparedPayload(payload PreparedTransferPayload) ([]*privacyv2.OutputEffect, error) {
	effect, err := payload.transferEffectMessage(nil)
	if err != nil {
		return nil, err
	}
	if len(effect.NewCommitments) != 2 || len(effect.CipherTexts) != 2 || len(effect.ViewTags) != 2 {
		return nil, fmt.Errorf("normal transfer payload requires exactly two output effects")
	}
	outputs := []*privacyv2.OutputEffect{
		{
			Commitment: bytes.Clone(effect.NewCommitments[0]), Ciphertext: bytes.Clone(effect.CipherTexts[0]), ViewTag: bytes.Clone(effect.ViewTags[0]),
			UserPrivacyPolicy: effect.UserPrivacyPolicy, UserDisclosureMode: uint32(effect.UserDisclosureMode),
			UserDisclosureDigest: bytes.Clone(effect.UserDisclosureDigest), UserDisclosureTargetPubkey: bytes.Clone(effect.UserDisclosureTargetPubkey),
			UserDisclosurePayload: bytes.Clone(effect.UserDisclosurePayload), SelfFullDisclosureDigest: bytes.Clone(effect.AuditDisclosureDigest),
			SelfViewDisclosurePayload: bytes.Clone(effect.SelfViewDisclosurePayload),
		},
		{
			Commitment: bytes.Clone(effect.NewCommitments[1]), Ciphertext: bytes.Clone(effect.CipherTexts[1]), ViewTag: bytes.Clone(effect.ViewTags[1]),
		},
	}
	return outputs, nil
}

func auditPlainFromPreparedPayload(payload PreparedTransferPayload) ([]auditfield.Field32, error) {
	assetRaw, err := decodePayloadField(payload.AssetIDHex, "asset id")
	if err != nil {
		return nil, err
	}
	asset, err := auditfield.ParseField32(assetRaw)
	if err != nil {
		return nil, err
	}
	assetValue, err := privacycrypto.ParseFieldValueBE32(assetRaw)
	if err != nil {
		return nil, err
	}
	plain := []auditfield.Field32{asset}
	for _, input := range payload.Inputs {
		note, err := auditNoteFromPayload(input.Amount, input.RandomnessHex, input.SpendPubKeyHex, input.ViewPubKeyHex, assetValue)
		if err != nil {
			return nil, err
		}
		commitment, err := note.CommitmentV1()
		if err != nil {
			return nil, err
		}
		field, err := auditFieldFromValue(commitment)
		if err != nil {
			return nil, err
		}
		plain = append(plain, field)
	}
	for _, output := range payload.Outputs {
		note, err := auditNoteFromPayload(output.Amount, output.RandomnessHex, output.SpendPubKeyHex, output.ViewPubKeyHex, assetValue)
		if err != nil {
			return nil, err
		}
		plain, err = appendAuditNoteRecipientFields(plain, note)
		if err != nil {
			return nil, err
		}
	}
	return plain, nil
}

func auditNoteFromPayload(amountText, randomnessHex, spendPubKeyHex, viewPubKeyHex string, asset privacycrypto.FieldValue) (privacytypes.SecretNoteV1, error) {
	amount, err := parseDecimalField(amountText, "note amount")
	if err != nil || !amount.IsUint64() {
		return privacytypes.SecretNoteV1{}, fmt.Errorf("invalid note amount")
	}
	randomness, err := decodeSecretPayloadField(randomnessHex, "note randomness")
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	spend, err := decodePublicKeyHex(spendPubKeyHex, "note spend pubkey")
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	view, err := decodePublicKeyHex(viewPubKeyHex, "note view pubkey")
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	spendX, spendY, err := privacycrypto.PublicPointFieldValues(*spend)
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	viewX, viewY, err := privacycrypto.PublicPointFieldValues(*view)
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	note, err := privacytypes.NewSecretNoteV1(spendX, spendY, viewX, viewY, amount.Uint64(), asset, randomness, "")
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	return *note, nil
}

func appendAuditNoteRecipientFields(plain []auditfield.Field32, note privacytypes.SecretNoteV1) ([]auditfield.Field32, error) {
	for _, value := range []privacycrypto.FieldValue{privacycrypto.FieldValueFromUint64(note.Amount), note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY, note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY} {
		field, err := auditFieldFromValue(value)
		if err != nil {
			return nil, err
		}
		plain = append(plain, field)
	}
	return plain, nil
}

func auditFieldFromValue(value privacycrypto.FieldValue) (auditfield.Field32, error) {
	raw := value.Bytes()
	return auditfield.ParseField32(raw[:])
}

func buildAuditV2Witness(prepared *privacyaudit.Prepared, old *privacycircuit.JoinSplitCircuit, outputs []*privacyv2.OutputEffect, sign func(*big.Int) ([]byte, error)) (witness.Witness, error) {
	if old == nil {
		return nil, fmt.Errorf("normal transfer witness is required")
	}
	public, random, err := prepared.CircuitBinding()
	if err != nil {
		return nil, err
	}
	userDigest, err := outputField(outputs[0].UserDisclosureDigest, true)
	if err != nil {
		return nil, err
	}
	fullDigest, err := outputField(outputs[0].SelfFullDisclosureDigest, false)
	if err != nil {
		return nil, err
	}
	assignment := &privacycircuit.JoinSplitAuditFieldV1{
		Public: public, Random: random, Nullifiers: old.Nullifiers, Commitments: old.Commitments,
		UserPrivacyPolicy: outputs[0].UserPrivacyPolicy, UserDisclosureDigest: userDigest, FullDisclosureDigest: fullDigest,
		AssetID: old.AssetID, InputAmounts: old.InputAmounts, InputRandomness: old.InputRandomness, InputPaths: old.InputPaths,
		InputPathHelpers: old.InputPathHelpers, InputSpendPubKeys: old.InputSpendPubKeys, InputViewPubKeys: old.InputViewPubKeys,
		OutputAmounts: old.OutputAmounts, OutputRandomness: old.OutputRandomness, OutputSpendPubKeys: old.OutputSpendPubKeys, OutputViewPubKeys: old.OutputViewPubKeys,
		UserDisclosureBlinding: old.UserDisclosureBlinding, FullDisclosureBlinding: old.FullDisclosureBlinding,
	}
	intent, err := prepared.OwnerIntent()
	if err != nil {
		return nil, err
	}
	signature, err := sign(new(big.Int).SetBytes(intent[:]))
	if err != nil {
		return nil, err
	}
	if err := assignSignature(&assignment.OwnerSignature, signature); err != nil {
		return nil, err
	}
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField())
}

func equalAuditV2OutputEffects(left, right []*privacyv2.OutputEffect) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] == nil || right[i] == nil ||
			!bytes.Equal(left[i].Commitment, right[i].Commitment) || !bytes.Equal(left[i].Ciphertext, right[i].Ciphertext) || !bytes.Equal(left[i].ViewTag, right[i].ViewTag) ||
			left[i].UserPrivacyPolicy != right[i].UserPrivacyPolicy || left[i].UserDisclosureMode != right[i].UserDisclosureMode ||
			!bytes.Equal(left[i].UserDisclosureDigest, right[i].UserDisclosureDigest) || !bytes.Equal(left[i].UserDisclosureTargetPubkey, right[i].UserDisclosureTargetPubkey) ||
			!bytes.Equal(left[i].UserDisclosurePayload, right[i].UserDisclosurePayload) || !bytes.Equal(left[i].SelfFullDisclosureDigest, right[i].SelfFullDisclosureDigest) ||
			!bytes.Equal(left[i].SelfViewDisclosurePayload, right[i].SelfViewDisclosurePayload) {
			return false
		}
	}
	return true
}

func outputField(raw []byte, allowEmpty bool) (*big.Int, error) {
	if len(raw) == 0 && allowEmpty {
		return big.NewInt(0), nil
	}
	value, err := auditfield.ParseField32(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid transfer output audit field")
	}
	return new(big.Int).SetBytes(value[:]), nil
}
