package batchtransfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func ValidateBatchTransferSigningRequest(req BatchTransferSigningRequest) error {
	if req.Version != PreparedBatchTransferPayloadVersion || req.CircuitSetID != BatchTransferCircuitSetID {
		return fmt.Errorf("unsupported batch signing request version or circuit")
	}
	if req.CanonicalEffect == nil {
		return fmt.Errorf("structured canonical effect is required")
	}
	if req.CanonicalEffect.Creator != "" || len(req.CanonicalEffect.Proof) != 0 {
		return fmt.Errorf("structured canonical effect must exclude creator and proof")
	}
	if req.ExpiresAtUnix != req.CanonicalEffect.ExpiresAtUnix || !bytes.Equal(req.Root, req.CanonicalEffect.Root) {
		return fmt.Errorf("structured chain effect root/expiry mismatch")
	}
	if req.AuditKeyID != req.CanonicalEffect.AuditKeyId || req.AuditKeyEpoch != req.CanonicalEffect.AuditKeyEpoch || !bytes.Equal(req.AuditDisclosureTargetPubKey, req.CanonicalEffect.AuditDisclosureTargetPubkey) {
		return fmt.Errorf("structured audit identity mismatch")
	}
	canonical, err := privacytypes.CanonicalMsgBatchTransferPayloadBytesV1(req.CanonicalEffect)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, req.CanonicalPayload) {
		return fmt.Errorf("canonical payload mismatch")
	}
	if len(req.OrderedInputs) != len(req.CanonicalEffect.Nullifiers) || len(req.OrderedInputNullifiers) != len(req.CanonicalEffect.Nullifiers) || len(req.OrderedOutputs) != len(req.CanonicalEffect.Outputs) {
		return fmt.Errorf("structured effect count mismatch")
	}
	if req.AssetID == nil || req.AssetID.Sign() <= 0 || req.InputTotal == nil || req.InputTotal.Sign() < 0 {
		return fmt.Errorf("structured common asset/input total is invalid")
	}
	for _, field := range []struct {
		name  string
		value *big.Int
	}{
		{"structured asset id", req.AssetID},
		{"structured nullifier root", req.NullifierRoot},
		{"structured commitment root", req.CommitmentRoot},
		{"structured user disclosure root", req.UserDisclosureRoot},
		{"structured full disclosure root", req.FullDisclosureRoot},
		{"structured payload digest high", req.PayloadDigestHi},
		{"structured payload digest low", req.PayloadDigestLo},
		{"structured expected intent", req.ExpectedIntent},
	} {
		if err := validateCanonicalBatchTransferField(field.name, field.value); err != nil {
			return err
		}
	}
	ownerSpend, err := privacycrypto.DecodeCanonicalPoint(req.OwnerSpendPubKey)
	if err != nil {
		return fmt.Errorf("structured owner spend key: %w", err)
	}
	ownerView, err := privacycrypto.DecodeCanonicalPoint(req.OwnerViewPubKey)
	if err != nil {
		return fmt.Errorf("structured owner view key: %w", err)
	}
	ownerSpendX, ownerSpendY, err := privacycrypto.PublicPointFieldValues(*ownerSpend)
	if err != nil {
		return err
	}
	ownerViewX, ownerViewY, err := privacycrypto.PublicPointFieldValues(*ownerView)
	if err != nil {
		return err
	}
	computedInputTotal := new(big.Int)
	var computedInputTotalNative privacyamount.Amount128
	inputRandomness := make([]privacycrypto.FieldValue, len(req.OrderedInputs))
	for i := range req.OrderedInputNullifiers {
		input := req.OrderedInputs[i]
		if !bytes.Equal(input.Nullifier, req.OrderedInputNullifiers[i]) || !bytes.Equal(req.OrderedInputNullifiers[i], req.CanonicalEffect.Nullifiers[i]) {
			return fmt.Errorf("ordered nullifier %d mismatch", i)
		}
		if !bytes.Equal(input.SpendPubKey, req.OwnerSpendPubKey) || !bytes.Equal(input.ViewPubKey, req.OwnerViewPubKey) || !fixedMatchesPublic(input.AssetID, req.AssetID) {
			return fmt.Errorf("structured input %d owner/amount/asset is invalid", i)
		}
		inputNote := privacytypes.SecretNoteV1{ReceiverSpendPubKeyX: ownerSpendX, ReceiverSpendPubKeyY: ownerSpendY, ReceiverViewPubKeyX: ownerViewX, ReceiverViewPubKeyY: ownerViewY, Amount: input.Amount, AssetID: input.AssetID, Randomness: input.Randomness}
		commitment, err := inputNote.CommitmentV1()
		if err != nil {
			return err
		}
		if !bytes.Equal(fixedBytes(commitment), input.Commitment) {
			return fmt.Errorf("structured input %d commitment mismatch", i)
		}
		nullifier, err := inputNote.NullifierV1()
		if err != nil {
			return err
		}
		if !bytes.Equal(fixedBytes(nullifier), input.Nullifier) {
			return fmt.Errorf("structured input %d nullifier recomputation mismatch", i)
		}
		inputRandomness[i] = input.Randomness
		nextTotal, err := computedInputTotalNative.Add(input.Amount)
		if err != nil {
			return fmt.Errorf("operation total exceeds uint128: %w", err)
		}
		computedInputTotalNative = nextTotal
		computedInputTotal = privacytypes.Amount128BigInt(nextTotal)
	}
	if computedInputTotal.Cmp(req.InputTotal) != 0 {
		return fmt.Errorf("structured input total mismatch")
	}
	outputSecrets := make([]batchTransferOutputSecrets, len(req.OrderedOutputs))
	for i := range req.OrderedOutputs {
		outputSecrets[i] = batchTransferOutputSecrets{
			Randomness:             req.OrderedOutputs[i].Randomness,
			UserDisclosureBlinding: req.OrderedOutputs[i].UserDisclosureBlinding,
			FullDisclosureBlinding: req.OrderedOutputs[i].FullDisclosureBlinding,
			PrivacyPolicy:          req.OrderedOutputs[i].PrivacyPolicy,
		}
	}
	if err := validateBatchTransferGlobalSecretReuse(inputRandomness, outputSecrets); err != nil {
		return err
	}
	computedOutputTotal := new(big.Int)
	var computedOutputTotalNative privacyamount.Amount128
	seenPayment := false
	seenChange := false
	seenPadding := false
	ownerTemplate := privacytypes.SecretNoteV1{ReceiverSpendPubKeyX: ownerSpendX, ReceiverSpendPubKeyY: ownerSpendY, ReceiverViewPubKeyX: ownerViewX, ReceiverViewPubKeyY: ownerViewY, AssetID: req.OrderedInputs[0].AssetID}
	for i := range req.OrderedOutputs {
		o := req.OrderedOutputs[i]
		wire := req.CanonicalEffect.Outputs[i]
		if o.WireOutput == nil || !batchWireOutputsEqual(o.WireOutput, wire) || !bytes.Equal(o.Commitment, wire.Commitment) || o.PrivacyPolicy != wire.UserPrivacyPolicy || o.DisclosureMode != wire.UserDisclosureMode {
			return fmt.Errorf("structured output %d mismatch", i)
		}
		if !fixedMatchesPublic(o.AssetID, req.AssetID) {
			return fmt.Errorf("structured output %d amount/asset mismatch", i)
		}
		recipientSpend, err := privacycrypto.DecodeCanonicalPoint(o.RecipientSpendPubKey)
		if err != nil {
			return fmt.Errorf("structured output %d spend key: %w", i, err)
		}
		recipientView, err := privacycrypto.DecodeCanonicalPoint(o.RecipientViewPubKey)
		if err != nil {
			return fmt.Errorf("structured output %d view key: %w", i, err)
		}
		recipientSpendX, recipientSpendY, err := privacycrypto.PublicPointFieldValues(*recipientSpend)
		if err != nil {
			return err
		}
		recipientViewX, recipientViewY, err := privacycrypto.PublicPointFieldValues(*recipientView)
		if err != nil {
			return err
		}
		outputNote := privacytypes.SecretNoteV1{ReceiverSpendPubKeyX: recipientSpendX, ReceiverSpendPubKeyY: recipientSpendY, ReceiverViewPubKeyX: recipientViewX, ReceiverViewPubKeyY: recipientViewY, Amount: o.Amount, AssetID: o.AssetID, Randomness: o.Randomness}
		commitment, err := outputNote.CommitmentV1()
		if err != nil {
			return err
		}
		if !bytes.Equal(fixedBytes(commitment), o.Commitment) {
			return fmt.Errorf("structured output %d commitment recomputation mismatch", i)
		}

		_, userDigest, err := disclosurePlaintext(uint32(i), false, ownerTemplate, PreparedBatchTransferOutput{Kind: o.Kind, Note: outputNote, PrivacyPolicy: o.PrivacyPolicy, DisclosureMode: o.DisclosureMode, UserDisclosureBlinding: o.UserDisclosureBlinding, FullDisclosureBlinding: o.FullDisclosureBlinding})
		if err != nil {
			return err
		}
		_, fullDigest, err := disclosurePlaintext(uint32(i), true, ownerTemplate, PreparedBatchTransferOutput{Kind: o.Kind, Note: outputNote, PrivacyPolicy: o.PrivacyPolicy, DisclosureMode: o.DisclosureMode, UserDisclosureBlinding: o.UserDisclosureBlinding, FullDisclosureBlinding: o.FullDisclosureBlinding})
		if err != nil {
			return err
		}
		if o.PrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
			if !o.UserDisclosureBlinding.IsZero() || len(wire.UserDisclosureDigest) != 0 {
				return fmt.Errorf("structured all-private output %d must use user disclosure sentinels", i)
			}
		} else if !bytes.Equal(fieldBytes(userDigest), wire.UserDisclosureDigest) {
			return fmt.Errorf("structured output %d user disclosure digest mismatch", i)
		}
		if !bytes.Equal(fieldBytes(fullDigest), wire.FullDisclosureDigest) {
			return fmt.Errorf("structured output %d full disclosure digest mismatch", i)
		}
		switch o.Kind {
		case OutputPayment:
			if seenChange || seenPadding || o.Amount.IsZero() {
				return fmt.Errorf("payment outputs must be a positive canonical prefix")
			}
			seenPayment = true
		case OutputChange:
			if seenChange || seenPadding || o.Amount.IsZero() || o.PrivacyPolicy != 0 || !bytes.Equal(o.RecipientSpendPubKey, req.OwnerSpendPubKey) || !bytes.Equal(o.RecipientViewPubKey, req.OwnerViewPubKey) {
				return fmt.Errorf("change output is not canonical")
			}
			seenChange = true
		case OutputPadding:
			if !o.Amount.IsZero() || o.PrivacyPolicy != 0 || !bytes.Equal(o.RecipientSpendPubKey, req.OwnerSpendPubKey) || !bytes.Equal(o.RecipientViewPubKey, req.OwnerViewPubKey) {
				return fmt.Errorf("padding output is not canonical")
			}
			seenPadding = true
		default:
			return fmt.Errorf("unsupported structured output kind %q", o.Kind)
		}
		nextTotal, err := computedOutputTotalNative.Add(o.Amount)
		if err != nil {
			return fmt.Errorf("operation total exceeds uint128: %w", err)
		}
		computedOutputTotalNative = nextTotal
		computedOutputTotal = privacytypes.Amount128BigInt(nextTotal)
		selfViewPresent := len(wire.SelfViewDisclosurePayload) > 0
		if selfViewPresent != req.SelfViewEnabled {
			return fmt.Errorf("structured self-view all-or-none mismatch at output %d", i)
		}
	}
	if !seenPayment {
		return fmt.Errorf("structured batch must contain at least one payment output")
	}
	if computedOutputTotal.Cmp(computedInputTotal) != 0 {
		return fmt.Errorf("structured input/output conservation mismatch")
	}
	digest, err := privacytypes.ComputeMsgBatchTransferPayloadDigestV1(req.CanonicalEffect)
	if err != nil {
		return err
	}
	if digest.Hi.Cmp(req.PayloadDigestHi) != 0 || digest.Lo.Cmp(req.PayloadDigestLo) != 0 {
		return fmt.Errorf("payload digest mismatch")
	}
	nulls := zeroVector(16)
	for i, bz := range req.OrderedInputNullifiers {
		nulls[i].SetBytes(bz)
	}
	commitments, users, fulls, policies := zeroVector(32), zeroVector(32), zeroVector(32), make([]uint32, 32)
	for i, out := range req.CanonicalEffect.Outputs {
		commitments[i].SetBytes(out.Commitment)
		fulls[i].SetBytes(out.FullDisclosureDigest)
		users[i].SetBytes(out.UserDisclosureDigest)
		policies[i] = out.UserPrivacyPolicy
	}
	nr, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorNullifierV1, uint32(len(req.OrderedInputNullifiers)), nulls)
	if err != nil {
		return err
	}
	cr, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorCommitmentV1, uint32(len(req.OrderedOutputs)), commitments)
	if err != nil {
		return err
	}
	ur, err := privacytypes.ComputeBatchUserDisclosureVectorRootV1(uint32(len(req.OrderedOutputs)), policies, users)
	if err != nil {
		return err
	}
	fr, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorFullDisclosureV1, uint32(len(req.OrderedOutputs)), fulls)
	if err != nil {
		return err
	}
	if nr.Cmp(req.NullifierRoot) != 0 || cr.Cmp(req.CommitmentRoot) != 0 || ur.Cmp(req.UserDisclosureRoot) != 0 || fr.Cmp(req.FullDisclosureRoot) != 0 {
		return fmt.Errorf("aggregate root mismatch")
	}
	chain, err := privacytypes.ComputeChainDomainV1(req.ChainID, privacytypes.ActiveCircuitSetID)
	if err != nil {
		return err
	}
	intent, err := privacytypes.ComputeBatchTransferIntentV1(privacytypes.BatchTransferIntentV1Input{ChainDomainHi: chain.Hi, ChainDomainLo: chain.Lo, MerkleRoot: new(big.Int).SetBytes(req.Root), InputCount: uint32(len(req.OrderedInputNullifiers)), OutputCount: uint32(len(req.OrderedOutputs)), AssetID: req.AssetID, NullifierRoot: nr, CommitmentRoot: cr, UserDisclosureRoot: ur, FullDisclosureRoot: fr, PayloadDigestHi: digest.Hi, PayloadDigestLo: digest.Lo, ExpiresAtUnix: req.ExpiresAtUnix})
	if err != nil {
		return err
	}
	if intent.Cmp(req.ExpectedIntent) != 0 {
		return fmt.Errorf("expected batch intent mismatch")
	}
	return nil
}

