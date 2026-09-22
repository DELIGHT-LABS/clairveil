// Package amount implements the unsigned 128-bit shielded operation amount.
package amount

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/bits"
)

const BitLength = 128
const MaxDecimal = "340282366920938463463374607431768211455"

// Amount128 has a zero value of zero. Its limbs never expose mutable storage.
type Amount128 struct{ hi, lo uint64 }

func FromUint64(v uint64) Amount128 { return Amount128{lo: v} }
func FromBytes16(v [16]byte) Amount128 {
	return Amount128{binary.BigEndian.Uint64(v[:8]), binary.BigEndian.Uint64(v[8:])}
}
func (a Amount128) Bytes16() (v [16]byte) {
	binary.BigEndian.PutUint64(v[:8], a.hi)
	binary.BigEndian.PutUint64(v[8:], a.lo)
	return
}
func (a Amount128) Bytes32() (v [32]byte) { b := a.Bytes16(); copy(v[16:], b[:]); return }
func FromBytes32(v [32]byte) (Amount128, error) {
	for _, b := range v[:16] {
		if b != 0 {
			return Amount128{}, fmt.Errorf("amount exceeds 128 bits")
		}
	}
	var b [16]byte
	copy(b[:], v[16:])
	return FromBytes16(b), nil
}
func (a Amount128) IsZero() bool { return a.hi == 0 && a.lo == 0 }
func (a Amount128) Cmp(b Amount128) int {
	if a.hi < b.hi || a.hi == b.hi && a.lo < b.lo {
		return -1
	}
	if a == b {
		return 0
	}
	return 1
}
func (a Amount128) Add(b Amount128) (Amount128, error) {
	lo, c := bits.Add64(a.lo, b.lo, 0)
	hi, c := bits.Add64(a.hi, b.hi, c)
	if c != 0 {
		return Amount128{}, fmt.Errorf("amount exceeds 128 bits")
	}
	return Amount128{hi, lo}, nil
}
func (a Amount128) Sub(b Amount128) (Amount128, error) {
	lo, c := bits.Sub64(a.lo, b.lo, 0)
	hi, c := bits.Sub64(a.hi, b.hi, c)
	if c != 0 {
		return Amount128{}, fmt.Errorf("amount underflow")
	}
	return Amount128{hi, lo}, nil
}

func Parse(s string) (Amount128, error) {
	var a Amount128
	if s == "" || len(s) > 1 && s[0] == '0' {
		return a, fmt.Errorf("amount must be a canonical non-negative decimal string")
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return Amount128{}, fmt.Errorf("amount must be a canonical non-negative decimal string")
		}
		carry, lo := bits.Mul64(a.lo, 10)
		overflow, hi := bits.Mul64(a.hi, 10)
		hi, c1 := bits.Add64(hi, carry, 0)
		lo, c2 := bits.Add64(lo, uint64(c-'0'), 0)
		hi, c3 := bits.Add64(hi, 0, c2)
		if overflow|c1|c3 != 0 {
			return Amount128{}, fmt.Errorf("amount exceeds 128 bits")
		}
		a = Amount128{hi, lo}
	}
	return a, nil
}
func (a Amount128) String() string {
	if a.IsZero() {
		return "0"
	}
	var digits [39]byte
	i := len(digits)
	for !a.IsZero() {
		qhi, r := bits.Div64(0, a.hi, 10)
		qlo, r := bits.Div64(r, a.lo, 10)
		i--
		digits[i] = byte(r) + '0'
		a = Amount128{qhi, qlo}
	}
	return string(digits[i:])
}

// MarshalJSON preserves every bit in intentionally exported amount fields.
func (a Amount128) MarshalJSON() ([]byte, error) { return []byte(`"` + a.String() + `"`), nil }

// UnmarshalJSON accepts only canonical decimal strings, never JSON numbers.
func (a *Amount128) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("amount must be a decimal string: %w", err)
	}
	parsed, err := Parse(value)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
