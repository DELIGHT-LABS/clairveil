package bn254fr

var pMinus2 = [4]uint64{
	0x43e1f593efffffff, 0x2833e84879b97091, 0xb85045b68181585d, 0x30644e72e131a029,
}

// Inv0 sets z = x^(p-2). It always performs 256 rounds and maps zero to
// zero. Callers that require an inverse must establish nonzero separately.
func (z *Element) Inv0(x *Element) *Element {
	r := One()
	for i := 255; i >= 0; i-- {
		var square, product Element
		square.Square(&r)
		product.Mul(&square, x)
		r.Select((pMinus2[i/64]>>uint(i%64))&1, &square, &product)
	}
	*z = r
	return z
}

// Inverse sets z to the inverse of x and reports whether x was nonzero. Its
// arithmetic schedule is the same fixed 256-round Inv0 schedule. For zero it
// leaves z equal to zero and reports false, so zero is never a successful
// inverse result.
func (z *Element) Inverse(x *Element) bool {
	z.Inv0(x)
	return x.IsZero() == 0
}
