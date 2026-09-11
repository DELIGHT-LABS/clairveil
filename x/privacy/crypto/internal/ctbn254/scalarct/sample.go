package scalarct

import "io"

// SampleScalar samples uniformly from Zq or Zq*. Each rejected candidate is
// replaced with an independent 32-byte draw. Reader failures are returned.
func SampleScalar(reader io.Reader, nonzero bool) (Scalar, error) {
	if reader == nil {
		return Scalar{}, ErrNilReader
	}
	for {
		var in [32]byte
		if _, err := io.ReadFull(reader, in[:]); err != nil {
			clear(in[:])
			return Scalar{}, err
		}
		in[0] &= 7
		z, ok := FromCanonicalBE(in)
		if !ok || (nonzero && z.IsZero() == 1) {
			z = Scalar{}
			clear(in[:])
			continue
		}
		clear(in[:])
		return z, nil
	}
}

// SampleNonzeroScalar samples a value from q* and returns the nonzero type.
func SampleNonzeroScalar(reader io.Reader) (NonzeroScalar, error) {
	z, err := SampleScalar(reader, true)
	if err != nil {
		return NonzeroScalar{}, err
	}
	return NewNonzeroScalar(z)
}
