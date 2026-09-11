package scalarct

// Reduce256 reduces an arbitrary big-endian 256-bit value modulo q with
// exactly 256 double, add-one, and select rounds.
func Reduce256(in [32]byte) (z Scalar) {
	one := One()
	for i := 0; i < 256; i++ {
		var doubled, incremented Scalar
		doubled.Add(&z, &z)
		incremented.Add(&doubled, &one)
		z.Select(uint64((in[i/8]>>uint(7-i%8))&1), &doubled, &incremented)
	}
	return z
}

// DeriveIdentityScalarSeed32 preserves seed mod q and maps only zero to one.
func DeriveIdentityScalarSeed32(seed [32]byte) NonzeroScalar {
	z := Reduce256(seed)
	one := One()
	z.Select(z.IsZero(), &z, &one)
	return NonzeroScalar{scalar: z}
}

// PoPResponse computes k + (h mod q) * a. A zero response is valid.
func PoPResponse(k, a Scalar, h [32]byte) (z Scalar) {
	c := Reduce256(h)
	z.Mul(&c, &a)
	z.Add(&z, &k)
	return z
}
