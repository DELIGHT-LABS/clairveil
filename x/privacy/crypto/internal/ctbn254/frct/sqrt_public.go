package bn254fr

// sqrtExponent is (s-1)/2 where p-1 = s*2^28. Sqrt is used only for the
// legacy public compressed-point decoder; secret curve arithmetic never calls
// it.
var sqrtExponent = [32]byte{0x00, 0x00, 0x00, 0x01, 0x83, 0x22, 0x73, 0x97, 0x09, 0x8d, 0x01, 0x4d, 0xc2, 0x82, 0x2d, 0xb4, 0x0c, 0x0a, 0xc2, 0xe9, 0x41, 0x9f, 0x42, 0x43, 0xcd, 0xcb, 0x84, 0x8a, 0x1f, 0x0f, 0xac, 0x9f}

var tonelliG = mustPublicField([32]byte{0x18, 0x8c, 0x51, 0xb4, 0xc8, 0x95, 0x31, 0x78, 0xe1, 0xed, 0x0c, 0xc8, 0x72, 0x3d, 0x5c, 0x5f, 0x67, 0xcf, 0x5a, 0xcd, 0x0e, 0x1a, 0x5d, 0x5d, 0xb8, 0xdd, 0xe8, 0x49, 0x88, 0x59, 0x08, 0x82})

func mustPublicField(in [32]byte) Element {
	z, ok := FromCanonicalBE(in)
	if !ok {
		panic("bn254fr: invalid public sqrt constant")
	}
	return z
}

func pow256(x *Element, exponent [32]byte) (z Element) {
	z = One()
	for i := 0; i < 256; i++ {
		var squared, product Element
		squared.Square(&z)
		product.Mul(&squared, x)
		z.Select(uint64((exponent[i/8]>>uint(7-i%8))&1), &squared, &product)
	}
	return z
}

// SqrtPublic returns one square root of x and whether it exists. This public
// parser helper intentionally has no secret-input timing contract.
func SqrtPublic(x *Element) (Element, bool) {
	if x.IsZero() == 1 {
		return Zero(), true
	}
	w := pow256(x, sqrtExponent)
	var y, b Element
	y.Mul(x, &w)
	b.Mul(&w, &y)
	var legendre Element = b
	for i := 0; i < 27; i++ {
		legendre.Square(&legendre)
	}
	one := One()
	if legendre.Equal(&one) == 0 {
		return Element{}, false
	}
	g, r := tonelliG, 28
	for b.Equal(&one) == 0 {
		t, m := b, 0
		for t.Equal(&one) == 0 && m < r {
			t.Square(&t)
			m++
		}
		if m == r {
			return Element{}, false
		}
		t = g
		for i := 0; i < r-m-1; i++ {
			t.Square(&t)
		}
		g.Square(&t)
		y.Mul(&y, &t)
		b.Mul(&b, &g)
		r = m
	}
	return y, true
}
