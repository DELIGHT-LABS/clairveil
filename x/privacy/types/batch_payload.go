package types

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const (
	BatchTransferPayloadV1ByteDomain             = "clairveil.batch-transfer-payload.v1"
	CanonicalBatchTransferPayloadFormatVersionV1 = uint32(1)
)

// CanonicalBatchTransferPayloadBytesV1 freezes the exact owner-authorized
// effect view for the Session 2 batch wire prototype. Creator and proof are
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
