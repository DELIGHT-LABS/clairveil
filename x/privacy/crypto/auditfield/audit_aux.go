package auditfield

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
)

const (
	depositRecoveryCiphertextSize  = 406
	noteRecoveryCiphertextSize     = 438
	publicDisclosurePayloadSize    = 400
	encryptedDisclosurePayloadSize = 480
	viewTagSize                    = 2
)

// AuxOutput is the leaf DTO for the canonical Aux encoder. Generated message
// types must be checked and defensively copied by the P4 adapter before they
// are converted into this type.
type AuxOutput struct {
	RecoveryCiphertext, ViewTag             []byte
	UserPolicy                              uint32
	UserMode                                uint8
	UserDigest                              Field32
	UserTargetPubKey, UserDisclosurePayload []byte
	SelfFullDigest                          Field32
	SelfViewPayload                         []byte
}

// EncodeAuxFrame encodes the independent Aux framing contract. It checks
// shape, fixed payload lengths, field consistency, and recipient compressed
// public points. Generated-message and nested payload validation remain the
// P4 adapter's responsibility. No frame authenticates its payload by itself.
func EncodeAuxFrame(kind Kind, outputs []AuxOutput) ([]byte, error) {
	if err := validateAuxOutputCount(kind, outputs); err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, 3)
	var version [2]byte
	binary.BigEndian.PutUint16(version[:], AuditPlainSchema)
	encoded = append(encoded, version[:]...)
	encoded = append(encoded, byte(kind), byte(len(outputs)))
	for index, output := range outputs {
		if err := validateAuxOutput(kind, index, output); err != nil {
			return nil, err
		}
		var err error
		for _, value := range [][]byte{output.RecoveryCiphertext, output.ViewTag} {
			encoded, err = appendLP(encoded, value)
			if err != nil {
				return nil, err
			}
		}
		var policy [4]byte
		binary.BigEndian.PutUint32(policy[:], output.UserPolicy)
		encoded = append(encoded, policy[:]...)
		encoded = append(encoded, output.UserMode)
		encoded = append(encoded, output.UserDigest[:]...)
		for _, value := range [][]byte{output.UserTargetPubKey, output.UserDisclosurePayload} {
			encoded, err = appendLP(encoded, value)
			if err != nil {
				return nil, err
			}
		}
		encoded = append(encoded, output.SelfFullDigest[:]...)
		encoded, err = appendLP(encoded, output.SelfViewPayload)
		if err != nil {
			return nil, err
		}
	}
	return encoded, nil
}

func validateAuxOutputCount(kind Kind, outputs []AuxOutput) error {
	switch kind {
	case KindDeposit:
		if len(outputs) != 1 {
			return fmt.Errorf("deposit Aux requires exactly one output")
		}
	case KindWithdraw:
		if len(outputs) != 0 {
			return fmt.Errorf("withdraw Aux requires no outputs")
		}
	case KindTransfer2x2:
		if len(outputs) != 2 {
			return fmt.Errorf("transfer Aux requires exactly two outputs")
		}
	case KindBatch16x32:
		if len(outputs) == 0 || len(outputs) > 32 {
			return fmt.Errorf("batch Aux requires 1..32 outputs")
		}
	default:
		return fmt.Errorf("%w: %d", ErrInvalidKind, kind)
	}
	return nil
}

func validateAuxOutput(kind Kind, index int, output AuxOutput) error {
	if err := validateField32(output.UserDigest); err != nil {
		return fmt.Errorf("Aux output %d user digest: %w", index, err)
	}
	if err := validateField32(output.SelfFullDigest); err != nil {
		return fmt.Errorf("Aux output %d self digest: %w", index, err)
	}
	recoverySize := noteRecoveryCiphertextSize
	if kind == KindDeposit {
		recoverySize = depositRecoveryCiphertextSize
	}
	if len(output.RecoveryCiphertext) != recoverySize {
		return fmt.Errorf("Aux output %d recovery ciphertext must be exactly %d bytes", index, recoverySize)
	}
	if kind == KindDeposit {
		if len(output.ViewTag) != 0 {
			return fmt.Errorf("deposit Aux output view tag must be empty")
		}
	} else if len(output.ViewTag) != viewTagSize {
		return fmt.Errorf("Aux output %d view tag must be exactly %d bytes", index, viewTagSize)
	}
	if len(output.SelfViewPayload) != 0 && len(output.SelfViewPayload) != encryptedDisclosurePayloadSize {
		return fmt.Errorf("Aux output %d self-view payload must be empty or exactly %d bytes", index, encryptedDisclosurePayloadSize)
	}
	if kind == KindDeposit || (kind == KindTransfer2x2 && index == 1) {
		if output.UserPolicy != 0 || output.UserMode != 0 || !output.UserDigest.IsZero() || len(output.UserTargetPubKey) != 0 || len(output.UserDisclosurePayload) != 0 || !output.SelfFullDigest.IsZero() || len(output.SelfViewPayload) != 0 {
			return fmt.Errorf("Aux output %d does not permit user or self disclosure fields", index)
		}
		return nil
	}
	if output.UserPolicy > 7 {
		return fmt.Errorf("Aux output %d has unsupported user policy %d", index, output.UserPolicy)
	}
	if kind == KindBatch16x32 && output.SelfFullDigest.IsZero() {
		return fmt.Errorf("Aux batch output %d full disclosure digest must be non-zero", index)
	}
	switch output.UserMode {
	case 0:
		if output.UserPolicy != 0 || !output.UserDigest.IsZero() || len(output.UserTargetPubKey) != 0 || len(output.UserDisclosurePayload) != 0 {
			return fmt.Errorf("Aux output %d all-private user disclosure must be empty", index)
		}
	case 1:
		if output.UserPolicy == 0 || len(output.UserTargetPubKey) != 0 || len(output.UserDisclosurePayload) != publicDisclosurePayloadSize {
			return fmt.Errorf("Aux output %d public disclosure fields are inconsistent", index)
		}
		if kind == KindBatch16x32 && output.UserDigest.IsZero() {
			return fmt.Errorf("Aux batch output %d public disclosure digest must be non-zero", index)
		}
	case 2:
		if output.UserPolicy == 0 || len(output.UserTargetPubKey) != FieldSize || len(output.UserDisclosurePayload) != encryptedDisclosurePayloadSize {
			return fmt.Errorf("Aux output %d recipient-encrypted disclosure frame is invalid", index)
		}
		if kind == KindBatch16x32 && output.UserDigest.IsZero() {
			return fmt.Errorf("Aux batch output %d recipient disclosure digest must be non-zero", index)
		}
		if _, err := edwardsct.DecodeLegacyCompressed(output.UserTargetPubKey); err != nil {
			return fmt.Errorf("Aux output %d recipient-encrypted target: %w", index, ErrInvalidPoint)
		}
	default:
		return fmt.Errorf("Aux output %d has unsupported user disclosure mode %d", index, output.UserMode)
	}
	return nil
}

func appendLP(dst, value []byte) ([]byte, error) {
	if uint64(len(value)) > math.MaxUint32 {
		return nil, fmt.Errorf("Aux length exceeds uint32")
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	dst = append(dst, length[:]...)
	dst = append(dst, value...)
	return dst, nil
}
