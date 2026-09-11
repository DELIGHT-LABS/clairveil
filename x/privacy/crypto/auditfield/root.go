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