func batchWireOutputsEqual(left, right *privacytypes.BatchTransferOutput) bool {
	if left == nil || right == nil {
		return left == right
	}
	return bytes.Equal(left.Commitment, right.Commitment) &&
		bytes.Equal(left.Ciphertext, right.Ciphertext) &&
		bytes.Equal(left.ViewTag, right.ViewTag) &&
		left.UserPrivacyPolicy == right.UserPrivacyPolicy &&
		left.UserDisclosureMode == right.UserDisclosureMode &&
		bytes.Equal(left.UserDisclosureDigest, right.UserDisclosureDigest) &&
		bytes.Equal(left.UserDisclosureTargetPubkey, right.UserDisclosureTargetPubkey) &&
		bytes.Equal(left.UserDisclosurePayload, right.UserDisclosurePayload) &&
		bytes.Equal(left.FullDisclosureDigest, right.FullDisclosureDigest) &&
		bytes.Equal(left.AuditDisclosurePayload, right.AuditDisclosurePayload) &&
		bytes.Equal(left.SelfViewDisclosurePayload, right.SelfViewDisclosurePayload)
}

func ValidatePreparedBatchTransferPayloadMetadataAt(p *PreparedBatchTransferPayload, now time.Time) error {
	if p == nil {
		return fmt.Errorf("prepared batch payload is required")
	}
	if p.Version != PreparedBatchTransferPayloadVersion || p.CircuitSetID != BatchTransferCircuitSetID {
		return fmt.Errorf("unsupported prepared batch payload version or circuit")
	}
	if !now.IsZero() && p.ExpiresAtUnix <= now.Unix() {
		return fmt.Errorf("prepared batch payload has expired")
	}
	if err := privacytypes.ValidateBatchJoinSplitCountsV1(uint32(len(p.Inputs)), uint32(len(p.Outputs))); err != nil {
		return err
	}
	if len(p.MessageOutputs) != len(p.Outputs) {
		return fmt.Errorf("message output count mismatch")
	}
	for i, output := range p.MessageOutputs {
		if output == nil {
			return fmt.Errorf("message output %d is required", i)
		}
	}
	if err := privacyfield.ValidateCanonicalBytes32(p.Root); err != nil {
		return fmt.Errorf("prepared batch root: %w", err)
	}
	if p.AssetID == nil || p.AssetID.Sign() <= 0 {
		return fmt.Errorf("prepared batch asset id must be positive")
	}
	for _, field := range []struct {
		name  string
		value *big.Int
	}{
		{"prepared batch asset id", p.AssetID},
		{"prepared batch nullifier root", p.NullifierRoot},
		{"prepared batch commitment root", p.CommitmentRoot},
		{"prepared batch user disclosure root", p.UserDisclosureRoot},
		{"prepared batch full disclosure root", p.FullDisclosureRoot},
		{"prepared batch payload digest high", p.PayloadDigestHi},
		{"prepared batch payload digest low", p.PayloadDigestLo},
		{"prepared batch expected intent", p.ExpectedIntent},
	} {
		if err := validateCanonicalBatchTransferField(field.name, field.value); err != nil {
			return err
		}
	}
	inputTotal := new(big.Int)
	var inputTotalNative privacyamount.Amount128
	inputRandomness := make([]privacycrypto.FieldValue, len(p.Inputs))
	for i, in := range p.Inputs {
		if err := in.Note.ValidateV1(); err != nil {
			return fmt.Errorf("input %d: %w", i, err)
		}
		n, err := in.Note.NullifierV1()
		if err != nil {
			return err
		}
		if !bytes.Equal(fixedBytes(n), in.Nullifier) {
			return fmt.Errorf("input %d nullifier mismatch", i)
		}
		if err := privacyfield.ValidateCanonicalBytes32(in.Nullifier); err != nil {
			return fmt.Errorf("input %d nullifier: %w", i, err)
		}
		if len(in.MerklePath) != 32 || len(in.MerklePath) != len(in.MerklePathHelper) {
			return fmt.Errorf("input %d path shape mismatch", i)
		}
		c, err := in.Note.CommitmentV1()
		if err != nil {
			return err
		}
		cur := fixedPublicBig(c)
		for level, raw := range in.MerklePath {
			if len(raw) != 64 || raw != strings.ToLower(raw) {
				return fmt.Errorf("input %d path %d is not canonical 32-byte lowercase hex", i, level)
			}
			siblingBytes, err := hex.DecodeString(raw)
			if err != nil || privacyfield.ValidateCanonicalBytes32(siblingBytes) != nil {
				return fmt.Errorf("input %d path %d is not a canonical field element", i, level)
			}
			sibling := new(big.Int).SetBytes(siblingBytes)
			bit := in.MerklePathHelper[level]
			if bit > 1 {
				return fmt.Errorf("input %d helper %d is not a bit", i, level)
			}
			if bit == 0 {
				cur = privacytypes.ComputeNoteTreeNodeV1(uint32(level), cur, sibling)
			} else {
				cur = privacytypes.ComputeNoteTreeNodeV1(uint32(level), sibling, cur)
			}
		}
		if cur.Cmp(new(big.Int).SetBytes(p.Root)) != 0 {
			return fmt.Errorf("input %d root mismatch", i)
		}
		if !fixedMatchesPublic(in.Note.AssetID, p.AssetID) {
			return fmt.Errorf("input %d asset mismatch", i)
		}
		if i > 0 && (!sameOwner(p.Inputs[0].Note, in.Note)) {
			return fmt.Errorf("input %d owner mismatch", i)
		}
		nextTotal, err := inputTotalNative.Add(in.Note.Amount)
		if err != nil {
			return fmt.Errorf("operation total exceeds uint128: %w", err)
		}
		inputTotalNative = nextTotal
		inputTotal = privacytypes.Amount128BigInt(nextTotal)
		inputRandomness[i] = in.Note.Randomness
	}
	outputSecrets := make([]batchTransferOutputSecrets, len(p.Outputs))
	for i := range p.Outputs {
		outputSecrets[i] = batchTransferOutputSecrets{
			Randomness:             p.Outputs[i].Note.Randomness,
			UserDisclosureBlinding: p.Outputs[i].UserDisclosureBlinding,
			FullDisclosureBlinding: p.Outputs[i].FullDisclosureBlinding,
			PrivacyPolicy:          p.Outputs[i].PrivacyPolicy,
		}
	}
	if err := validateBatchTransferGlobalSecretReuse(inputRandomness, outputSecrets); err != nil {
		return err
	}
	outputTotal := new(big.Int)
	var outputTotalNative privacyamount.Amount128
	for i, out := range p.Outputs {
		if err := out.Note.ValidateV1(); err != nil {
			return fmt.Errorf("output %d: %w", i, err)
		}
		c, err := out.Note.CommitmentV1()
		if err != nil {
			return err
		}
		if !bytes.Equal(fixedBytes(c), p.MessageOutputs[i].Commitment) {
			return fmt.Errorf("output %d commitment mismatch", i)
		}
		if !fixedMatchesPublic(out.Note.AssetID, p.AssetID) {
			return fmt.Errorf("output %d asset mismatch", i)
		}
		userPlain, userDigest, err := disclosurePlaintext(uint32(i), false, p.Inputs[0].Note, out)
		if err != nil {
			return err
		}
		_, fullDigest, err := disclosurePlaintext(uint32(i), true, p.Inputs[0].Note, out)
		if err != nil {
			return err
		}
		if out.PrivacyPolicy == 0 {
			if len(p.MessageOutputs[i].UserDisclosureDigest) != 0 {
				return fmt.Errorf("output %d all-private user digest must be empty", i)
			}
		} else if !bytes.Equal(fieldBytes(userDigest), p.MessageOutputs[i].UserDisclosureDigest) {
			return fmt.Errorf("output %d user disclosure digest mismatch", i)
		}
		if !bytes.Equal(fieldBytes(fullDigest), p.MessageOutputs[i].FullDisclosureDigest) {
			return fmt.Errorf("output %d full disclosure digest mismatch", i)
		}
		if out.DisclosureMode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC && !bytes.Equal(userPlain, p.MessageOutputs[i].UserDisclosurePayload) {
			return fmt.Errorf("output %d public disclosure plaintext mismatch", i)
		}
		nextTotal, err := outputTotalNative.Add(out.Note.Amount)
		if err != nil {
			return fmt.Errorf("operation total exceeds uint128: %w", err)
		}
		outputTotalNative = nextTotal
		outputTotal = privacytypes.Amount128BigInt(nextTotal)
	}
	if inputTotal.Cmp(outputTotal) != 0 {
		return fmt.Errorf("prepared batch input/output conservation mismatch")
	}
	if err := privacytypes.ValidateMsgBatchTransferEffectsV1(p.effectMessage(nil, "")); err != nil {
		return err
	}
	canonical, err := privacytypes.CanonicalMsgBatchTransferPayloadBytesV1(p.effectMessage(nil, ""))
	if err != nil {
		return err
	}
	req, err := signingRequest(p, canonical)
	if err != nil {
		return err
	}
	if err := ValidateBatchTransferSigningRequest(req); err != nil {
		return err
	}
	if _, err := privacycrypto.DecodeCanonicalEdDSASignature(p.OwnerSignature); err != nil {
		return err
	}
	ownerKey, err := fixedPoint(p.Inputs[0].Note.ReceiverSpendPubKeyX, p.Inputs[0].Note.ReceiverSpendPubKeyY)
	if err != nil {
		return err
	}
	pubBytes := ownerKey.Bytes()
	pub, err := privacycrypto.DecodeCanonicalPoint(pubBytes[:])
	if err != nil {
		return err
	}
	nativePub := cryptoeddsa.PublicKey{A: *pub}
	message := p.ExpectedIntent.FillBytes(make([]byte, 32))
	valid, err := nativePub.Verify(p.OwnerSignature, message, mimc.NewMiMC())
	if err != nil || !valid {
		return fmt.Errorf("owner signature does not authorize the canonical batch intent")
	}
	wantHash, err := computePayloadHash(p)
	if err != nil {
		return err
	}
	if wantHash != p.PayloadHash {
		return fmt.Errorf("prepared batch payload hash mismatch")
	}
	return nil
}

