package batchtransfer

import (
	"bytes"
	"fmt"
	"math/big"
	"time"

	privacycircuit "github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
)

// PrepareAuditV2 creates the bounded 1..16/1..32 v2 final PI/envelope.
func PrepareAuditV2(input privacyaudit.PrepareInput) (*privacyaudit.Prepared, error) {
	if input.Kind != auditfield.KindBatch16x32 || len(input.Inputs) == 0 || len(input.Inputs) > 16 || len(input.Outputs) == 0 || len(input.Outputs) > 32 || input.Recipient != "" || input.Amount != "" || len(input.Principal) != 0 {
		return nil, fmt.Errorf("batch v2 requires 1..16 inputs and 1..32 outputs without a transparent principal")
	}
	return privacyaudit.Prepare(input)
}

// PrepareAuditV2FromPreparedPayload derives the audit plaintext from the
// normal batch selection/output payload. It preserves the original 1..16
// inputs, 1..32 outputs, recovery ciphertexts and disclosure values instead
// of accepting a parallel manually assembled witness.
func PrepareAuditV2FromPreparedPayload(snapshot privacyaudit.Snapshot, payload *PreparedBatchTransferPayload) (*privacyaudit.Prepared, error) {
	if err := ValidatePreparedBatchTransferPayloadMetadata(payload); err != nil {
		return nil, err
	}
	outputs, err := AuditV2OutputEffectsFromPreparedPayload(payload)
	if err != nil {
		return nil, err
	}
	plain, err := auditPlainFromPreparedBatchPayload(payload)
	if err != nil {
		return nil, err
	}
	inputs := make([][]byte, len(payload.Inputs))
	for i := range payload.Inputs {
		inputs[i] = bytes.Clone(payload.Inputs[i].Nullifier)
	}
	return PrepareAuditV2(privacyaudit.PrepareInput{
		Snapshot: snapshot, Kind: auditfield.KindBatch16x32, Creator: payload.Creator, ExpiresAtUnix: payload.ExpiresAtUnix,
		Root: bytes.Clone(payload.Root), Inputs: inputs, Outputs: outputs, Plaintext: plain,
	})
}

// PrepareAuditV2FromPreparedAuditV2Payload is the resumable v2 path. The
// stage payload supplies only normal note material and output effects; audit
// encryption randomness remains in Prepared until the current prove attempt.
func PrepareAuditV2FromPreparedAuditV2Payload(snapshot privacyaudit.Snapshot, payload *PreparedAuditV2BatchTransferPayload) (*privacyaudit.Prepared, error) {
	if err := ValidatePreparedAuditV2BatchTransferPayloadAt(payload, time.Time{}); err != nil {
		return nil, err
	}
	plain, err := auditPlainFromPreparedAuditV2BatchPayload(payload)
	if err != nil {
		return nil, err
	}
	inputs := make([][]byte, len(payload.Inputs))
	for i := range payload.Inputs {
		inputs[i] = bytes.Clone(payload.Inputs[i].Nullifier)
	}
	return PrepareAuditV2(privacyaudit.PrepareInput{Snapshot: snapshot, Kind: auditfield.KindBatch16x32, Creator: payload.Creator, ExpiresAtUnix: payload.ExpiresAtUnix, Root: bytes.Clone(payload.Root), Inputs: inputs, Outputs: payload.Effects, Plaintext: plain})
}

func BuildAuditV2Message(prepared *privacyaudit.Prepared, proof []byte) (*privacyv2.MsgBatchTransfer, error) {
	message, err := prepared.BuildMessage(proof)
	if err != nil {
		return nil, err
	}
	batch, ok := message.(*privacyv2.MsgBatchTransfer)
	if !ok {
		return nil, fmt.Errorf("prepared message is not a v2 batch transfer")
	}
	return batch, nil
}

