package timingtest

import (
	"math"
	"testing"
)

func TestWelchStatistic(t *testing.T) {
	var a, b moments
	for _, v := range []float64{1, 2, 3} {
		a.add(v)
	}
	for _, v := range []float64{4, 5, 6} {
		b.add(v)
	}
	if got := welch(a, b); math.Abs(got-(-3.6742346141747673)) > 1e-12 {
		t.Fatalf("incorrect Welch statistic %v", got)
	}
	if welch(a, a) != 0 {
		t.Fatal("identical groups must have zero t")
	}
}
