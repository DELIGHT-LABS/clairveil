package auditfield

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestT03DomainEncodingGolden(t *testing.T) {
	var nonce [32]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	network, err := NetworkDigest("clairveil-test-1", nonce)
	if err != nil || hex.EncodeToString(network[:]) != "9b0a38d675a601eef36d47307aa2fb9d682a66575e314670d0b81c88ea0d344b" {
		t.Fatalf("network golden mismatch: %x %v", network, err)
	}
	changed, _ := NetworkDigest("clairveil-test-2", nonce)
	if changed == network {
		t.Fatal("chain ID not bound")
	}
	nonce[0] ^= 1
	changed, _ = NetworkDigest("clairveil-test-1", nonce)
	if changed == network {
		t.Fatal("nonce not bound")
	}
	address := make([]byte, 20)
	for i := range address {
		address[i] = byte(i)
	}
	for i, want := range []string{"42423e7077300a8ef6fe395ac21988a1906b73a421cf798404d6a3eaab99849d", "d5d6dcd380beaf0bf309d372212644e0b53597fc33c1fd89c2c2359dd3a779fb"} {
		got, err := PublicTargetDigest(Kind(i+1), address)
		if err != nil || hex.EncodeToString(got[:]) != want {
			t.Fatalf("target golden mismatch: %x %v", got, err)
		}
	}
	hi, lo := DigestFields(network)
	if !bytes.Equal(hi[:16], make([]byte, 16)) || !bytes.Equal(lo[:16], make([]byte, 16)) || !bytes.Equal(append(hi[16:32:32], lo[16:]...), network[:]) {
		t.Fatal("digest split is not lossless uint128")
	}
	for _, chain := range []string{"", "bad\x00chain", "비ASCII"} {
		if _, err := NetworkDigest(chain, nonce); err == nil {
			t.Fatal("invalid chain ID accepted")
		}
	}
	if _, err := PublicTargetDigest(KindTransfer2x2, address); err == nil {
		t.Fatal("transfer target accepted")
	}
	if _, err := PublicTargetDigest(KindWithdraw, nil); err == nil {
		t.Fatal("empty target accepted")
	}
}
