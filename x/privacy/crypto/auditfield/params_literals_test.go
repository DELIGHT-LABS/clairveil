package auditfield

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"

	"golang.org/x/crypto/sha3"
)

// Regeneration is test-only: upgrading dependencies must not silently change
// the suite's runtime constants. The seed is public, not cryptographic entropy.
func TestT13Poseidon2PinnedConstants(t *testing.T) {
	p, _ := new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte("Poseidon2-BN254[t=3,rF=8,rP=56,d=5]"))
	rnd := h.Sum(nil)
	constants := Poseidon2RoundConstants()
	digest := sha256.New()
	for i, want := range constants {
		h.Reset()
		h.Write(rnd)
		rnd = h.Sum(nil)
		var got [32]byte
		v := new(big.Int).SetBytes(rnd)
		v.Mod(v, p).FillBytes(got[:])
		if got != want {
			t.Fatalf("round constant %d changed", i)
		}
		digest.Write(want[:])
	}
	if hex.EncodeToString(digest.Sum(nil)) != Poseidon2RoundConstantsSHA256 {
		t.Fatal("round constants hash mismatch")
	}
	constants[0][0] ^= 1
	if constants == Poseidon2RoundConstants() {
		t.Fatal("mutable constant alias")
	}
}

func TestT13StaticTagPinnedConstants(t *testing.T) {
	p, _ := new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	tags := StaticTags()
	digest := sha256.New()
	for i, pair := range tags {
		domain := "clairveil.audit.field.kdf.v1"
		words := []uint32{0x8000001d, 2}
		if i >= 1 && i <= 4 {
			domain = "clairveil.audit.field.ae.v1"
			words = []uint32{0x8000001e}
			for j := 0; j < []int{6, 2, 13, 177}[i-1]; j++ {
				words = append(words, 1, 0x80000001)
			}
			words = append(words, 1)
		} else if i >= 5 {
			domain = "clairveil.audit.field.root.v1"
			words = []uint32{0x80000000 | []uint32{34, 30, 41, 205}[i-5], 2}
		}
		var pre bytes.Buffer
		pre.WriteString("clairveil.audit.static-tag.v1")
		binary.Write(&pre, binary.BigEndian, uint32(len(words)))
		for _, w := range words {
			binary.Write(&pre, binary.BigEndian, w)
		}
		binary.Write(&pre, binary.BigEndian, uint32(len(domain)))
		pre.WriteString(domain)
		h := sha3.NewShake256()
		h.Write(pre.Bytes())
		for j, want := range pair {
			var got [32]byte
			for {
				h.Read(got[:])
				if new(big.Int).SetBytes(got[:]).Cmp(p) < 0 {
					break
				}
			}
			if got != want {
				t.Fatalf("static tag %d/%d changed", i, j)
			}
			digest.Write(want[:])
		}
	}
	if hex.EncodeToString(digest.Sum(nil)) != StaticTagsSHA256 {
		t.Fatal("tag hash mismatch")
	}
	tags[0][0][0] ^= 1
	if tags == StaticTags() {
		t.Fatal("mutable tag alias")
	}
}
