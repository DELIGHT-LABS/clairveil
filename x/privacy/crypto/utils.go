package crypto

import (
	"crypto/rand"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

// GenerateRandomness returns a cryptographically secure scalar below the curve order.
func GenerateRandomness() (*big.Int, error) {
	curve := twistededwards.GetEdwardsCurve()

	r, err := rand.Int(rand.Reader, &curve.Order)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// GenerateNonZeroRandomness returns a cryptographically secure non-zero
// scalar. It is used for blindings where zero would remove the privacy
// protection even though it is a valid field value.
func GenerateNonZeroRandomness() (*big.Int, error) {
	for {
		r, err := GenerateRandomness()
		if err != nil {
			return nil, err
		}
		if r.Sign() != 0 {
			return r, nil
		}
	}
}
