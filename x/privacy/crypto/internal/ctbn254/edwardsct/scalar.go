package edwardsct

import (
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
)

var subgroupOrderBE = [32]byte{0x06, 0x0c, 0x89, 0xce, 0x5c, 0x26, 0x34, 0x05, 0x37, 0x0a, 0x08, 0xb6, 0xd0, 0x30, 0x2b, 0x0b, 0xab, 0x3e, 0xed, 0xb8, 0x39, 0x20, 0xee, 0x0a, 0x67, 0x72, 0x97, 0xdc, 0x39, 0x21, 0x26, 0xf1}

// ScalarMult uses the same 256-round double/add/select schedule for all
// scalar values. No window/table lookup is used.
func (p *Point) ScalarMult(a *Point, s scalarct.Scalar) *Point {
	defer secretmem.Clear(&s)
	b := s.Bytes()
	defer secretmem.Clear(&b)
	return p.scalarMultBE(a, b)
}

func (p *Point) ScalarMultNonzero(a *Point, s scalarct.NonzeroScalar) *Point {
	defer secretmem.Clear(&s)
	b := s.Bytes()
	defer secretmem.Clear(&b)
	return p.scalarMultBE(a, b)
}

func (p *Point) scalarMultBE(a *Point, bits [32]byte) *Point {
	defer secretmem.Clear(&bits)
	result := Identity()
	for i := 0; i < 256; i++ {
		var doubled, added Point
		doubled.Double(&result)
		added.Add(&doubled, a)
		result.selectPoint(uint64((bits[i/8]>>uint(7-i%8))&1), &doubled, &added)
	}
	*p = result
	return p
}

func (p *Point) inPrimeSubgroup() bool {
	var multiple Point
	multiple.scalarMultBE(p, subgroupOrderBE)
	return multiple.IsIdentity()
}
