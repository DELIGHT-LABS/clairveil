package circuit

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	ecc_twistededwards "github.com/consensys/gnark-crypto/ecc/twistededwards"
)

const MerkleDepth = 32

type SpendCircuit struct {
	MerkleRoot        frontend.Variable `gnark:",public"`
	ChainDomainHi     frontend.Variable `gnark:",public"`
	ChainDomainLo     frontend.Variable `gnark:",public"`
	ExpiresAtUnix     frontend.Variable `gnark:",public"`
	Nullifier         frontend.Variable `gnark:",public"`
	Amount            frontend.Variable `gnark:",public"`
	RecipientDigestHi frontend.Variable `gnark:",public"`
	RecipientDigestLo frontend.Variable `gnark:",public"`
	AssetID           frontend.Variable `gnark:",public"`

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
	api.ToBinary(c.ChainDomainHi, 128)
	api.ToBinary(c.ChainDomainLo, 128)
	api.ToBinary(c.RecipientDigestHi, 128)
	api.ToBinary(c.RecipientDigestLo, 128)
	api.ToBinary(c.ExpiresAtUnix, 64)
	api.AssertIsDifferent(c.ExpiresAtUnix, 0)
	assertPrimeSubgroupPoint(api, curve, c.ReceiverSpendPubKey.A)
	assertPrimeSubgroupPoint(api, curve, c.ReceiverViewPubKey.A)
	assertCanonicalEdDSASignature(api, curve, c.Signature)

	h.Write(
		privacytypes.DomainFieldV1(privacytypes.NoteCommitmentV1FieldDomain),
		c.ReceiverSpendPubKey.A.X,
		c.ReceiverSpendPubKey.A.Y,
		c.ReceiverViewPubKey.A.X,
		c.ReceiverViewPubKey.A.Y,
		c.Amount,
		c.AssetID,
		c.Randomness,
	)
	noteCommitment := h.Sum()
	api.AssertIsDifferent(noteCommitment, 0)
	currentHash := noteCommitment

	for i := 0; i < MerkleDepth; i++ {
		left := api.Select(c.PathHelper[i], c.Path[i], currentHash)
		right := api.Select(c.PathHelper[i], currentHash, c.Path[i])
		h.Reset()
		h.Write(
			privacytypes.DomainFieldV1(privacytypes.NoteTreeNodeV1FieldDomain),
			i,
			left,
			right,
		)
		currentHash = h.Sum()
	}
	api.AssertIsEqual(currentHash, c.MerkleRoot)

	h.Reset()
	h.Write(
		privacycrypto.HashString(privacytypes.SpendIntentV2FieldDomain),
		c.ChainDomainHi,
		c.ChainDomainLo,
		privacycrypto.HashString(privacytypes.SpendV2CircuitKindFieldDomain),
		c.MerkleRoot,
		c.Nullifier,
		c.Amount,
		c.AssetID,
		c.RecipientDigestHi,
		c.RecipientDigestLo,
		c.ExpiresAtUnix,
	)
	msg := h.Sum()

	h.Reset()
	if err := eddsa.Verify(curve, c.Signature, msg, c.ReceiverSpendPubKey, &h); err != nil {
		return err
	}

	h.Reset()
	h.Write(
		privacytypes.DomainFieldV1(privacytypes.NoteNullifierV1FieldDomain),
		noteCommitment,
		c.Randomness,
		c.ReceiverSpendPubKey.A.X,
		c.ReceiverSpendPubKey.A.Y,
	)
	api.AssertIsDifferent(c.Nullifier, 0)
	api.AssertIsEqual(h.Sum(), c.Nullifier)

	return nil
}

func assertAmountRange(api frontend.API, amount frontend.Variable) {
	api.ToBinary(amount, privacytypes.ShieldedAmountBitLength)
}
