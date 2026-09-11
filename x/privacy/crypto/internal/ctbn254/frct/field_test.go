package bn254fr

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"testing"
)

var frModulus, _ = new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)

func TestPinnedGeneratedSourceHash(t *testing.T) {
	source, err := os.ReadFile("fiat_p64.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != "e5ab56d5bc1b0fa0ef6f94885c5a279680620085770c3821b9c9d1696711fb1c" {
		t.Fatalf("pinned generated source hash = %s", got)
	}
}

func frBE(x *big.Int) (out [32]byte) { x.FillBytes(out[:]); return out }

func frElement(t *testing.T, x *big.Int) Element {
	t.Helper()
	z, ok := FromCanonicalBE(frBE(x))
	if !ok {
		t.Fatal("test oracle produced non-canonical Fr value")
	}
	return z
}

func frBig(z *Element) *big.Int {
	b := z.Bytes()
	return new(big.Int).SetBytes(b[:])
}

func requireFrEqual(t *testing.T, got *Element, want *big.Int) {
	t.Helper()
	want = new(big.Int).Mod(want, frModulus)
	if frBig(got).Cmp(want) != 0 {
		t.Fatalf("field differential mismatch: got %x want %x", got.Bytes(), frBE(want))
	}
}

func TestFrCanonicalEncoding(t *testing.T) {
	pm1 := new(big.Int).Sub(new(big.Int).Set(frModulus), big.NewInt(1))
	values := []*big.Int{big.NewInt(0), big.NewInt(1), pm1}
	for _, shift := range []uint{64, 128, 192} {
		base := new(big.Int).Lsh(big.NewInt(1), shift)
		values = append(values, new(big.Int).Sub(new(big.Int).Set(base), big.NewInt(1)), base, new(big.Int).Add(base, big.NewInt(1)))
	}
	for _, x := range values {
		z, ok := FromCanonicalBE(frBE(x))
		if !ok || frBig(&z).Cmp(x) != 0 {
			t.Fatalf("canonical boundary rejected: %s", x)
		}
	}
	if _, ok := FromCanonicalBE(frBE(frModulus)); ok {
		t.Fatal("modulus accepted as Fr")
	}
	if _, ok := FromCanonicalBE(frBE(new(big.Int).Add(new(big.Int).Set(frModulus), big.NewInt(1)))); ok {
		t.Fatal("modulus plus one accepted as Fr")
	}
	max256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if _, ok := FromCanonicalBE(frBE(max256)); ok {
		t.Fatal("maximum 256-bit value accepted as Fr")
	}
	var z Element
	if err := z.SetBytes(make([]byte, 31)); err == nil || z.IsZero() != 1 {
		t.Fatal("short encoding must fail and clear destination")
	}
	if err := z.SetBytes(make([]byte, 33)); err == nil || z.IsZero() != 1 {
		t.Fatal("long encoding must fail and clear destination")
	}
	modulusBytes := frBE(frModulus)
	if err := z.SetBytes(modulusBytes[:]); err == nil || z.IsZero() != 1 {
		t.Fatal("non-canonical encoding must fail and clear destination")
	}
	zero := Zero()
	if z.Inverse(&zero) || z.IsZero() != 1 {
		t.Fatal("zero must not produce a successful inverse")
	}
}

func TestFrArithmeticDifferentialAliasesAndInverse(t *testing.T) {
	values := []*big.Int{big.NewInt(0), big.NewInt(1), new(big.Int).Sub(new(big.Int).Set(frModulus), big.NewInt(1))}
	rng := rand.New(rand.NewSource(0x46524354))
	for i := 0; i < 40; i++ {
		b := make([]byte, 32)
		if _, err := rng.Read(b); err != nil {
			t.Fatal(err)
		}
		values = append(values, new(big.Int).Mod(new(big.Int).SetBytes(b), frModulus))
	}
	for _, x := range values {
		a := frElement(t, x)
		var inverse Element
		inverse.Inv0(&a)
		wantInverse := new(big.Int)
		if x.Sign() != 0 {
			wantInverse.ModInverse(x, frModulus)
		}
		requireFrEqual(t, &inverse, wantInverse)
		for _, y := range values {
			b := frElement(t, y)
			var z Element
			z.Add(&a, &b)
			requireFrEqual(t, &z, new(big.Int).Add(x, y))
			z = a
			z.Add(&z, &b)
			requireFrEqual(t, &z, new(big.Int).Add(x, y))
			z = b
			z.Add(&a, &z)
			requireFrEqual(t, &z, new(big.Int).Add(x, y))
			z.Sub(&a, &b)
			requireFrEqual(t, &z, new(big.Int).Sub(x, y))
			z = a
			z.Sub(&z, &b)
			requireFrEqual(t, &z, new(big.Int).Sub(x, y))
			z = b
			z.Sub(&a, &z)
			requireFrEqual(t, &z, new(big.Int).Sub(x, y))
			z.Neg(&a)
			requireFrEqual(t, &z, new(big.Int).Neg(x))
			z = a
			z.Neg(&z)
			requireFrEqual(t, &z, new(big.Int).Neg(x))
			z.Mul(&a, &b)
			requireFrEqual(t, &z, new(big.Int).Mul(x, y))
			z = a
			z.Mul(&z, &b)
			requireFrEqual(t, &z, new(big.Int).Mul(x, y))
			z = b
			z.Mul(&a, &z)
			requireFrEqual(t, &z, new(big.Int).Mul(x, y))
			z.Select(0, &a, &b)
			requireFrEqual(t, &z, x)
			z.Select(1, &a, &b)
			requireFrEqual(t, &z, y)
			z = a
			z.Select(0, &z, &b)
			requireFrEqual(t, &z, x)
			z = b
			z.Select(1, &a, &z)
			requireFrEqual(t, &z, y)
			z.Square(&a)
			requireFrEqual(t, &z, new(big.Int).Mul(x, x))
			z = a
			z.Square(&z)
			requireFrEqual(t, &z, new(big.Int).Mul(x, x))
		}
	}
}

func TestFrSecretFormattingIsRedacted(t *testing.T) {
	z := Uint64(9)
	for _, format := range []string{"%v", "%+v", "%#v", "%x", "%d", "%b"} {
		if got := fmt.Sprintf(format, z); got != "bn254fr.Element(<redacted>)" {
			t.Fatalf("unexpected value formatting %q: %q", format, got)
		}
		if got := fmt.Sprintf(format, &z); got != "bn254fr.Element(<redacted>)" {
			t.Fatalf("unexpected pointer formatting %q: %q", format, got)
		}
	}
	if _, err := json.Marshal(z); err == nil {
		t.Fatal("JSON marshaling a secret field element must fail")
	}
	if _, err := z.MarshalText(); err == nil {
		t.Fatal("text marshaling a secret field element must fail")
	}
}