type batchTransferOutputSecrets struct {
	Randomness, UserDisclosureBlinding, FullDisclosureBlinding privacycrypto.FieldValue
	PrivacyPolicy                                              uint32
}

func validateBatchTransferGlobalSecretReuse(inputs []privacycrypto.FieldValue, outputs []batchTransferOutputSecrets) error {
	seen := make([]privacycrypto.FieldValue, 0, len(inputs)+3*len(outputs))
	register := func(value privacycrypto.FieldValue, label string, nonzero bool) error {
		if nonzero && value.IsZero() {
			return fmt.Errorf("%s must be non-zero", label)
		}
		duplicate := false
		for _, old := range seen {
			equal := old.Equal(value)
			duplicate = equal || duplicate
		}
		if duplicate {
			return fmt.Errorf("%s reuses a previous secret; randomness and disclosure blindings must be fresh and independent", label)
		}
		seen = append(seen, value)
		return nil
	}
	for i, value := range inputs {
		if err := register(value, fmt.Sprintf("input %d randomness", i), false); err != nil {
			return err
		}
	}
	for i, o := range outputs {
		if err := register(o.Randomness, fmt.Sprintf("output %d randomness", i), true); err != nil {
			return err
		}
		if err := register(o.FullDisclosureBlinding, fmt.Sprintf("output %d full disclosure blinding", i), true); err != nil {
			return err
		}
		if o.PrivacyPolicy != 0 {
			if err := register(o.UserDisclosureBlinding, fmt.Sprintf("output %d user disclosure blinding", i), true); err != nil {
				return err
			}
		} else if !o.UserDisclosureBlinding.IsZero() {
			return fmt.Errorf("output %d all-private user blinding must use the zero sentinel", i)
		}
	}
	return nil
}

func zeroVector(n int) []*big.Int {
	out := make([]*big.Int, n)
	for i := range out {
		out[i] = new(big.Int)
	}
	return out
}

func validateCanonicalBatchTransferField(name string, value *big.Int) error {
	if _, err := privacyfield.CanonicalBytesFromBigInt(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func sameOwner(a, b privacytypes.SecretNoteV1) bool {
	return a.ReceiverSpendPubKeyX.Equal(b.ReceiverSpendPubKeyX) && a.ReceiverSpendPubKeyY.Equal(b.ReceiverSpendPubKeyY) && a.ReceiverViewPubKeyX.Equal(b.ReceiverViewPubKeyX) && a.ReceiverViewPubKeyY.Equal(b.ReceiverViewPubKeyY)
}

func digestCanonicalPayload(canonical []byte) (*big.Int, *big.Int) {
	sum := sha256.Sum256(append([]byte(privacytypes.BatchTransferPayloadV1ByteDomain), canonical...))
	return new(big.Int).SetBytes(sum[:16]), new(big.Int).SetBytes(sum[16:])
}
