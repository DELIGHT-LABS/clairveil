package auditfield

import "fmt"

// ParseCipherRoot decodes two full Fr values, not two uint128 digest limbs.
func ParseCipherRoot(raw []byte) (CipherRoot, error) {
	if len(raw) != 2*FieldSize {
		return CipherRoot{}, fmt.Errorf("cipher root requires exactly 64 bytes")
	}
	left, err := ParseField32(raw[:FieldSize])
	if err != nil {
		return CipherRoot{}, err
	}
	right, err := ParseField32(raw[FieldSize:])
	if err != nil {
		return CipherRoot{}, err
	}
	return CipherRoot{Left: left, Right: right}, nil
}

func (r CipherRoot) Bytes() ([64]byte, error) {
	if err := r.Validate(); err != nil {
		return [64]byte{}, err
	}
	var out [64]byte
	copy(out[:32], r.Left[:])
	copy(out[32:], r.Right[:])
	return out, nil
}

// ComputeCipherRoot binds a validated public context to the actual envelope.
// This does not authenticate chain provenance or establish correct encryption;
// the pinned transaction proof establishes the encryption relation.
func ComputeCipherRoot(context AuditContext, envelope EnvelopeFrame) (CipherRoot, error) {
	if _, err := envelope.Bytes(); err != nil {
		return CipherRoot{}, err
	}
	if context.Kind() != envelope.Kind() || context.pi[9][31] != envelope.inputs || context.pi[10][31] != envelope.outputs {
		return CipherRoot{}, fmt.Errorf("audit context and envelope shape differ")
	}
	t, err := context.T(envelope.Nonce())
	if err != nil {
		return CipherRoot{}, err
	}
	transcript, err := fieldElements(t[:])
	if err != nil {
		return CipherRoot{}, err
	}
	fields := append(envelope.Ciphertext(), envelope.Tag())
	ciphertext, err := fieldElements(fields)
	if err != nil {
		return CipherRoot{}, err
	}
	point, err := envelope.EphemeralFrame().validPoint()
	if err != nil {
		return CipherRoot{}, err
	}
	return cipherRoot(context.Kind(), transcript, &point, ciphertext)
}
