package types

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const (
	BatchTransferPayloadV1ByteDomain             = "clairveil.batch-transfer-payload.v1"
	CanonicalBatchTransferPayloadFormatVersionV1 = uint32(1)
	BatchTransferProofSizeV1                     = 164

	// MaxBatchTransferMessageBytesV1 is a consensus framing limit. The frozen
	// fixed-field 16/32 maximum is about 65 KiB, so 128 KiB leaves roughly 2x
	// headroom while remaining far below the chain's 1 MiB transaction limit.
	MaxBatchTransferMessageBytesV1 = 128 << 10
)

// ValidateMsgBatchTransferFramingV1 performs only bounded, length-based checks
// suitable for the keeper's cheap precharge path. It deliberately does not
// decode canonical points or envelopes, recompute public disclosure digests,
// or run any cryptographic validation.
func ValidateMsgBatchTransferFramingV1(msg *MsgBatchTransfer) error {
	if msg == nil {
		return fmt.Errorf("batch transfer message is required")
	}
	if len(msg.Nullifiers) == 0 || len(msg.Nullifiers) > int(BatchJoinSplitV1MaxInputs) {
		return fmt.Errorf("input count must be in 1..%d", BatchJoinSplitV1MaxInputs)
	}
	if len(msg.Outputs) == 0 || len(msg.Outputs) > int(BatchJoinSplitV1MaxOutputs) {
		return fmt.Errorf("output count must be in 1..%d", BatchJoinSplitV1MaxOutputs)
	}
	if len(msg.Proof) != BatchTransferProofSizeV1 {
		return fmt.Errorf("batch proof must be exactly %d bytes", BatchTransferProofSizeV1)
	}
	if size := msg.Size(); size > MaxBatchTransferMessageBytesV1 {
		return fmt.Errorf("batch transfer message exceeds %d-byte hard cap: got %d", MaxBatchTransferMessageBytesV1, size)
	}
	return validateMsgBatchTransferEffectFramingV1(msg)
}

// validateMsgBatchTransferEffectFramingV1 validates only fields that belong to
// canonical_batch_payload_v1. It must remain independent of creator, proof,
// and the outer protobuf message size so prepare-before-proof flows can compute
// the owner-authorized digest.
func validateMsgBatchTransferEffectFramingV1(msg *MsgBatchTransfer) error {
	if msg == nil {
		return fmt.Errorf("batch transfer message is required")
	}
	if len(msg.Nullifiers) == 0 || len(msg.Nullifiers) > int(BatchJoinSplitV1MaxInputs) {
		return fmt.Errorf("input count must be in 1..%d", BatchJoinSplitV1MaxInputs)
	}
	if len(msg.Outputs) == 0 || len(msg.Outputs) > int(BatchJoinSplitV1MaxOutputs) {
		return fmt.Errorf("output count must be in 1..%d", BatchJoinSplitV1MaxOutputs)
	}
	if len(msg.Root) != expectedFieldElementBytes {
		return fmt.Errorf("batch merkle root must be exactly %d bytes", expectedFieldElementBytes)
	}
	if len(msg.AuditKeyId) == 0 || len(msg.AuditKeyId) > AuditKeyIDV1MaxBytes {
		return fmt.Errorf("audit key id must contain 1..%d bytes", AuditKeyIDV1MaxBytes)
	}
	if len(msg.AuditDisclosureTargetPubkey) != expectedFieldElementBytes {
		return fmt.Errorf("batch audit disclosure target pubkey must be exactly %d bytes", expectedFieldElementBytes)
	}

	for i, nullifier := range msg.Nullifiers {
		if len(nullifier) != expectedFieldElementBytes {
			return fmt.Errorf("batch nullifier %d must be exactly %d bytes", i, expectedFieldElementBytes)
		}
	}

	transferEnvelopeSize, err := EncryptedEnvelopeV1Size(EnvelopeTransferNoteV1)
	if err != nil {
		return err
	}
	disclosureEnvelopeSize, err := EncryptedEnvelopeV1Size(EnvelopeUserDisclosureV1)
	if err != nil {
		return err
	}
	for i, output := range msg.Outputs {
		if output == nil {
			return fmt.Errorf("batch output %d is required", i)
		}
		if len(output.Commitment) != expectedFieldElementBytes {
			return fmt.Errorf("batch output %d commitment must be exactly %d bytes", i, expectedFieldElementBytes)
		}
		if len(output.Ciphertext) != transferEnvelopeSize {
			return fmt.Errorf("batch output %d ciphertext must be exactly %d bytes", i, transferEnvelopeSize)
		}
		if len(output.ViewTag) != ViewTagLength {
			return fmt.Errorf("batch output %d view tag must be exactly %d bytes", i, ViewTagLength)
		}
		if !hasOneOfLengths(output.UserDisclosureDigest, 0, expectedFieldElementBytes) {
			return fmt.Errorf("batch output %d user disclosure digest must be empty or exactly %d bytes", i, expectedFieldElementBytes)
		}
		if !hasOneOfLengths(output.UserDisclosureTargetPubkey, 0, expectedFieldElementBytes) {
			return fmt.Errorf("batch output %d user disclosure target pubkey must be empty or exactly %d bytes", i, expectedFieldElementBytes)
		}
		if !hasOneOfLengths(output.UserDisclosurePayload, 0, DisclosurePlaintextV1Size, disclosureEnvelopeSize) {
			return fmt.Errorf("batch output %d user disclosure payload has invalid fixed length %d", i, len(output.UserDisclosurePayload))
		}
		if len(output.FullDisclosureDigest) != expectedFieldElementBytes {
			return fmt.Errorf("batch output %d full disclosure digest must be exactly %d bytes", i, expectedFieldElementBytes)
		}
		if len(output.AuditDisclosurePayload) != disclosureEnvelopeSize {
			return fmt.Errorf("batch output %d audit disclosure payload must be exactly %d bytes", i, disclosureEnvelopeSize)
		}
		if !hasOneOfLengths(output.SelfViewDisclosurePayload, 0, disclosureEnvelopeSize) {
			return fmt.Errorf("batch output %d self-view disclosure payload must be empty or exactly %d bytes", i, disclosureEnvelopeSize)
		}
	}
	return nil
}

