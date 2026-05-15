package circuit

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"

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
	h.Write(c.Signature.R.X, c.Signature.R.Y)
	h.Write(c.ReceiverSpendPubKey.A.X, c.ReceiverSpendPubKey.A.Y)
	h.Write(msg)
	hRAM := h.Sum()

	base := twistededwards.Point{X: curve.Params().Base[0], Y: curve.Params().Base[1]}
	lhs := curve.ScalarMul(base, c.Signature.S)

	hA := curve.ScalarMul(c.ReceiverSpendPubKey.A, hRAM)
	rhs := curve.Add(c.Signature.R, hA)

	api.AssertIsEqual(lhs.X, rhs.X)
	api.AssertIsEqual(lhs.Y, rhs.Y)

	h.Reset()
	h.Write(c.Randomness, c.ReceiverSpendPubKey.A.X, c.ReceiverSpendPubKey.A.Y)
	api.AssertIsEqual(h.Sum(), c.Nullifier)

	return nil
}
