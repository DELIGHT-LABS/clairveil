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
