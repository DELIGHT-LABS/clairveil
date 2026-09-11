package edwardsct

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"
)

// DecodePoint64 accepts only Point64(P)=F32(x)||F32(y), an on-curve,
// nonidentity prime-subgroup point. It is the only public Point64 decoder.
func DecodePoint64(in []byte) (Point, error) {
	if len(in) != 64 {
		return Point{}, fmt.Errorf("%w: Point64 must be 64 bytes", ErrInvalidPoint)
	}
	var xb, yb [32]byte
	copy(xb[:], in[:32])
	copy(yb[:], in[32:])
	x, ok := frct.FromCanonicalBE(xb)
	if !ok {
		return Point{}, fmt.Errorf("%w: noncanonical x", ErrInvalidPoint)
	}
	y, ok := frct.FromCanonicalBE(yb)
	if !ok {
		return Point{}, fmt.Errorf("%w: noncanonical y", ErrInvalidPoint)
	}
	p, err := FromAffine(Affine{X: x, Y: y})
	if err != nil {
		return Point{}, err
	}
	if p.IsIdentity() {
		return Point{}, fmt.Errorf("%w: identity", ErrInvalidPoint)
	}
	if !p.inPrimeSubgroup() {
		return Point{}, fmt.Errorf("%w: not in subgroup", ErrInvalidPoint)
	}
	return p, nil
}

// DecodeLegacyCompressed parses the existing 32-byte EdDSA representation:
// little-endian Y and a high sign bit in the final byte.
func DecodeLegacyCompressed(in []byte) (Point, error) {
	if len(in) != 32 {
		return Point{}, fmt.Errorf("%w: compressed point must be 32 bytes", ErrInvalidPoint)
	}
	var le, yb [32]byte
	copy(le[:], in)
	sign := le[31] >> 7
	le[31] &= 0x7f
	for i := range le {
		yb[i] = le[len(le)-1-i]
	}
	y, ok := frct.FromCanonicalBE(yb)
	if !ok {
		return Point{}, fmt.Errorf("%w: noncanonical y", ErrInvalidPoint)
	}
	// x²=(1-y²)/(a-dy²), a=-1.
	var one, y2, numerator, denominator, inv, x2 frct.Element
	one = frct.One()
	y2.Square(&y)
	numerator.Sub(&one, &y2)
	denominator.Mul(&curveD, &y2)
	denominator.Neg(&denominator)
	denominator.Sub(&denominator, &one)
	if !inv.Inverse(&denominator) {
		return Point{}, fmt.Errorf("%w: singular compressed point", ErrInvalidPoint)
	}
	x2.Mul(&numerator, &inv)
	x, ok := frct.SqrtPublic(&x2)
	if !ok {
		return Point{}, fmt.Errorf("%w: x is not square", ErrInvalidPoint)
	}
	if legacySignBit(&x) != uint64(sign) {
		x.Neg(&x)
	}
	p, err := FromAffine(Affine{X: x, Y: y})
	if err != nil {
		return Point{}, err
	}
	if p.IsIdentity() || !p.inPrimeSubgroup() {
		return Point{}, ErrInvalidPoint
	}
	encoded, err := p.CompressLegacy()
	if err != nil || string(encoded[:]) != string(in) {
		return Point{}, ErrInvalidPoint
	}
	return p, nil
}

// EncodePoint64 returns canonical F32(x)||F32(y). It succeeds only for a
// normalized valid internal point.
func (p *Point) EncodePoint64() ([64]byte, error) {
	a, ok := p.Affine()
	if !ok {
		return [64]byte{}, fmt.Errorf("%w: zero Z", ErrInvalidPoint)
	}
	x, y := a.X.Bytes(), a.Y.Bytes()
	var out [64]byte
	copy(out[:32], x[:])
	copy(out[32:], y[:])
	return out, nil
}

// CompressLegacy preserves the existing EdDSA wire: little-endian Y with the
// high bit of its final byte set when X is lexicographically largest.
func (p *Point) CompressLegacy() ([32]byte, error) {
	a, ok := p.Affine()
	if !ok {
		return [32]byte{}, fmt.Errorf("%w: zero Z", ErrInvalidPoint)
	}
	_, y := a.X.Bytes(), a.Y.Bytes()
	negative := legacySignBit(&a.X)
	var out [32]byte
	for i := range y {
		out[i] = y[len(y)-1-i]
	}
	out[31] |= byte(negative << 7)
	return out, nil
}

func legacySignBit(x *frct.Element) uint64 {
	bx := x.Bytes()
	var neg frct.Element
	neg.Neg(x)
	bn := neg.Bytes()
	var borrow uint64
	for i := 3; i >= 0; i-- {
		_, borrow = bits.Sub64(binary.BigEndian.Uint64(bn[i*8:]), binary.BigEndian.Uint64(bx[i*8:]), borrow)
	}
	return borrow
}