// BuildAuditV2Witness reuses the normal 16x32 plan, note recovery output and
// disclosure blinding preparation in payload. Its owner signature is replaced
// only after the prepared audit envelope has fixed the final intent.
func BuildAuditV2Witness(prepared *privacyaudit.Prepared, payload *PreparedBatchTransferPayload, sign func(*big.Int) ([]byte, error)) (witness.Witness, error) {
	if prepared == nil || prepared.Kind() != auditfield.KindBatch16x32 || payload == nil || sign == nil {
		return nil, fmt.Errorf("batch audit preparation, payload and signer are required")
	}
	outputs, err := AuditV2OutputEffectsFromPreparedPayload(payload)
	if err != nil {
		return nil, err
	}
	if !equalAuditV2BatchOutputEffects(prepared.OutputEffects(), outputs) {
		return nil, fmt.Errorf("prepared batch outputs do not match final audit PI")
	}
	old, err := buildAssignment(payload, payload.OwnerSignature)
	if err != nil {
		return nil, err
	}
	public, random, err := prepared.CircuitBinding()
	if err != nil {
		return nil, err
	}
	assignment := &privacycircuit.BatchJoinSplitAuditFieldV1{
		Public: public, Random: random, AssetID: old.AssetID, OwnerSpendPubKey: old.OwnerSpendPubKey, OwnerViewPubKey: old.OwnerViewPubKey,
		InputAmounts: old.InputAmounts, InputRandomness: old.InputRandomness, InputSpendPubKeys: old.InputSpendPubKeys, InputViewPubKeys: old.InputViewPubKeys,
		InputPaths: old.InputPaths, InputPathHelpers: old.InputPathHelpers, OutputAmounts: old.OutputAmounts, OutputRandomness: old.OutputRandomness,
		OutputSpendPubKeys: old.OutputSpendPubKeys, OutputViewPubKeys: old.OutputViewPubKeys, OutputPrivacyPolicies: old.OutputPrivacyPolicies,
		UserDisclosureBlindings: old.UserDisclosureBlindings, FullDisclosureBlindings: old.FullDisclosureBlindings,
	}
	intent, err := prepared.OwnerIntent()
	if err != nil {
		return nil, err
	}
	signature, err := sign(new(big.Int).SetBytes(intent[:]))
	if err != nil {
		return nil, err
	}
	if err := assignSig(&assignment.OwnerSignature, signature); err != nil {
		return nil, err
	}
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField())
}

// BuildAuditV2WitnessFromPayload is the v2 stage-file counterpart. The
// temporary legacy-shaped value exists only to reuse the normal circuit
// assignment routine; it is never marshalled and carries no retired audit
// fields or ciphertext.
func BuildAuditV2WitnessFromPayload(prepared *privacyaudit.Prepared, payload *PreparedAuditV2BatchTransferPayload, sign func(*big.Int) ([]byte, error)) (witness.Witness, error) {
	if prepared == nil || prepared.Kind() != auditfield.KindBatch16x32 || payload == nil || sign == nil {
		return nil, fmt.Errorf("batch audit preparation, v2 payload and signer are required")
	}
	if err := ValidatePreparedAuditV2BatchTransferPayloadAt(payload, time.Time{}); err != nil {
		return nil, err
	}
	if !equalAuditV2BatchOutputEffects(prepared.OutputEffects(), payload.Effects) {
		return nil, fmt.Errorf("prepared batch outputs do not match final audit PI")
	}
	old, err := buildAssignment(&PreparedBatchTransferPayload{ChainID: payload.ChainID, Root: bytes.Clone(payload.Root), AssetID: new(big.Int).Set(payload.AssetID), Inputs: payload.Inputs, Outputs: payload.Outputs}, nil)
	if err != nil {
		return nil, err
	}
	public, random, err := prepared.CircuitBinding()
	if err != nil {
		return nil, err
	}
	assignment := &privacycircuit.BatchJoinSplitAuditFieldV1{
		Public: public, Random: random, AssetID: old.AssetID, OwnerSpendPubKey: old.OwnerSpendPubKey, OwnerViewPubKey: old.OwnerViewPubKey,
		InputAmounts: old.InputAmounts, InputRandomness: old.InputRandomness, InputSpendPubKeys: old.InputSpendPubKeys, InputViewPubKeys: old.InputViewPubKeys,
		InputPaths: old.InputPaths, InputPathHelpers: old.InputPathHelpers, OutputAmounts: old.OutputAmounts, OutputRandomness: old.OutputRandomness,
		OutputSpendPubKeys: old.OutputSpendPubKeys, OutputViewPubKeys: old.OutputViewPubKeys, OutputPrivacyPolicies: old.OutputPrivacyPolicies,
		UserDisclosureBlindings: old.UserDisclosureBlindings, FullDisclosureBlindings: old.FullDisclosureBlindings,
	}
	intent, err := prepared.OwnerIntent()
	if err != nil {
		return nil, err
	}
	signature, err := sign(new(big.Int).SetBytes(intent[:]))
	if err != nil {
		return nil, err
	}
	if err := assignSig(&assignment.OwnerSignature, signature); err != nil {
		return nil, err
	}
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField())
}

// AuditV2OutputEffectsFromPreparedPayload keeps normal batch recovery and
// disclosure wire values byte-for-byte when forming the v2 effect list.
func AuditV2OutputEffectsFromPreparedPayload(payload *PreparedBatchTransferPayload) ([]*privacyv2.OutputEffect, error) {
	if err := ValidatePreparedBatchTransferPayloadMetadata(payload); err != nil {
		return nil, err
	}
	outputs := make([]*privacyv2.OutputEffect, len(payload.MessageOutputs))
	for i, output := range payload.MessageOutputs {
		outputs[i] = &privacyv2.OutputEffect{
			Commitment: bytes.Clone(output.Commitment), Ciphertext: bytes.Clone(output.Ciphertext), ViewTag: bytes.Clone(output.ViewTag),
			UserPrivacyPolicy: output.UserPrivacyPolicy, UserDisclosureMode: uint32(output.UserDisclosureMode),
			UserDisclosureDigest: bytes.Clone(output.UserDisclosureDigest), UserDisclosureTargetPubkey: bytes.Clone(output.UserDisclosureTargetPubkey),
			UserDisclosurePayload: bytes.Clone(output.UserDisclosurePayload), SelfFullDisclosureDigest: bytes.Clone(output.FullDisclosureDigest),
			SelfViewDisclosurePayload: bytes.Clone(output.SelfViewDisclosurePayload),
		}
	}
	return outputs, nil
}

