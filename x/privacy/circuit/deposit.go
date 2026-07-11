package circuit

import (
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
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
	assertPrimeSubgroupPoint(api, curve, c.ReceiverSpendPubKey)
	assertPrimeSubgroupPoint(api, curve, c.ReceiverViewPubKey)

	h.Write(
		privacytypes.DomainFieldV1(privacytypes.NoteCommitmentV1FieldDomain),
		c.ReceiverSpendPubKey.X,
		c.ReceiverSpendPubKey.Y,
		c.ReceiverViewPubKey.X,
		c.ReceiverViewPubKey.Y,
		c.Amount,
		c.AssetID,
		c.Randomness,
	)
	calculatedCommitment := h.Sum()
	api.AssertIsDifferent(calculatedCommitment, 0)
	api.AssertIsEqual(calculatedCommitment, c.Commitment)

	return nil
}
