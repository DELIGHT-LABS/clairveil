package circuit

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	ecc_twistededwards "github.com/consensys/gnark-crypto/ecc/twistededwards"
)

const MerkleDepth = 32

type SpendCircuit struct {
	MerkleRoot frontend.Variable `gnark:",public"`
	Nullifier  frontend.Variable `gnark:",public"`
	Amount     frontend.Variable `gnark:",public"`
	Recipient  frontend.Variable `gnark:",public"`
	AssetID    frontend.Variable `gnark:",public"`

	ReceiverSpendPubKey eddsa.PublicKey `gnark:",secret"`
	ReceiverViewPubKey  eddsa.PublicKey `gnark:",secret"`
	Signature           eddsa.Signature `gnark:",secret"`

	Randomness frontend.Variable `gnark:",secret"`

	Path       [MerkleDepth]frontend.Variable `gnark:",secret"`
	PathHelper [MerkleDepth]frontend.Variable `gnark:",secret"`
}

func (c *SpendCircuit) Define(api frontend.API) error {
	h, _ := mimc.NewMiMC(api)
	curve, _ := twistededwards.NewEdCurve(api, ecc_twistededwards.BN254)

	assertAmountRange(api, c.Amount)
	curve.AssertIsOnCurve(c.ReceiverSpendPubKey.A)
	curve.AssertIsOnCurve(c.ReceiverViewPubKey.A)
	curve.AssertIsOnCurve(c.Signature.R)

	h.Write(
		c.ReceiverSpendPubKey.A.X,
		c.ReceiverSpendPubKey.A.Y,
		c.ReceiverViewPubKey.A.X,
		c.ReceiverViewPubKey.A.Y,
		c.Amount,
		c.AssetID,
		c.Randomness,
	)
	currentHash := h.Sum()

	for i := 0; i < MerkleDepth; i++ {
		left := api.Select(c.PathHelper[i], c.Path[i], currentHash)
		right := api.Select(c.PathHelper[i], currentHash, c.Path[i])
		h.Reset()
		h.Write(left, right)
		currentHash = h.Sum()
	}
	api.AssertIsEqual(currentHash, c.MerkleRoot)

	h.Reset()
	h.Write(c.Amount, c.AssetID, c.Randomness, c.Recipient)
	msg := h.Sum()

	h.Reset()
	if err := eddsa.Verify(curve, c.Signature, msg, c.ReceiverSpendPubKey, &h); err != nil {
		return err
	}

	h.Reset()
	h.Write(c.Randomness, c.ReceiverSpendPubKey.A.X, c.ReceiverSpendPubKey.A.Y)
	api.AssertIsEqual(h.Sum(), c.Nullifier)

	return nil
}

func assertAmountRange(api frontend.API, amount frontend.Variable) {
	api.ToBinary(amount, privacytypes.ShieldedAmountBitLength)
}
