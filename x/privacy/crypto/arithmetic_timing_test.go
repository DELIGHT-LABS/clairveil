package crypto

import (
	"crypto/subtle"
	"testing"

	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/timingtest"
)

var timingFieldSink frct.Element
var timingScalarSink scalarct.Scalar

func TestT19ArithmeticTiming(t *testing.T) {
	if err := secretprofile.Check(); err != nil {
		t.Skip(err)
	}
	one := frct.One()
	var high frct.Element
	high.Neg(&one)
	fields := [2]frct.Element{one, high}
	qOne := scalarct.One()
	var qHigh scalarct.Scalar
	qHigh.Neg(&qOne)
	scalars := [2]scalarct.Scalar{qOne, qHigh}
	t.Run("fr-mul", func(t *testing.T) {
		timingtest.Run(t, "fr-mul-low-high", func(class uint64) {
			x := fields[class]
			subtle.WithDataIndependentTiming(func() {
				for i := 0; i < 64; i++ {
					timingFieldSink.Mul(&x, &x)
				}
			})
		})
	})
	t.Run("fr-inv0", func(t *testing.T) {
		timingtest.Run(t, "fr-inv0-low-high", func(class uint64) {
			x := fields[class]
			subtle.WithDataIndependentTiming(func() { timingFieldSink.Inv0(&x) })
		})
	})
	t.Run("q-mul", func(t *testing.T) {
		timingtest.Run(t, "q-mul-low-high", func(class uint64) {
			x := scalars[class]
			subtle.WithDataIndependentTiming(func() {
				for i := 0; i < 64; i++ {
					timingScalarSink.Mul(&x, &x)
				}
			})
		})
	})
}