func auditPlainFromPreparedBatchPayload(payload *PreparedBatchTransferPayload) ([]auditfield.Field32, error) {
	asset, err := batchAuditFieldFromBig(payload.AssetID)
	if err != nil {
		return nil, err
	}
	plain := []auditfield.Field32{asset}
	for i := 0; i < int(privacytypes.BatchJoinSplitV1MaxInputs); i++ {
		field := auditfield.Field32{}
		if i < len(payload.Inputs) {
			commitment, err := payload.Inputs[i].Note.CommitmentV1()
			if err != nil {
				return nil, err
			}
			field, err = batchAuditFieldFromValue(commitment)
			if err != nil {
				return nil, err
			}
		}
		plain = append(plain, field)
	}
	for i := 0; i < int(privacytypes.BatchJoinSplitV1MaxOutputs); i++ {
		if i >= len(payload.Outputs) {
			plain = append(plain, auditfield.Field32{}, auditfield.Field32{}, auditfield.Field32{}, auditfield.Field32{}, auditfield.Field32{})
			continue
		}
		note := payload.Outputs[i].Note
		for _, value := range []privacycrypto.FieldValue{privacycrypto.FieldValueFromUint64(note.Amount), note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY, note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY} {
			field, err := batchAuditFieldFromValue(value)
			if err != nil {
				return nil, err
			}
			plain = append(plain, field)
		}
	}
	return plain, nil
}

func auditPlainFromPreparedAuditV2BatchPayload(payload *PreparedAuditV2BatchTransferPayload) ([]auditfield.Field32, error) {
	asset, err := batchAuditFieldFromBig(payload.AssetID)
	if err != nil {
		return nil, err
	}
	plain := []auditfield.Field32{asset}
	for i := 0; i < int(privacytypes.BatchJoinSplitV1MaxInputs); i++ {
		field := auditfield.Field32{}
		if i < len(payload.Inputs) {
			commitment, err := payload.Inputs[i].Note.CommitmentV1()
			if err != nil {
				return nil, err
			}
			field, err = batchAuditFieldFromValue(commitment)
			if err != nil {
				return nil, err
			}
		}
		plain = append(plain, field)
	}
	for i := 0; i < int(privacytypes.BatchJoinSplitV1MaxOutputs); i++ {
		if i >= len(payload.Outputs) {
			plain = append(plain, auditfield.Field32{}, auditfield.Field32{}, auditfield.Field32{}, auditfield.Field32{}, auditfield.Field32{})
			continue
		}
		note := payload.Outputs[i].Note
		for _, value := range []privacycrypto.FieldValue{privacycrypto.FieldValueFromUint64(note.Amount), note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY, note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY} {
			field, err := batchAuditFieldFromValue(value)
			if err != nil {
				return nil, err
			}
			plain = append(plain, field)
		}
	}
	return plain, nil
}

func batchAuditFieldFromValue(value privacycrypto.FieldValue) (auditfield.Field32, error) {
	raw := value.Bytes()
	return auditfield.ParseField32(raw[:])
}

func batchAuditFieldFromBig(value *big.Int) (auditfield.Field32, error) {
	raw, err := privacyfield.CanonicalBytesFromBigInt(value)
	if err != nil {
		return auditfield.Field32{}, err
	}
	return auditfield.ParseField32(raw)
}

func equalAuditV2BatchOutputEffects(left, right []*privacyv2.OutputEffect) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] == nil || right[i] == nil ||
			!bytes.Equal(left[i].Commitment, right[i].Commitment) || !bytes.Equal(left[i].Ciphertext, right[i].Ciphertext) || !bytes.Equal(left[i].ViewTag, right[i].ViewTag) ||
			left[i].UserPrivacyPolicy != right[i].UserPrivacyPolicy || left[i].UserDisclosureMode != right[i].UserDisclosureMode ||
			!bytes.Equal(left[i].UserDisclosureDigest, right[i].UserDisclosureDigest) || !bytes.Equal(left[i].UserDisclosureTargetPubkey, right[i].UserDisclosureTargetPubkey) ||
			!bytes.Equal(left[i].UserDisclosurePayload, right[i].UserDisclosurePayload) || !bytes.Equal(left[i].SelfFullDisclosureDigest, right[i].SelfFullDisclosureDigest) || !bytes.Equal(left[i].SelfViewDisclosurePayload, right[i].SelfViewDisclosurePayload) {
			return false
		}
	}
	return true
}
