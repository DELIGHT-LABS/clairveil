// Package timingtest implements opt-in, interleaved native timing diagnostics.
// It is used only by tests and does not certify constant-time execution.
package timingtest

import (
	"math"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

type moments struct {
	n        int
	mean, m2 float64
}

func (m *moments) add(x float64) {
	m.n++
	d := x - m.mean
	m.mean += d / float64(m.n)
	m.m2 += d * (x - m.mean)
}
func welch(a, b moments) float64 {
	variance := a.m2/float64(a.n-1)/float64(a.n) + b.m2/float64(b.n-1)/float64(b.n)
	if variance == 0 {
		return 0
	}
	return (a.mean - b.mean) / math.Sqrt(variance)
}

// Run uses three independent randomized batches, plus identical-input negative
// controls. Every batch is reported; repeated |t|>4.5 blocks acceptance rather
// than selectively discarding inconvenient measurements.
func Run(t *testing.T, name string, operation func(class uint64)) {
	t.Helper()
	if os.Getenv("CLAIRVEIL_NATIVE_TIMING") != "1" {
		t.Skip("opt-in native timing: CLAIRVEIL_NATIVE_TIMING=1")
	}
	samples := 3000
	if raw := os.Getenv("CLAIRVEIL_TIMING_SAMPLES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1000 || n > 1000000 {
			t.Fatal("timing samples must be 1000..1000000")
		}
		samples = n
	}
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for i := 0; i < 200; i++ {
		operation(uint64(i & 1))
	}
	positive, negative := 0, 0
	for batch := 0; batch < 3; batch++ {
		rng := rand.New(rand.NewSource(0x43544649584544 + int64(batch)))
		for _, control := range []bool{true, false} {
			// Balanced classes in a fresh random order avoid drift/order correlation.
			order := make([]uint64, 2*samples)
			for i := samples; i < len(order); i++ {
				order[i] = 1
			}
			rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			var groups [2]moments
			for _, class := range order {
				operand := class
				if control {
					operand = 0
				}
				start := time.Now()
				operation(operand)
				elapsed := time.Since(start)
				groups[class].add(float64(elapsed.Nanoseconds()))
			}
			statistic := welch(groups[0], groups[1])
			t.Logf("timing name=%s batch=%d identical_control=%t samples_per_class=%d mean0_ns=%.2f mean1_ns=%.2f welch_t=%.5f go=%s cpu=%s/%s", name, batch, control, samples, groups[0].mean, groups[1].mean, statistic, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			if math.Abs(statistic) > 4.5 {
				if control {
					negative++
				} else {
					positive++
				}
			}
		}
	}
	if negative >= 2 {
		t.Errorf("%s: repeated significant identical-input control; environment/profile not accepted", name)
	}
	if positive >= 2 {
		t.Errorf("%s: repeated significant timing difference; profile blocked pending investigation", name)
	}
}
