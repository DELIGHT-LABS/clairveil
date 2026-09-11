// Package scalarct implements opaque Montgomery-form residues modulo the
// BN254 twisted-Edwards prime-subgroup order.
package scalarct

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

var (
	ErrInvalidEncoding = errors.New("scalarct: invalid canonical BE32 encoding")
	ErrZeroScalar      = errors.New("scalarct: zero scalar is not permitted")
	ErrNilReader       = errors.New("scalarct: nil randomness reader")
)

var modulusQ = NonMontgomeryDomainFieldElement{
	0x677297dc392126f1, 0xab3eedb83920ee0a, 0x370a08b6d0302b0b, 0x060c89ce5c263405,
}

// Scalar is an opaque Montgomery-form residue modulo q. Its zero value is a
// valid residue, including a valid PoP response, but never a secret key.
type Scalar struct{ m MontgomeryDomainFieldElement }

// NonzeroScalar marks a Scalar whose value is nonzero. Its zero value is
// invalid and cannot be constructed through this package's constructors.
type NonzeroScalar struct{ scalar Scalar }

// Zero returns the additive identity.
func Zero() Scalar { return Scalar{} }

// One returns the multiplicative identity.
func One() (z Scalar) {
	SetOne(&z.m)
	return z
}

// Set assigns x to z.
func (z *Scalar) Set(x *Scalar) *Scalar { *z = *x; return z }

// Add sets z = x + y. It supports z aliasing either input.
func (z *Scalar) Add(x, y *Scalar) *Scalar { Add(&z.m, &x.m, &y.m); return z }

// Sub sets z = x - y. It supports z aliasing either input.
func (z *Scalar) Sub(x, y *Scalar) *Scalar { Sub(&z.m, &x.m, &y.m); return z }

// Neg sets z = -x. It supports z aliasing x.
func (z *Scalar) Neg(x *Scalar) *Scalar { Opp(&z.m, &x.m); return z }

// Mul sets z = x * y. It supports z aliasing either input.
func (z *Scalar) Mul(x, y *Scalar) *Scalar { Mul(&z.m, &x.m, &y.m); return z }

// Select sets z to x when bit is zero and y when bit is one. Callers must
// supply a bit in {0,1}; the fixed arithmetic schedule relies on that input.
func (z *Scalar) Select(bit uint64, x, y *Scalar) *Scalar {
	Selectznz((*[4]uint64)(&z.m), uint1(bit), (*[4]uint64)(&x.m), (*[4]uint64)(&y.m))
	return z
}

// Equal reports equality as 0 or 1.
func (z *Scalar) Equal(x *Scalar) uint64 {
	v := z.m[0] ^ x.m[0] | z.m[1] ^ x.m[1] | z.m[2] ^ x.m[2] | z.m[3] ^ x.m[3]
	return 1 ^ ((v | (0 - v)) >> 63)
}

// IsZero reports whether z is zero as 0 or 1.
func (z *Scalar) IsZero() uint64 {
	v := z.m[0] | z.m[1] | z.m[2] | z.m[3]
	return 1 ^ ((v | (0 - v)) >> 63)
}

// SetBytes decodes exactly one canonical big-endian q encoding.
func (z *Scalar) SetBytes(in []byte) error {
	if len(in) != 32 {
		*z = Scalar{}
		return ErrInvalidEncoding
	}
	var be [32]byte
	copy(be[:], in)
	decoded, ok := FromCanonicalBE(be)
	if !ok {
		*z = Scalar{}
		return ErrInvalidEncoding
	}
	*z = decoded
	return nil
}

// FromCanonicalBE decodes one canonical big-endian q encoding.
func FromCanonicalBE(in [32]byte) (Scalar, bool) {
	rawBE := rawBE32(in)
	if rawLessThan(&rawBE, &modulusQ) == 0 {
		return Scalar{}, false
	}
	var le [32]uint8
	for i := range in {
		le[i] = in[31-i]
	}
	var raw NonMontgomeryDomainFieldElement
	FromBytes((*[4]uint64)(&raw), &le)
	var z Scalar
	ToMontgomery(&z.m, &raw)
	return z, true
}

// Bytes returns the canonical big-endian 32-byte encoding of z.
func (z *Scalar) Bytes() [32]byte {
	var raw NonMontgomeryDomainFieldElement
	FromMontgomery(&raw, &z.m)
	var le [32]uint8
	ToBytes(&le, (*[4]uint64)(&raw))
	var out [32]byte
	for i := range out {
		out[i] = le[31-i]
	}
	return out
}

// Scalar returns a copy of n's scalar value. Callers must check IsValid before
// using a NonzeroScalar obtained from an arbitrary zero value.
func (n NonzeroScalar) Scalar() Scalar { return n.scalar }

// Bytes returns the canonical big-endian encoding of n.
func (n NonzeroScalar) Bytes() [32]byte { return n.scalar.Bytes() }

// IsValid reports whether n carries a nonzero scalar as 0 or 1.
func (n NonzeroScalar) IsValid() uint64 { return 1 ^ n.scalar.IsZero() }

// NewNonzeroScalar converts a nonzero scalar to its typed form.
func NewNonzeroScalar(s Scalar) (NonzeroScalar, error) {
	if s.IsZero() == 1 {
		return NonzeroScalar{}, ErrZeroScalar
	}
	return NonzeroScalar{scalar: s}, nil
}

// ParseNonzeroScalarBE32 strictly decodes a nonzero canonical q scalar.
func ParseNonzeroScalarBE32(in []byte) (NonzeroScalar, error) {
	var s Scalar
	if err := s.SetBytes(in); err != nil {
		return NonzeroScalar{}, err
	}
	return NewNonzeroScalar(s)
}

func rawBE32(in [32]byte) NonMontgomeryDomainFieldElement {
	return NonMontgomeryDomainFieldElement{
		binary.BigEndian.Uint64(in[24:32]), binary.BigEndian.Uint64(in[16:24]),
		binary.BigEndian.Uint64(in[8:16]), binary.BigEndian.Uint64(in[0:8]),
	}
}

func rawLessThan(x, y *NonMontgomeryDomainFieldElement) uint64 {
	var borrow uint64
	for i := 0; i < len(x); i++ {
		_, borrow = bits.Sub64(x[i], y[i], borrow)
	}
	return borrow
}
