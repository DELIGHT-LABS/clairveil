package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
)

const (
	// CanonicalPointSize is the exact compressed size of a BN254 twisted
	// Edwards point used on Clairveil wire boundaries.
	CanonicalPointSize = fr.Bytes
	// CanonicalEdDSASignatureSize is the exact R || S wire size.
	CanonicalEdDSASignatureSize = 2 * fr.Bytes
)

var (
	ErrInvalidPointEncoding  = errors.New("invalid canonical BN254 twisted Edwards point")
	ErrInvalidEdDSASignature = errors.New("invalid canonical EdDSA signature")
)

// DecodeCanonicalPoint decodes one exact compressed point and rejects
// non-canonical encodings, off-curve points, the identity, and points outside
// the prime-order subgroup.
func DecodeCanonicalPoint(encoded []byte) (*twistededwards.PointAffine, error) {
	if len(encoded) != CanonicalPointSize {
		return nil, fmt.Errorf("%w: expected exactly %d bytes, got %d", ErrInvalidPointEncoding, CanonicalPointSize, len(encoded))
	}

	var point twistededwards.PointAffine
	if _, err := point.SetBytes(encoded); err != nil {
		return nil, fmt.Errorf("%w: decode failed: %v", ErrInvalidPointEncoding, err)
	}

	canonical := point.Bytes()
	if !bytes.Equal(canonical[:], encoded) {
		return nil, fmt.Errorf("%w: non-canonical compressed encoding", ErrInvalidPointEncoding)
	}
	if err := ValidatePrimeSubgroupPoint(&point); err != nil {
		return nil, err
	}

	return &point, nil
}

// ValidatePrimeSubgroupPoint validates an already-decoded point before it is
// used by a native cryptographic operation.
func ValidatePrimeSubgroupPoint(point *twistededwards.PointAffine) error {
	if point == nil {
		return fmt.Errorf("%w: point is nil", ErrInvalidPointEncoding)
	}
	if !point.IsOnCurve() {
		return fmt.Errorf("%w: point is not on the curve", ErrInvalidPointEncoding)
	}
	if point.IsZero() {
		return fmt.Errorf("%w: identity point is not allowed", ErrInvalidPointEncoding)
	}

	curve := twistededwards.GetEdwardsCurve()
	var orderMultiple twistededwards.PointAffine
	orderMultiple.ScalarMultiplication(point, &curve.Order)
	if !orderMultiple.IsZero() {
		return fmt.Errorf("%w: point is not in the prime-order subgroup", ErrInvalidPointEncoding)
	}

	return nil
}

// DecodeCanonicalEdDSASignature decodes an exact 64-byte R || S signature.
// R follows the same point rules as public wire keys, and S must use the
// fixed-width canonical representation in the range 0 < S < subgroup order.
func DecodeCanonicalEdDSASignature(encoded []byte) (*cryptoeddsa.Signature, error) {
	if len(encoded) != CanonicalEdDSASignatureSize {
		return nil, fmt.Errorf("%w: expected exactly %d bytes, got %d", ErrInvalidEdDSASignature, CanonicalEdDSASignatureSize, len(encoded))
	}

	if _, err := DecodeCanonicalPoint(encoded[:CanonicalPointSize]); err != nil {
		return nil, fmt.Errorf("%w: invalid R: %v", ErrInvalidEdDSASignature, err)
	}

	curve := twistededwards.GetEdwardsCurve()
	scalar := new(big.Int).SetBytes(encoded[CanonicalPointSize:])
	if scalar.Sign() <= 0 {
		return nil, fmt.Errorf("%w: S must be greater than zero", ErrInvalidEdDSASignature)
	}
	if scalar.Cmp(&curve.Order) >= 0 {
		return nil, fmt.Errorf("%w: S must be smaller than the subgroup order", ErrInvalidEdDSASignature)
	}

	var signature cryptoeddsa.Signature
	if _, err := signature.SetBytes(encoded); err != nil {
		return nil, fmt.Errorf("%w: decode failed: %v", ErrInvalidEdDSASignature, err)
	}
	if canonical := signature.Bytes(); !bytes.Equal(canonical, encoded) {
		return nil, fmt.Errorf("%w: non-canonical R or S encoding", ErrInvalidEdDSASignature)
	}

	return &signature, nil
}
