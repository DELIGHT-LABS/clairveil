package crypto

import (
	"crypto/subtle"
	"encoding/hex"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/timingtest"
)

var timingPointSink [32]byte
var timingMiMCSink FieldValue

func TestT19CurveAndLegacyMiMCTiming(t *testing.T) {
	if err := secretprofile.Check(); err != nil {
		t.Skip(err)
	}
	low := scalarFromByte(t, 1)
	raw, _ := hex.DecodeString("060c89ce5c263405370a08b6d0302b0bab3eedb83920ee0a677297dc392126f0")
	high, err := ImportNonzeroScalarBE32(raw)
	if err != nil {
		t.Fatal(err)
	}
	scalars := [2]SecretScalar{low, high}
	t.Run("curve-fixed-256", func(t *testing.T) {
		timingtest.Run(t, "curve-low-high", func(class uint64) {
			var pub interface{ Bytes() [32]byte }
			subtle.WithDataIndependentTiming(func() {
				p, err := PublicKey(scalars[class])
				if err != nil {
					t.Fatal(err)
				}
				pub = p
			})
			timingPointSink = pub.Bytes()
		})
	})
	one, two := FieldValueFromUint64(1), FieldValueFromUint64(2)
	fieldRaw, _ := hex.DecodeString("30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000000")
	highField, err := ParseFieldValueBE32(fieldRaw)
	if err != nil {
		t.Fatal(err)
	}
	fields := [2]FieldValue{one, highField}
	t.Run("legacy-mimc-110", func(t *testing.T) {
		timingtest.Run(t, "mimc-low-high", func(class uint64) {
			hash, err := LegacyMiMCHash(fields[class], one, two)
			if err != nil {
				t.Fatal(err)
			}
			timingMiMCSink = hash
		})
	})
}