func hasOneOfLengths(value []byte, allowed ...int) bool {
	for _, length := range allowed {
		if len(value) == length {
			return true
		}
	}
	return false
}

// CanonicalMsgBatchTransferPayloadSizeV1 returns the exact byte length of the
// frozen canonical owner-effect encoding without allocating or constructing
// that encoding. Only the cheap framing contract is evaluated.
func CanonicalMsgBatchTransferPayloadSizeV1(msg *MsgBatchTransfer) (uint64, error) {
	if err := validateMsgBatchTransferEffectFramingV1(msg); err != nil {
		return 0, err
	}

	var size uint64
	add := func(value uint64) error {
		if math.MaxUint64-size < value {
			return fmt.Errorf("canonical batch payload size overflows uint64")
		}
		size += value
		return nil
	}
	addLP := func(value []byte) error {
		if uint64(len(value)) > math.MaxUint32 {
			return fmt.Errorf("canonical batch payload field exceeds uint32 length framing")
		}
		return add(4 + uint64(len(value)))
	}

	if err := add(4); err != nil { // format version
		return 0, err
	}
	if err := addLP(msg.Root); err != nil {
		return 0, err
	}
	if err := add(4); err != nil { // input count
		return 0, err
	}
	for _, nullifier := range msg.Nullifiers {
		if err := addLP(nullifier); err != nil {
			return 0, err
		}
	}
	if err := add(4); err != nil { // output count
		return 0, err
	}
	for _, output := range msg.Outputs {
		for _, value := range [][]byte{output.Commitment, output.Ciphertext, output.ViewTag} {
			if err := addLP(value); err != nil {
				return 0, err
			}
		}
		if err := add(8); err != nil { // user policy and mode
			return 0, err
		}
		for _, value := range [][]byte{
			output.UserDisclosureDigest,
			output.UserDisclosureTargetPubkey,
			output.UserDisclosurePayload,
			output.FullDisclosureDigest,
			output.AuditDisclosurePayload,
			output.SelfViewDisclosurePayload,
		} {
			if err := addLP(value); err != nil {
				return 0, err
			}
		}
	}
	if err := addLP([]byte(msg.AuditKeyId)); err != nil {
		return 0, err
	}
	if err := add(8); err != nil { // audit key epoch
		return 0, err
	}
	if err := addLP(msg.AuditDisclosureTargetPubkey); err != nil {
		return 0, err
	}
	if err := add(8); err != nil { // expiry
		return 0, err
	}
	return size, nil
}

