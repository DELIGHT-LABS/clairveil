package bn254fr

// Reduce256 reduces an arbitrary big-endian 256-bit value modulo Fr using a
// fixed 256-round double, add-one and select schedule. It is used only for
// protocol hash-to-field inputs; canonical wire decoding remains SetBytes.
func Reduce256(in [32]byte) (z Element) {
	one := One()
	for i := 0; i < 256; i++ {
		var doubled, incremented Element
		doubled.Add(&z, &z)
		incremented.Add(&doubled, &one)
		z.Select(uint64((in[i/8]>>uint(7-i%8))&1), &doubled, &incremented)
	}
	return z
}
