// Package bn254fr provides opaque Montgomery-form elements of BN254 Fr.
package bn254fr

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

var (
	// ErrInvalidEncoding reports a non-exact or non-canonical field encoding.
	ErrInvalidEncoding = errors.New("bn254fr: invalid canonical BE32 encoding")
)

var modulus = NonMontgomeryDomainFieldElement{
	0x43e1f593f0000001, 0x2833e84879b97091, 0xb85045b68181585d, 0x30644e72e131a029,
}

// Element is an opaque Montgomery-form Fr residue. Its zero value is zero.
type Element struct{ m MontgomeryDomainFieldElement }

// Zero returns the additive identity.
func Zero() Element { return Element{} }

// One returns the multiplicative identity.
func One() (z Element) {
	SetOne(&z.m)
	return z
}

// Uint64 returns x as an Fr element.
func Uint64(x uint64) (z Element) {
	var raw NonMontgomeryDomainFieldElement
	raw[0] = x
	ToMontgomery(&z.m, &raw)
	return z
}

// Set assigns x to z.
func (z *Element) Set(x *Element) *Element { *z = *x; return z }

// Add sets z = x + y. It supports z aliasing either input.
func (z *Element) Add(x, y *Element) *Element { Add(&z.m, &x.m, &y.m); return z }

// Sub sets z = x - y. It supports z aliasing either input.
func (z *Element) Sub(x, y *Element) *Element { Sub(&z.m, &x.m, &y.m); return z }

// Neg sets z = -x. It supports z aliasing x.
func (z *Element) Neg(x *Element) *Element { Opp(&z.m, &x.m); return z }

// Mul sets z = x * y. It supports z aliasing either input.
func (z *Element) Mul(x, y *Element) *Element { Mul(&z.m, &x.m, &y.m); return z }

// Square sets z = x². It supports z aliasing x.
func (z *Element) Square(x *Element) *Element { Square(&z.m, &x.m); return z }

// Select sets z to x when bit is zero and y when bit is one. Callers must
// supply a bit in {0,1}; it is intentionally an internal fixed-schedule API.
func (z *Element) Select(bit uint64, x, y *Element) *Element {
	Selectznz((*[4]uint64)(&z.m), uint1(bit), (*[4]uint64)(&x.m), (*[4]uint64)(&y.m))
	return z
}

// Equal reports equality as 0 or 1.
func (z *Element) Equal(x *Element) uint64 {
	v := z.m[0] ^ x.m[0] | z.m[1] ^ x.m[1] | z.m[2] ^ x.m[2] | z.m[3] ^ x.m[3]
	return 1 ^ ((v | (0 - v)) >> 63)
}

// IsZero reports whether z is zero as 0 or 1.
func (z *Element) IsZero() uint64 {
	v := z.m[0] | z.m[1] | z.m[2] | z.m[3]
	return 1 ^ ((v | (0 - v)) >> 63)
}

// SetBytes decodes exactly one canonical big-endian 32-byte Fr encoding.
func (z *Element) SetBytes(in []byte) error {
	if len(in) != 32 {
		*z = Element{}
		return ErrInvalidEncoding
	}
	var be [32]byte
	copy(be[:], in)
	decoded, ok := FromCanonicalBE(be)
	if !ok {
		*z = Element{}
		return ErrInvalidEncoding
	}
	*z = decoded
	return nil
}

// FromCanonicalBE decodes one canonical big-endian Fr encoding.
func FromCanonicalBE(in [32]byte) (Element, bool) {
	rawBE := scalarRawBE(in)
	if rawLessThan(&rawBE, &modulus) == 0 {
		return Element{}, false
	}
	var le [32]uint8
	for i := range in {
		le[i] = in[31-i]
	}
	var raw NonMontgomeryDomainFieldElement
	FromBytes((*[4]uint64)(&raw), &le)
	var z Element
	ToMontgomery(&z.m, &raw)
	return z, true
}

// Bytes returns the canonical big-endian 32-byte encoding of z.
func (z *Element) Bytes() [32]byte {
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

// CanonicalBE is retained as an explicit name for protocol codecs.
func (z *Element) CanonicalBE() [32]byte { return z.Bytes() }

func rawLessThan(x, y *NonMontgomeryDomainFieldElement) uint64 {
	var borrow uint64
	for i := 0; i < len(x); i++ {
		_, borrow = bits.Sub64(x[i], y[i], borrow)
	}
	return borrow
}

// scalarRawBE is kept here so the canonical comparison and byte order are
// visible next to the generated little-endian codec.
func scalarRawBE(in [32]byte) NonMontgomeryDomainFieldElement {
	return NonMontgomeryDomainFieldElement{
		binary.BigEndian.Uint64(in[24:32]), binary.BigEndian.Uint64(in[16:24]),
		binary.BigEndian.Uint64(in[8:16]), binary.BigEndian.Uint64(in[0:8]),
	}
}