// ValidateMsgBatchTransferEffectsV1 applies the frozen batch effect
// semantics to the production message. Creator/proof framing is intentionally
// outside this helper because neither field belongs to the owner-effect digest.
func ValidateMsgBatchTransferEffectsV1(msg *MsgBatchTransfer) error {
	if msg == nil {
		return fmt.Errorf("batch transfer message is required")
	}
	return ValidateBatchTransferWirePrototypeV1(batchTransferPrototypeV1(msg))
}

// CanonicalMsgBatchTransferPayloadBytesV1 encodes the production message with
// the exact canonical batch owner-effect contract.
func CanonicalMsgBatchTransferPayloadBytesV1(msg *MsgBatchTransfer) ([]byte, error) {
	if err := validateMsgBatchTransferEffectFramingV1(msg); err != nil {
		return nil, err
	}
	return CanonicalBatchTransferPayloadBytesV1(batchTransferPrototypeV1(msg))
}

// ComputeMsgBatchTransferPayloadDigestV1 returns public inputs 11 and 12 for
// a production MsgBatchTransfer.
func ComputeMsgBatchTransferPayloadDigestV1(msg *MsgBatchTransfer) (DigestLimbs, error) {
	if err := validateMsgBatchTransferEffectFramingV1(msg); err != nil {
		return DigestLimbs{}, err
	}
	return ComputeBatchTransferPayloadDigestV1(batchTransferPrototypeV1(msg))
}

func batchTransferPrototypeV1(msg *MsgBatchTransfer) *BatchTransferWirePrototypeV1 {
	outputs := make([]*BatchTransferOutputWirePrototypeV1, len(msg.Outputs))
	for i, output := range msg.Outputs {
		if output == nil {
			continue
		}
		outputs[i] = &BatchTransferOutputWirePrototypeV1{
			Commitment:                 output.Commitment,
			Ciphertext:                 output.Ciphertext,
			ViewTag:                    output.ViewTag,
			UserPrivacyPolicy:          output.UserPrivacyPolicy,
			UserDisclosureMode:         output.UserDisclosureMode,
			UserDisclosureDigest:       output.UserDisclosureDigest,
			UserDisclosureTargetPubkey: output.UserDisclosureTargetPubkey,
			UserDisclosurePayload:      output.UserDisclosurePayload,
			FullDisclosureDigest:       output.FullDisclosureDigest,
			AuditDisclosurePayload:     output.AuditDisclosurePayload,
			SelfViewDisclosurePayload:  output.SelfViewDisclosurePayload,
		}
	}
	return &BatchTransferWirePrototypeV1{
		Creator:                     msg.Creator,
		Proof:                       msg.Proof,
		Root:                        msg.Root,
		Nullifiers:                  msg.Nullifiers,
		Outputs:                     outputs,
		AuditKeyId:                  msg.AuditKeyId,
		AuditKeyEpoch:               msg.AuditKeyEpoch,
		AuditDisclosureTargetPubkey: msg.AuditDisclosureTargetPubkey,
		ExpiresAtUnix:               msg.ExpiresAtUnix,
	}
}

// CanonicalBatchTransferPayloadBytesV1 freezes the exact owner-authorized
// effect view for the batch wire compatibility mirror. Creator and proof are
// deliberately excluded so a relayer may be replaced and a proof may be
// regenerated without changing the logical effect. Counts are encoded once
// as the lengths of their ordered vectors.
func CanonicalBatchTransferPayloadBytesV1(msg *BatchTransferWirePrototypeV1) ([]byte, error) {
	if err := ValidateBatchTransferWirePrototypeV1(msg); err != nil {
		return nil, err
	}

	var encoded bytes.Buffer
	writeUint32(&encoded, CanonicalBatchTransferPayloadFormatVersionV1)
	if err := writeLengthPrefixedBytes(&encoded, msg.Root); err != nil {
		return nil, err
	}
	if err := writeByteSlice(&encoded, msg.Nullifiers); err != nil {
		return nil, err
	}
	writeUint32(&encoded, uint32(len(msg.Outputs)))
	for _, output := range msg.Outputs {
		for _, value := range [][]byte{
			output.Commitment,
			output.Ciphertext,
			output.ViewTag,
		} {
			if err := writeLengthPrefixedBytes(&encoded, value); err != nil {
				return nil, err
			}
		}
		writeUint32(&encoded, output.UserPrivacyPolicy)
		writeUint32(&encoded, uint32(output.UserDisclosureMode))
		for _, value := range [][]byte{
			output.UserDisclosureDigest,
			output.UserDisclosureTargetPubkey,
			output.UserDisclosurePayload,
			output.FullDisclosureDigest,
			output.AuditDisclosurePayload,
			output.SelfViewDisclosurePayload,
		} {
			if err := writeLengthPrefixedBytes(&encoded, value); err != nil {
				return nil, err
			}
		}
	}
	if err := writeLengthPrefixedBytes(&encoded, []byte(msg.AuditKeyId)); err != nil {
		return nil, err
	}
	writeUint64(&encoded, msg.AuditKeyEpoch)
	if err := writeLengthPrefixedBytes(&encoded, msg.AuditDisclosureTargetPubkey); err != nil {
		return nil, err
	}
	writeUint64(&encoded, uint64(msg.ExpiresAtUnix))
	return encoded.Bytes(), nil
}

