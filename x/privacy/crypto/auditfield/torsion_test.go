package auditfield

import (
	"encoding/hex"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
)

func TestPoint64RejectsOrderTwoTorsion(t *testing.T) {
	// (0,-1) is on the curve but has order 2, outside the odd prime subgroup.
	y, err := hex.DecodeString("30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000000")
	if err != nil {
		t.Fatal(err)
	}
	var encoded [64]byte
	copy(encoded[32:], y)
	if _, err := edwardsct.DecodePoint64(encoded[:]); err == nil {
		t.Fatal("core accepted torsion")
	}
	if _, err := ParsePoint64(encoded[:]); err == nil {
		t.Fatal("Point64 accepted torsion")
	}
}
