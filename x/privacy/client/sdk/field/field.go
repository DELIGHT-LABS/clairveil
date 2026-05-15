package field

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

const ByteSize = 32

func ValidateCanonicalBytes32(bz []byte) error {
	if len(bz) != ByteSize {
		return fmt.Errorf("field element must be %d bytes", ByteSize)
	}

	var elem fr.Element
	if err := elem.SetBytesCanonical(bz); err != nil {
		return fmt.Errorf("field element is not canonical")
	}

	return nil
}

func CanonicalBytesFromBytes(bz []byte) ([]byte, error) {
	if len(bz) == 0 {
		return nil, fmt.Errorf("field element is empty")
	}
	if len(bz) > ByteSize {
		return nil, fmt.Errorf("field element exceeds %d bytes", ByteSize)
	}

	out := make([]byte, ByteSize)
	copy(out[ByteSize-len(bz):], bz)

	if err := ValidateCanonicalBytes32(out); err != nil {
		return nil, err
	}

	return out, nil
}

func CanonicalBytesFromBigInt(v *big.Int) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("field element is nil")
	}
	if v.Sign() < 0 {
		return nil, fmt.Errorf("field element is negative")
	}

	bz := v.Bytes()
	if len(bz) > ByteSize {
		return nil, fmt.Errorf("field element exceeds %d bytes", ByteSize)
	}

	out := make([]byte, ByteSize)
	copy(out[ByteSize-len(bz):], bz)

	if err := ValidateCanonicalBytes32(out); err != nil {
		return nil, err
	}

	return out, nil
}

func CanonicalHexFromBigInt(v *big.Int) (string, error) {
	bz, err := CanonicalBytesFromBigInt(v)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bz), nil
}

func DecodeCanonicalHex(value, fieldName string) ([]byte, error) {
	bz, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}

	canonical, err := CanonicalBytesFromBytes(bz)
	if err != nil {
		return nil, fmt.Errorf("invalid %s bytes: %w", fieldName, err)
	}

	return canonical, nil
}

func CircuitHexFromBigInt(v *big.Int) (string, error) {
	if v == nil {
		return "", fmt.Errorf("field element is nil")
	}
	if v.Sign() < 0 {
		return "", fmt.Errorf("field element is negative")
	}

	var elem fr.Element
	elem.SetBigInt(v)
	bz := elem.Bytes()
	return hex.EncodeToString(bz[:]), nil
}
