package crypto

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
	"io"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

// FieldValue is an opaque BN254 Fr value used by secret fixed-field code.
// Canonical bytes are the sole interchange format at package boundaries.
type FieldValue struct{ value frct.Element }

func (FieldValue) String() string   { return "crypto.FieldValue(<redacted>)" }
func (FieldValue) GoString() string { return "crypto.FieldValue(<redacted>)" }
func (FieldValue) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("crypto.FieldValue(<redacted>)"))
}
func (FieldValue) MarshalText() ([]byte, error) {
	return nil, errors.New("secret field serialization is disabled")
}
func (FieldValue) MarshalJSON() ([]byte, error) {
	return nil, errors.New("secret field serialization is disabled")
}

func ParseFieldValueBE32(in []byte) (FieldValue, error) {
	var v frct.Element
	if err := v.SetBytes(in); err != nil {
		return FieldValue{}, fmt.Errorf("invalid FieldValue: %w", err)
	}
	return FieldValue{value: v}, nil
}

func ReduceFieldBytes32(in [32]byte) FieldValue  { return FieldValue{value: frct.Reduce256(in)} }
func FieldValueFromUint64(v uint64) FieldValue   { return FieldValue{value: frct.Uint64(v)} }
func (v FieldValue) Bytes() [32]byte             { return v.value.Bytes() }
func (v FieldValue) IsZero() bool                { return v.value.IsZero() == 1 }
func (v FieldValue) Equal(other FieldValue) bool { return v.value.Equal(&other.value) == 1 }

// SampleNonzeroFieldValue preserves the legacy nonzero q randomness domain
// while returning an Fr transport for note and disclosure preimages.
func SampleNonzeroFieldValue(reader io.Reader) (out FieldValue, err error) {
	if err = secretprofile.Check(); err != nil {
		return out, err
	}
	subtle.WithDataIndependentTiming(func() {
		var value scalarct.NonzeroScalar
		value, err = scalarct.SampleNonzeroScalar(reader)
		defer secretmem.Clear(&value)
		if err != nil {
			return
		}
		raw := value.Bytes()
		defer secretmem.Clear(&raw)
		out, err = ParseFieldValueBE32(raw[:])
	})
	return out, err
}

// SecretPoint is a validated Edwards point for use with a private scalar.
// It cannot be constructed from arbitrary coordinates.
type SecretPoint struct {
	point edwardsct.Point
	valid bool
}

func (SecretPoint) String() string   { return "crypto.SecretPoint(<redacted>)" }
func (SecretPoint) GoString() string { return "crypto.SecretPoint(<redacted>)" }
func (SecretPoint) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("crypto.SecretPoint(<redacted>)"))
}

// ParseSecretPoint64 performs the full public Point64 validation: exact Fr
// coordinates, curve equation, prime subgroup and nonidentity.
func ParseSecretPoint64(in []byte) (SecretPoint, error) {
	p, err := edwardsct.DecodePoint64(in)
	if err != nil {
		return SecretPoint{}, err
	}
	return SecretPoint{point: p, valid: true}, nil
}

func parseLegacySecretPoint(in []byte) (SecretPoint, error) {
	p, err := edwardsct.DecodeLegacyCompressed(in)
	if err != nil {
		return SecretPoint{}, err
	}
	return SecretPoint{point: p, valid: true}, nil
}

func baseEdwardsPoint() edwardsct.Point { return edwardsct.Base() }

// SecretPointFromPublic validates a legacy public point before crossing into
// the private core. Existing APIs can use this compatibility boundary.
func SecretPointFromPublic(in crypto_tedwards.PointAffine) (SecretPoint, error) {
	x, y := in.X.Bytes(), in.Y.Bytes()
	var encoded [64]byte
	copy(encoded[:32], x[:])
	copy(encoded[32:], y[:])
	return ParseSecretPoint64(encoded[:])
}

// PublicPointFieldValues is the explicit public-key adapter for SDK builders.
// It validates the complete Point64 contract before exposing canonical field
// coordinates to a typed secret note request.
func PublicPointFieldValues(in crypto_tedwards.PointAffine) (FieldValue, FieldValue, error) {
	p, err := SecretPointFromPublic(in)
	if err != nil {
		return FieldValue{}, FieldValue{}, err
	}
	encoded, err := p.Point64()
	if err != nil {
		return FieldValue{}, FieldValue{}, err
	}
	x, err := ParseFieldValueBE32(encoded[:32])
	if err != nil {
		return FieldValue{}, FieldValue{}, err
	}
	y, err := ParseFieldValueBE32(encoded[32:])
	if err != nil {
		return FieldValue{}, FieldValue{}, err
	}
	return x, y, nil
}

// LegacyCompressedPointFromFieldValues serializes a validated public key from
// its canonical affine coordinates. It keeps SDK payload construction on the
// fixed-field public-point boundary rather than rebuilding a math/big point.
func LegacyCompressedPointFromFieldValues(x, y FieldValue) ([32]byte, error) {
	var encoded [64]byte
	xb, yb := x.Bytes(), y.Bytes()
	copy(encoded[:32], xb[:])
	copy(encoded[32:], yb[:])
	p, err := edwardsct.DecodePoint64(encoded[:])
	if err != nil {
		return [32]byte{}, err
	}
	return p.CompressLegacy()
}

func (p SecretPoint) Point64() ([64]byte, error) { return p.point.EncodePoint64() }

func sharedSecretPoint(p SecretPoint, scalar SecretScalar) (SecretPoint, error) {
	if !p.valid {
		return SecretPoint{}, errors.New("invalid SecretPoint")
	}
	if err := secretprofile.Check(); err != nil {
		return SecretPoint{}, err
	}
	n, err := scalar.scalar()
	defer secretmem.Clear(&n)
	defer secretmem.Clear(&scalar)
	if err != nil {
		return SecretPoint{}, err
	}
	var result edwardsct.Point
	subtle.WithDataIndependentTiming(func() { result.ScalarMultNonzero(&p.point, n) })
	return SecretPoint{point: result, valid: true}, nil
}

func (p SecretPoint) legacyCompressed() ([32]byte, error) {
	if !p.valid {
		return [32]byte{}, errors.New("invalid SecretPoint")
	}
	var out [32]byte
	var err error
	subtle.WithDataIndependentTiming(func() { out, err = p.point.CompressLegacy() })
	return out, err
}
func (p SecretPoint) affineFieldValues() (FieldValue, FieldValue, error) {
	if !p.valid {
		return FieldValue{}, FieldValue{}, errors.New("invalid SecretPoint")
	}
	var a edwardsct.Affine
	var ok bool
	subtle.WithDataIndependentTiming(func() { a, ok = p.point.Affine() })
	if !ok {
		return FieldValue{}, FieldValue{}, fmt.Errorf("point normalization failed")
	}
	return FieldValue{value: a.X}, FieldValue{value: a.Y}, nil
}
