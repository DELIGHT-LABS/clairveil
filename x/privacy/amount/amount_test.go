package amount

import (
	"encoding/json"
	"math/big"
	"testing"
)

func TestAmount128Boundaries(t *testing.T) {
	for _, s := range []string{"0", "1", "18446744073709551615", "18446744073709551616", MaxDecimal} {
		a, err := Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		if a.String() != s || FromBytes16(a.Bytes16()) != a {
			t.Fatalf("round trip %s", s)
		}
		b, err := FromBytes32(a.Bytes32())
		if err != nil || b != a {
			t.Fatalf("field round trip %s", s)
		}
	}
	for _, s := range []string{"", "00", "01", "-1", "+1", " 1", "1 ", "1.0", "340282366920938463463374607431768211456"} {
		if _, err := Parse(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	one := FromUint64(1)
	low, _ := Parse("18446744073709551615")
	carry, err := low.Add(one)
	if err != nil || carry.String() != "18446744073709551616" {
		t.Fatal("carry")
	}
	back, err := carry.Sub(one)
	if err != nil || back != low {
		t.Fatal("borrow")
	}
	var field [32]byte
	field[15] = 1
	if _, err := FromBytes32(field); err == nil {
		t.Fatal("upper field bits accepted")
	}
}

func TestAmount128ArithmeticDifferential(t *testing.T) {
	values := []string{"0", "1", "18446744073709551615", "18446744073709551616", "100000000000000000000", MaxDecimal}
	for _, as := range values {
		for _, bs := range values {
			a, _ := Parse(as)
			b, _ := Parse(bs)
			ab, _ := new(big.Int).SetString(as, 10)
			bb, _ := new(big.Int).SetString(bs, 10)
			if a.Cmp(b) != ab.Cmp(bb) {
				t.Fatal("comparison")
			}
			sum := new(big.Int).Add(ab, bb)
			got, err := a.Add(b)
			if sum.BitLen() > 128 {
				if err == nil {
					t.Fatal("overflow")
				}
			} else if err != nil || got.String() != sum.String() {
				t.Fatal("addition")
			}
			diff := new(big.Int).Sub(ab, bb)
			got, err = a.Sub(b)
			if diff.Sign() < 0 {
				if err == nil {
					t.Fatal("underflow")
				}
			} else if err != nil || got.String() != diff.String() {
				t.Fatal("subtraction")
			}
		}
	}
}

func TestAmount128JSON(t *testing.T) {
	for _, s := range []string{"0", "18446744073709551616", MaxDecimal} {
		a, _ := Parse(s)
		raw, err := json.Marshal(a)
		if err != nil || string(raw) != `"`+s+`"` {
			t.Fatalf("JSON export %s: %s %v", s, raw, err)
		}
		var b Amount128
		if err := json.Unmarshal(raw, &b); err != nil || a != b {
			t.Fatalf("JSON decode: %v", err)
		}
	}
	for _, raw := range []string{`0`, `null`, `"01"`, `"-1"`, `"340282366920938463463374607431768211456"`} {
		var a Amount128
		if err := json.Unmarshal([]byte(raw), &a); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
