package auditfield

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"testing"
)

// These immutable expected values come from the independently prepared native
// handoff vectors. The product implementation never regenerates expectations.
func TestFourKindNativeKAT(t *testing.T) {
	for _, name := range []string{"final_deposit", "final_spend", "final_join", "final_batch"} {
		t.Run(name, func(t *testing.T) {
			var vector struct {
				T        []json.Number `json:"T"`
				Plain    []json.Number `json:"plain"`
				Root     []json.Number `json:"root"`
				Envelope string        `json:"envelope_hex"`
			}
			data, err := os.ReadFile("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(&vector); err != nil {
				t.Fatal(err)
			}
			fields := func(numbers []json.Number) []Field32 {
				result := make([]Field32, len(numbers))
				for i, number := range numbers {
					value, ok := new(big.Int).SetString(string(number), 10)
					if !ok || value.Sign() < 0 || value.BitLen() > 256 {
						t.Fatal("invalid fixture integer")
					}
					var raw [32]byte
					value.FillBytes(raw[:])
					result[i], err = ParseField32(raw[:])
					if err != nil {
						t.Fatal(err)
					}
				}
				return result
			}
			transcript := fields(vector.T)
			if len(transcript) != 25 {
				t.Fatal("invalid fixture transcript")
			}
			kind := Kind(transcript[2][31])
			context, err := NewAuditContext(kind, transcript[3:24])
			if err != nil {
				t.Fatal(err)
			}
			plain, err := NewAuditPlain(kind, fields(vector.Plain))
			if err != nil {
				t.Fatal(err)
			}
			var nonce Nonce128
			copy(nonce[:], transcript[24][16:])
			envelope, root, err := encryptAuditWithNonce(context.AuditKey(), testNonzeroScalar(t, 101), context, nonce, plain)
			if err != nil {
				t.Fatal(err)
			}
			expectedWire, err := hex.DecodeString(vector.Envelope)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := envelope.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(wire, expectedWire) {
				t.Fatal("KAT envelope mismatch")
			}
			expectedRoot := fields(vector.Root)
			if root.Left != expectedRoot[0] || root.Right != expectedRoot[1] {
				t.Fatal("KAT root mismatch")
			}
			decoded, err := DecryptAudit(testNonzeroScalar(t, 37), context, envelope, root)
			if err != nil || decoded == nil {
				t.Fatalf("KAT decrypt: %v", err)
			}
			got, want := decoded.Fields(), plain.Fields()
			if len(got) != len(want) {
				t.Fatal("KAT plaintext length")
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("KAT plaintext field %d", i)
				}
			}
			// A matching public root is not authentication. Recompute it after
			// corrupting the tag so this reaches the AE tag check itself.
			badTag := envelope.Tag()
			badTag[31] ^= 1
			inputs, outputs := envelope.ActiveCounts()
			bad, err := NewEnvelopeFrame(kind, inputs, outputs, nonce, envelope.EphemeralFrame(), envelope.Ciphertext(), badTag)
			if err != nil {
				t.Fatal(err)
			}
			tr, err := fieldElements(transcript)
			if err != nil {
				t.Fatal(err)
			}
			ct, err := fieldElements(append(bad.Ciphertext(), bad.Tag()))
			if err != nil {
				t.Fatal(err)
			}
			ep, err := bad.EphemeralFrame().validPoint()
			if err != nil {
				t.Fatal(err)
			}
			badRoot, err := cipherRoot(kind, tr, &ep, ct)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := DecryptAudit(testNonzeroScalar(t, 37), context, bad, badRoot); out != nil || err != ErrAuditDecrypt {
				t.Fatal("tag failure with matching root disclosed plaintext")
			}
		})
	}
}
