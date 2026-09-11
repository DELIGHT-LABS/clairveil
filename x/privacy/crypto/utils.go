package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"io"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

// GenerateRandomness preserves the legacy zero-allowed exact q sampler using
// a fixed field transport. Secret callers never receive a variable-width integer.
func GenerateRandomness() (FieldValue, error) { return sampleFieldScalar(rand.Reader, false) }

// GenerateNonZeroRandomness samples q* independently for each call.
func GenerateNonZeroRandomness() (FieldValue, error) { return SampleNonzeroFieldValue(rand.Reader) }

func sampleFieldScalar(reader io.Reader, nonzero bool) (out FieldValue, err error) {
	if err = secretprofile.Check(); err != nil {
		return out, err
	}
	subtle.WithDataIndependentTiming(func() {
		var scalar scalarct.Scalar
		scalar, err = scalarct.SampleScalar(reader, nonzero)
		defer secretmem.Clear(&scalar)
		if err != nil {
			return
		}
		raw := scalar.Bytes()
		defer secretmem.Clear(&raw)
		out, err = ParseFieldValueBE32(raw[:])
	})
	return out, err
}
