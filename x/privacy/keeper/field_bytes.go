package keeper

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

const fieldElementByteSize = 32

func canonicalizeFieldBytes(bz []byte) ([]byte, error) {
	bi := new(big.Int).SetBytes(bz)
	if bi.BitLen() > fieldElementByteSize*8 {
		return nil, fmt.Errorf("field element exceeds the %d-byte width", fieldElementByteSize)
	}

	canonical := make([]byte, fieldElementByteSize)
	raw := bi.Bytes()
	copy(canonical[fieldElementByteSize-len(raw):], raw)

	return canonical, nil
}

func canonicalizeFieldBytesOrOriginal(bz []byte) []byte {
	canonical, err := canonicalizeFieldBytes(bz)
	if err != nil {
		return bz
	}

	return canonical
}

func validateFieldElementBytesStrict(bz []byte) ([]byte, error) {
	if len(bz) != fieldElementByteSize {
		return nil, fmt.Errorf("field element must be exactly %d bytes", fieldElementByteSize)
	}

	var elem fr.Element
	if err := elem.SetBytesCanonical(bz); err != nil {
		return nil, fmt.Errorf("field element must be canonical")
	}

	canonical := elem.Bytes()
	out := make([]byte, fieldElementByteSize)
	copy(out, canonical[:])
	return out, nil
}
