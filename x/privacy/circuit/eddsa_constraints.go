package circuit

import (
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/signature/eddsa"
)

func assertPrimeSubgroupPoint(api frontend.API, curve twistededwards.Curve, point twistededwards.Point) {
	curve.AssertIsOnCurve(point)
	// The only BN254 Edwards points with X == 0 are the identity and the
	// order-two point. Neither is a valid prime-order wire key.
	api.AssertIsDifferent(point.X, 0)
	orderMinusOne := new(big.Int).Sub(curve.Params().Order, big.NewInt(1))
	orderMultiple := curve.Add(curve.ScalarMul(point, orderMinusOne), point)
	api.AssertIsEqual(orderMultiple.X, 0)
	api.AssertIsEqual(orderMultiple.Y, 1)
}

func assertCanonicalEdDSASignature(api frontend.API, curve twistededwards.Curve, signature eddsa.Signature) {
	assertPrimeSubgroupPoint(api, curve, signature.R)
	api.AssertIsDifferent(signature.S, 0)
	api.AssertIsLessOrEqual(signature.S, new(big.Int).Sub(curve.Params().Order, big.NewInt(1)))
}
