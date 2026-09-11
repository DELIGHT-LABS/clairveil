package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
	"io"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

// ErrUnsupportedSecretProfile identifies unavailable native acceleration.
var ErrUnsupportedSecretProfile = secretprofile.ErrUnsupported

// SecretScalar is an opaque, nonzero scalar modulo the BN254 Edwards subgroup
// order. It has no big.Int conversion; callers serialize it only as BE32.
type SecretScalar struct{ value scalarct.NonzeroScalar }

func (SecretScalar) String() string   { return "crypto.SecretScalar(<redacted>)" }
func (SecretScalar) GoString() string { return "crypto.SecretScalar(<redacted>)" }
func (SecretScalar) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("crypto.SecretScalar(<redacted>)"))
}
func (SecretScalar) MarshalText() ([]byte, error) {
	return nil, errors.New("secret scalar serialization is disabled")
}
func (SecretScalar) MarshalJSON() ([]byte, error) {
	return nil, errors.New("secret scalar serialization is disabled")
}

// ImportNonzeroScalarBE32 strictly imports a persisted private key. Seeds are
// not private-key encodings and must use DeriveIdentityScalarSeed32 instead.
func ImportNonzeroScalarBE32(in []byte) (out SecretScalar, err error) {
	if err = secretprofile.Check(); err != nil {
		return SecretScalar{}, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = importNonzeroScalarBE32(in) })
	return out, err
}

func importNonzeroScalarBE32(in []byte) (SecretScalar, error) {
	if err := secretprofile.Check(); err != nil {
		return SecretScalar{}, err
	}
	n, err := scalarct.ParseNonzeroScalarBE32(in)
	if err != nil {
		return SecretScalar{}, fmt.Errorf("invalid secret scalar: %w", err)
	}
	return SecretScalar{value: n}, nil
}

// DeriveIdentityScalarSeed32 preserves the legacy seed mod q derivation and
// maps only a zero result to one.
func DeriveIdentityScalarSeed32(seed [32]byte) (result SecretScalar, err error) {
	if err = secretprofile.Check(); err != nil {
		return SecretScalar{}, err
	}
	subtle.WithDataIndependentTiming(func() { result = SecretScalar{value: scalarct.DeriveIdentityScalarSeed32(seed)} })
	return result, nil
}

// SampleSecretScalar samples a fresh nonzero scalar from reader. Nil uses the
// system CSPRNG. Reader failures are returned without a fallback.
func SampleSecretScalar(reader io.Reader) (out SecretScalar, err error) {
	if err = secretprofile.Check(); err != nil {
		return SecretScalar{}, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = sampleSecretScalar(reader) })
	return out, err
}

func sampleSecretScalar(reader io.Reader) (SecretScalar, error) {
	if err := secretprofile.Check(); err != nil {
		return SecretScalar{}, err
	}
	if reader == nil {
		reader = rand.Reader
	}
	n, err := scalarct.SampleNonzeroScalar(reader)
	if err != nil {
		return SecretScalar{}, err
	}
	return SecretScalar{value: n}, nil
}

// Bytes returns the exact canonical big-endian scalar encoding.
func (s SecretScalar) Bytes() [32]byte { return s.value.Bytes() }

// IsValid detects a zero-value SecretScalar before it reaches a secret core.
func (s SecretScalar) IsValid() bool { return s.value.IsValid() == 1 }

func (s SecretScalar) scalar() (scalarct.NonzeroScalar, error) {
	if !s.IsValid() {
		return scalarct.NonzeroScalar{}, fmt.Errorf("invalid zero SecretScalar")
	}
	return s.value, nil
}

// PublicKey derives [s]G through the fixed 256-bit Edwards path and converts
// the resulting public coordinates at this explicit compatibility boundary.
func PublicKey(s SecretScalar) (out *crypto_tedwards.PointAffine, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = publicKey(s) })
	return out, err
}

func publicKey(s SecretScalar) (*crypto_tedwards.PointAffine, error) {
	if err := secretprofile.Check(); err != nil {
		return nil, err
	}
	n, err := s.scalar()
	defer secretmem.Clear(&n)
	defer secretmem.Clear(&s)
	if err != nil {
		return nil, err
	}
	var p edwardsct.Point
	var a edwardsct.Affine
	var ok bool
	base := edwardsct.Base()
	subtle.WithDataIndependentTiming(func() { p.ScalarMultNonzero(&base, n); a, ok = p.Affine() })
	if !ok {
		return nil, fmt.Errorf("public key normalization failed")
	}
	var out crypto_tedwards.PointAffine
	x, y := a.X.Bytes(), a.Y.Bytes()
	out.X.SetBytes(x[:])
	out.Y.SetBytes(y[:])
	return &out, nil
}