// ComputeBatchTransferPayloadDigestV1 returns the non-reduced SHA-256 digest
// limbs used by public inputs 11 and 12 of BatchJoinSplit16x32.
func ComputeBatchTransferPayloadDigestV1(msg *BatchTransferWirePrototypeV1) (DigestLimbs, error) {
	payload, err := CanonicalBatchTransferPayloadBytesV1(msg)
	if err != nil {
		return DigestLimbs{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte(BatchTransferPayloadV1ByteDomain))
	_, _ = h.Write(payload)
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return SplitDigestToLimbs(digest), nil
}

// ValidateBatchTransferWirePrototypeV1 applies the frozen effect-field
// structural and canonical rules without registering a production Msg or
// keeper handler. Creator and proof framing remain separate outer-message
// checks because those two fields are deliberately excluded from the effect.
func ValidateBatchTransferWirePrototypeV1(msg *BatchTransferWirePrototypeV1) error {
	if msg == nil {
		return fmt.Errorf("batch transfer wire prototype is required")
	}
	if err := ValidateBatchJoinSplitCountsV1(uint32(len(msg.Nullifiers)), uint32(len(msg.Outputs))); err != nil {
		return err
	}
	if err := validateActiveFieldElementBytesStrict("batch merkle root", msg.Root); err != nil {
		return err
	}
	for i, nullifier := range msg.Nullifiers {
		if err := validateActiveFieldElementBytesStrict(fmt.Sprintf("batch nullifier %d", i), nullifier); err != nil {
			return err
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("batch nullifier", msg.Nullifiers); err != nil {
		return err
	}

	commitments := make([][]byte, len(msg.Outputs))
	if msg.Outputs[0] == nil {
		return fmt.Errorf("batch output 0 is required")
	}
	selfViewEnabled := len(msg.Outputs[0].GetSelfViewDisclosurePayload()) != 0
	for i, output := range msg.Outputs {
		if output == nil {
			return fmt.Errorf("batch output %d is required", i)
		}
		if err := validateBatchTransferOutputWirePrototypeV1(uint32(i), output, selfViewEnabled); err != nil {
			return fmt.Errorf("batch output %d is invalid: %w", i, err)
		}
		commitments[i] = output.Commitment
	}
	if err := ValidateDistinctCanonicalFieldElements("batch commitment", commitments); err != nil {
		return err
	}
	if err := ValidateAuditKeyIDV1(msg.AuditKeyId); err != nil {
		return err
	}
	if msg.AuditKeyEpoch == 0 {
		return fmt.Errorf("batch audit key epoch must be positive")
	}
	if _, err := privacycrypto.DecodeCanonicalPoint(msg.AuditDisclosureTargetPubkey); err != nil {
		return fmt.Errorf("batch audit disclosure target pubkey is invalid: %w", err)
	}
	if msg.ExpiresAtUnix <= 0 {
		return fmt.Errorf("batch expires_at_unix must be positive")
	}
	return nil
}

func validateBatchTransferOutputWirePrototypeV1(index uint32, output *BatchTransferOutputWirePrototypeV1, selfViewEnabled bool) error {
	if err := validateActiveFieldElementBytesStrict("commitment", output.Commitment); err != nil {
		return err
	}
	if _, err := UnwrapEncryptedEnvelopeV1(output.Ciphertext, EnvelopeTransferNoteV1); err != nil {
		return fmt.Errorf("ciphertext is not a canonical transfer-note envelope: %w", err)
	}
	if len(output.ViewTag) != ViewTagLength {
		return fmt.Errorf("view tag must be exactly %d bytes", ViewTagLength)
	}
	if err := validateTransferDisclosurePolicy(output.UserPrivacyPolicy); err != nil {
		return err
	}
	if err := validateBatchUserDisclosureWirePrototypeV1(index, output); err != nil {
		return err
	}
	if err := validateActiveFieldElementBytesStrict("full disclosure digest", output.FullDisclosureDigest); err != nil {
		return err
	}
	if _, err := UnwrapEncryptedEnvelopeV1(output.AuditDisclosurePayload, EnvelopeAuditDisclosureV1); err != nil {
		return fmt.Errorf("audit disclosure payload is not a canonical envelope: %w", err)
	}
	present := len(output.SelfViewDisclosurePayload) != 0
	if present != selfViewEnabled {
		return fmt.Errorf("self-view disclosure must be batch-level all-or-none")
	}
	if present {
		if _, err := UnwrapEncryptedEnvelopeV1(output.SelfViewDisclosurePayload, EnvelopeSelfViewDisclosureV1); err != nil {
			return fmt.Errorf("self-view disclosure payload is not a canonical envelope: %w", err)
		}
	}
	return nil
}

func validateBatchUserDisclosureWirePrototypeV1(index uint32, output *BatchTransferOutputWirePrototypeV1) error {
	if output.UserPrivacyPolicy == TransferPrivacyPolicyAllPrivate {
		if output.UserDisclosureMode != UserDisclosureMode_USER_DISCLOSURE_MODE_NONE ||
			len(output.UserDisclosureDigest) != 0 || len(output.UserDisclosureTargetPubkey) != 0 ||
			len(output.UserDisclosurePayload) != 0 {
			return fmt.Errorf("all-private output must use NONE mode and empty user disclosure fields")
		}
		return nil
	}
	if err := validateActiveFieldElementBytesStrict("user disclosure digest", output.UserDisclosureDigest); err != nil {
		return err
	}
	switch output.UserDisclosureMode {
	case UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
		if len(output.UserDisclosureTargetPubkey) != 0 {
			return fmt.Errorf("public user disclosure target must be empty")
		}
		plaintext, err := UnmarshalDisclosurePlaintextV1(output.UserDisclosurePayload)
		if err != nil {
			return fmt.Errorf("public user disclosure payload is invalid: %w", err)
		}
		if plaintext.Plane != DisclosurePlaneUserV1 || plaintext.OutputIndex != index ||
			plaintext.Policy != output.UserPrivacyPolicy || !bytes.Equal(plaintext.Commitment.FillBytes(make([]byte, expectedFieldElementBytes)), output.Commitment) {
			return fmt.Errorf("public user disclosure metadata does not match output")
		}
		digest, err := ComputeBatchUserDisclosureDigestV1(BatchUserDisclosureV1Input{
			OutputIndex: index, Commitment: plaintext.Commitment, Policy: plaintext.Policy,
			DisclosedFieldBitmap: plaintext.DisclosedFieldBitmap, SelectedAmount: plaintext.Amount,
			SelectedFromSpendKeyX: plaintext.SenderSpendKeyX, SelectedFromSpendKeyY: plaintext.SenderSpendKeyY,
			SelectedFromViewKeyX: plaintext.SenderViewKeyX, SelectedFromViewKeyY: plaintext.SenderViewKeyY,
			SelectedToSpendKeyX: plaintext.RecipientSpendKeyX, SelectedToSpendKeyY: plaintext.RecipientSpendKeyY,
			SelectedToViewKeyX: plaintext.RecipientViewKeyX, SelectedToViewKeyY: plaintext.RecipientViewKeyY,
			AssetID: plaintext.AssetID, UserDisclosureBlinding: plaintext.DisclosureBlinding,
		})
		if err != nil {
			return err
		}
		if !bytes.Equal(digest.FillBytes(make([]byte, expectedFieldElementBytes)), output.UserDisclosureDigest) {
			return fmt.Errorf("public user disclosure digest does not match plaintext")
		}
	case UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
		if _, err := privacycrypto.DecodeCanonicalPoint(output.UserDisclosureTargetPubkey); err != nil {
			return fmt.Errorf("user disclosure target pubkey is invalid: %w", err)
		}
		if _, err := UnwrapEncryptedEnvelopeV1(output.UserDisclosurePayload, EnvelopeUserDisclosureV1); err != nil {
			return fmt.Errorf("user disclosure payload is not a canonical envelope: %w", err)
		}
	case UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
		return fmt.Errorf("NONE user disclosure mode is only valid for all-private output")
	default:
		return fmt.Errorf("unsupported user disclosure mode %d", output.UserDisclosureMode)
	}
	return nil
}
