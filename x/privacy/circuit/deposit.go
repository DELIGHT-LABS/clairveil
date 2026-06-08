package circuit

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"

	ecc_twistededwards "github.com/consensys/gnark-crypto/ecc/twistededwards"
)

type DepositCircuit struct {
	Commitment frontend.Variable `gnark:",public"`
	Amount     frontend.Variable `gnark:",public"`
	AssetID    frontend.Variable `gnark:",public"`

	ReceiverSpendPubKey twistededwards.Point `gnark:",secret"`
	ReceiverViewPubKey  twistededwards.Point `gnark:",secret"`
	Randomness          frontend.Variable    `gnark:",secret"`
}

func (c *DepositCircuit) Define(api frontend.API) error {
	h, _ := mimc.NewMiMC(api)
	curve, _ := twistededwards.NewEdCurve(api, ecc_twistededwards.BN254)

	assertAmountRange(api, c.Amount)
	curve.AssertIsOnCurve(c.ReceiverSpendPubKey)
	curve.AssertIsOnCurve(c.ReceiverViewPubKey)

	h.Write(
		c.ReceiverSpendPubKey.X,
		c.ReceiverSpendPubKey.Y,
		c.ReceiverViewPubKey.X,
		c.ReceiverViewPubKey.Y,
		c.Amount,
		c.AssetID,
		c.Randomness,
	)
	api.AssertIsEqual(h.Sum(), c.Commitment)

	return nil
}
