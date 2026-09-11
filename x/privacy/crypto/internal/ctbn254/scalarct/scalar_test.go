package scalarct

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"os"
	"testing"
)

var scalarModulus, _ = new(big.Int).SetString("2736030358979909402780800718157159386076813972158567259200215660948447373041", 10)

func TestPinnedGeneratedSourceHash(t *testing.T) {
	source, err := os.ReadFile("fiat_q64.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != "cacf057e59044206a35180851d7de7da60cd02c68df984b9e89e6fd9369b534e" {
		t.Fatalf("pinned generated source hash = %s", got)
	}
}

func scalarBE(x *big.Int) (out [32]byte) { x.FillBytes(out[:]); return out }

func scalarValue(t *testing.T, x *big.Int) Scalar {
	t.Helper()
	z, ok := FromCanonicalBE(scalarBE(x))
	if !ok {
		t.Fatal("test oracle produced non-canonical scalar")
	}
	return z
}

func scalarBig(z *Scalar) *big.Int {
	b := z.Bytes()
	return new(big.Int).SetBytes(b[:])
}

func requireScalarEqual(t *testing.T, got *Scalar, want *big.Int) {
	t.Helper()
	want = new(big.Int).Mod(want, scalarModulus)
	if scalarBig(got).Cmp(want) != 0 {
		t.Fatalf("scalar differential mismatch: got %x want %x", got.Bytes(), scalarBE(want))
	}
}

func TestScalarCanonicalEncodingAndNonzeroType(t *testing.T) {
	qm1 := new(big.Int).Sub(new(big.Int).Set(scalarModulus), big.NewInt(1))
	values := []*big.Int{big.NewInt(0), big.NewInt(1), qm1}
	for _, shift := range []uint{64, 128, 192} {
		base := new(big.Int).Lsh(big.NewInt(1), shift)
		values = append(values, new(big.Int).Sub(new(big.Int).Set(base), big.NewInt(1)), base, new(big.Int).Add(base, big.NewInt(1)))
	}
	for _, x := range values {
		z, ok := FromCanonicalBE(scalarBE(x))
		if !ok || scalarBig(&z).Cmp(x) != 0 {
			t.Fatalf("canonical boundary rejected: %s", x)
		}
	}
	if _, ok := FromCanonicalBE(scalarBE(scalarModulus)); ok {
		t.Fatal("q accepted as scalar")
	}
	if _, ok := FromCanonicalBE(scalarBE(new(big.Int).Add(new(big.Int).Set(scalarModulus), big.NewInt(1)))); ok {
		t.Fatal("q plus one accepted as scalar")
	}
	max256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if _, ok := FromCanonicalBE(scalarBE(max256)); ok {
		t.Fatal("maximum 256-bit value accepted as scalar")
	}
	var z Scalar
	if err := z.SetBytes(make([]byte, 31)); err == nil || z.IsZero() != 1 {
		t.Fatal("short encoding must fail and clear destination")
	}
	if err := z.SetBytes(make([]byte, 33)); err == nil || z.IsZero() != 1 {
		t.Fatal("long encoding must fail and clear destination")
	}
	zeroBytes := make([]byte, 32)
	if _, err := ParseNonzeroScalarBE32(zeroBytes); !errors.Is(err, ErrZeroScalar) {
		t.Fatalf("zero scalar error = %v", err)
	}
	if (NonzeroScalar{}).IsValid() != 0 {
		t.Fatal("NonzeroScalar zero value must be invalid")
	}
	identity := DeriveIdentityScalarSeed32([32]byte{})
	one := One()
	identityScalar := identity.Scalar()
	if identity.IsValid() != 1 || identityScalar.Equal(&one) != 1 {
		t.Fatal("zero identity seed must map to one")
	}
	qSeed := scalarBE(scalarModulus)
	if got := DeriveIdentityScalarSeed32(qSeed).Scalar(); got.Equal(&one) != 1 {
		t.Fatal("q identity seed must map to one")
	}
}

