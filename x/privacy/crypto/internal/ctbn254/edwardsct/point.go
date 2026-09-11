// Package edwardsct implements the BN254 twisted-Edwards subgroup using
// complete extended coordinates. It deliberately has no dependency on the
// gnark native curve package: callers use this package whenever a scalar is
// private.
package edwardsct

import (
	"errors"

	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"
)

var (
	ErrInvalidPoint = errors.New("edwardsct: invalid prime-subgroup point")
)

// Affine is an affine twisted-Edwards point. It is intended for validated
// public inputs and for conversion at an explicit wire boundary.
type Affine struct {
	X frct.Element
	Y frct.Element
}

// Point is an extended point (X:Y:Z:T), where x=X/Z, y=Y/Z and XY=ZT.
// Its fields remain private so callers cannot manufacture an invalid
// extended representation.
type Point struct {
	x, y, z, t frct.Element
}

var (
	curveD    = mustField("1aee90f15f2189693df072d799fd11fc039b2959ebb7c867d075ca8cf4d7eb8e")
	basePoint = func() Point {
		x := mustField("1561ff836ce19d358a4eb7a4c199e94c377c749ae6f2a277f1f9195afe553f9f")
		y := mustField("25797203f7a0b24925572e1cd16bf9edfce0051fb9e133774b3c257a872d7d8b")
		p, err := FromAffine(Affine{X: x, Y: y})
		if err != nil {
			panic(err)
		}
		return p
	}()
)

func mustField(s string) frct.Element {
	// The fixed constants are checked at package construction and never accept
	// caller-controlled text. Keep the BE bytes visible in source instead of
	// importing a big integer parser into the secret backend.
	var in [32]byte
	for i := 0; i < len(in); i++ {
		in[i] = fromHex(s[2*i])<<4 | fromHex(s[2*i+1])
	}
	z, ok := frct.FromCanonicalBE(in)
	if !ok {
		panic("edwardsct: invalid fixed field constant")
	}
	return z
}

func fromHex(c byte) byte {
	if c >= '0' && c <= '9' {
		return c - '0'
	}
	if c >= 'a' && c <= 'f' {
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// Identity returns (0,1) in extended coordinates.
func Identity() (p Point) {
	p.y = frct.One()
	p.z = frct.One()
	return p
}

// Base returns the fixed prime-subgroup generator.
func Base() Point {
	return basePoint
}

// FromAffine validates the curve equation and returns a complete extended
// representation. It permits the identity so scalar multiplication can use
// it internally; external decoders reject it separately.
func FromAffine(a Affine) (Point, error) {
	if !a.IsOnCurve() {
		return Point{}, ErrInvalidPoint
	}
	var p Point
	p.x.Set(&a.X)
	p.y.Set(&a.Y)
	p.z = frct.One()
	p.t.Mul(&a.X, &a.Y)
	return p, nil
}

func (a *Affine) IsOnCurve() bool {
	// -x²+y² = 1+d*x²*y²
	var x2, y2, lhs, rhs, tmp frct.Element
	x2.Square(&a.X)
	y2.Square(&a.Y)
	lhs.Sub(&y2, &x2)
	tmp.Mul(&x2, &y2).Mul(&tmp, &curveD)
	rhs = frct.One()
	rhs.Add(&rhs, &tmp)
	return lhs.Equal(&rhs) == 1
}

// IsIdentity reports whether p represents (0,1). It is used only after a
// public point has been decoded/validated.
func (p *Point) IsIdentity() bool {
	var zero frct.Element
	return (p.x.IsZero() & p.y.Equal(&p.z) & p.t.Equal(&zero)) == 1
}

// Equal compares projective points without normalization.
func (p *Point) Equal(q *Point) bool {
	var lx, rx, ly, ry frct.Element
	lx.Mul(&p.x, &q.z)
	rx.Mul(&q.x, &p.z)
	ly.Mul(&p.y, &q.z)
	ry.Mul(&q.y, &p.z)
	return (lx.Equal(&rx) & ly.Equal(&ry)) == 1
}

// Add performs the complete extended-coordinate addition formula for a=-1.
func (p *Point) Add(a, b *Point) *Point {
	var A, B, C, D, E, F, G, H, u, v frct.Element
	u.Sub(&a.y, &a.x)
	v.Sub(&b.y, &b.x)
	A.Mul(&u, &v)
	u.Add(&a.y, &a.x)
	v.Add(&b.y, &b.x)
	B.Mul(&u, &v)
	C.Mul(&a.t, &b.t).Mul(&C, &curveD)
	C.Add(&C, &C)
	D.Mul(&a.z, &b.z)
	D.Add(&D, &D)
	E.Sub(&B, &A)
	F.Sub(&D, &C)
	G.Add(&D, &C)
	H.Add(&B, &A)
	p.x.Mul(&E, &F)
	p.y.Mul(&G, &H)
	p.t.Mul(&E, &H)
	p.z.Mul(&F, &G)
	return p
}

// Double performs the a=-1 extended-coordinate doubling formula.
func (p *Point) Double(a *Point) *Point {
	var A, B, C, D, E, F, G, H, u frct.Element
	A.Square(&a.x)
	B.Square(&a.y)
	C.Square(&a.z)
	C.Add(&C, &C)
	D.Neg(&A)
	u.Add(&a.x, &a.y)
	E.Square(&u)
	E.Sub(&E, &A)
	E.Sub(&E, &B)
	G.Add(&D, &B)
	F.Sub(&G, &C)
	H.Sub(&D, &B)
	p.x.Mul(&E, &F)
	p.y.Mul(&G, &H)
	p.t.Mul(&E, &H)
	p.z.Mul(&F, &G)
	return p
}

func (p *Point) selectPoint(bit uint64, x, y *Point) *Point {
	p.x.Select(bit, &x.x, &y.x)
	p.y.Select(bit, &x.y, &y.y)
	p.z.Select(bit, &x.z, &y.z)
	p.t.Select(bit, &x.t, &y.t)
	return p
}

// Affine normalizes p with the fixed-schedule Inv0 operation. A valid
// subgroup point has Z != 0; false is returned for malformed internal input.
func (p *Point) Affine() (Affine, bool) {
	var inv frct.Element
	if !inv.Inverse(&p.z) {
		return Affine{}, false
	}
	var a Affine
	a.X.Mul(&p.x, &inv)
	a.Y.Mul(&p.y, &inv)
	return a, true
}