func TestScalarArithmeticReduceAndPoP(t *testing.T) {
	values := []*big.Int{big.NewInt(0), big.NewInt(1), new(big.Int).Sub(new(big.Int).Set(scalarModulus), big.NewInt(1))}
	rng := rand.New(rand.NewSource(0x5343414c4152))
	for i := 0; i < 48; i++ {
		b := make([]byte, 32)
		if _, err := rng.Read(b); err != nil {
			t.Fatal(err)
		}
		input := new(big.Int).SetBytes(b)
		values = append(values, new(big.Int).Mod(new(big.Int).Set(input), scalarModulus))
		var fixed [32]byte
		copy(fixed[:], b)
		got := Reduce256(fixed)
		requireScalarEqual(t, &got, input)
	}
	for _, x := range values {
		a := scalarValue(t, x)
		for _, y := range values {
			b := scalarValue(t, y)
			var z Scalar
			z.Add(&a, &b)
			requireScalarEqual(t, &z, new(big.Int).Add(x, y))
			z = a
			z.Add(&z, &b)
			requireScalarEqual(t, &z, new(big.Int).Add(x, y))
			z = b
			z.Add(&a, &z)
			requireScalarEqual(t, &z, new(big.Int).Add(x, y))
			z.Sub(&a, &b)
			requireScalarEqual(t, &z, new(big.Int).Sub(x, y))
			z = a
			z.Sub(&z, &b)
			requireScalarEqual(t, &z, new(big.Int).Sub(x, y))
			z = b
			z.Sub(&a, &z)
			requireScalarEqual(t, &z, new(big.Int).Sub(x, y))
			z.Mul(&a, &b)
			requireScalarEqual(t, &z, new(big.Int).Mul(x, y))
			z = a
			z.Mul(&z, &b)
			requireScalarEqual(t, &z, new(big.Int).Mul(x, y))
			z = b
			z.Mul(&a, &z)
			requireScalarEqual(t, &z, new(big.Int).Mul(x, y))
			z.Neg(&a)
			requireScalarEqual(t, &z, new(big.Int).Neg(x))
			z = a
			z.Neg(&z)
			requireScalarEqual(t, &z, new(big.Int).Neg(x))
			z.Select(0, &a, &b)
			requireScalarEqual(t, &z, x)
			z.Select(1, &a, &b)
			requireScalarEqual(t, &z, y)
			z = a
			z.Select(0, &z, &b)
			requireScalarEqual(t, &z, x)
			z = b
			z.Select(1, &a, &z)
			requireScalarEqual(t, &z, y)
		}
	}
	qMinusOne := new(big.Int).Sub(new(big.Int).Set(scalarModulus), big.NewInt(1))
	h := scalarBE(qMinusOne)
	one := scalarValue(t, big.NewInt(1))
	response := PoPResponse(one, one, h)
	if response.IsZero() != 1 {
		t.Fatal("zero PoP response must remain a valid scalar")
	}
	for _, x := range []*big.Int{big.NewInt(0), new(big.Int).Set(scalarModulus), new(big.Int).Mul(new(big.Int).Set(scalarModulus), big.NewInt(2)), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))} {
		input := scalarBE(x)
		got := Reduce256(input)
		requireScalarEqual(t, &got, x)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type partialFailingReader struct{}

func (partialFailingReader) Read(p []byte) (int, error) {
	if len(p) > 0 {
		p[0] = 1
		return 1, io.ErrUnexpectedEOF
	}
	return 0, io.ErrUnexpectedEOF
}

func TestSampleScalarPoliciesAndReaderError(t *testing.T) {
	q := scalarBE(scalarModulus)
	zero := [32]byte{}
	one := scalarBE(big.NewInt(1))
	stream := append(append(q[:], zero[:]...), one[:]...)
	z, err := SampleScalar(bytes.NewReader(stream), true)
	wantOne := One()
	if err != nil || z.Equal(&wantOne) != 1 {
		t.Fatalf("nonzero sampler did not reject q and zero: z=%v err=%v", z, err)
	}
	if _, err := SampleScalar(failingReader{}, false); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("reader failure = %v", err)
	}
	if _, err := SampleNonzeroScalar(failingReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("typed reader failure = %v", err)
	}
	if _, err := SampleScalar(nil, false); !errors.Is(err, ErrNilReader) {
		t.Fatalf("nil reader failure = %v", err)
	}
	if _, err := SampleScalar(partialFailingReader{}, false); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial reader failure = %v", err)
	}
	zeroSample, err := SampleScalar(bytes.NewReader(zero[:]), false)
	if err != nil || zeroSample.IsZero() != 1 {
		t.Fatalf("zero-allowed sampler = %v, %v", zeroSample, err)
	}
	masked := one
	masked[0] |= 0xf8
	maskedSample, err := SampleScalar(bytes.NewReader(masked[:]), false)
	if err != nil || maskedSample.Equal(&wantOne) != 1 {
		t.Fatalf("sampler high-bit mask = %v, %v", maskedSample, err)
	}
}

func TestScalarSecretFormattingIsRedacted(t *testing.T) {
	z := One()
	for _, format := range []string{"%v", "%+v", "%#v", "%x", "%d", "%b"} {
		if got := fmt.Sprintf(format, z); got != "scalarct.Scalar(<redacted>)" {
			t.Fatalf("unexpected scalar formatting %q: %q", format, got)
		}
		if got := fmt.Sprintf(format, &z); got != "scalarct.Scalar(<redacted>)" {
			t.Fatalf("unexpected scalar pointer formatting %q: %q", format, got)
		}
	}
	if _, err := json.Marshal(z); err == nil {
		t.Fatal("JSON marshaling a secret scalar must fail")
	}
	n, err := NewNonzeroScalar(z)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%x", "%d", "%b"} {
		if got := fmt.Sprintf(format, n); got != "scalarct.NonzeroScalar(<redacted>)" {
			t.Fatalf("unexpected nonzero formatting %q: %q", format, got)
		}
		if got := fmt.Sprintf(format, &n); got != "scalarct.NonzeroScalar(<redacted>)" {
			t.Fatalf("unexpected nonzero pointer formatting %q: %q", format, got)
		}
	}
	if _, err := json.Marshal(n); err == nil {
		t.Fatal("JSON marshaling a nonzero secret scalar must fail")
	}
	if _, err := n.MarshalText(); err == nil {
		t.Fatal("text marshaling a nonzero secret scalar must fail")
	}
}
