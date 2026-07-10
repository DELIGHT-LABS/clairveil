package circuit

import (
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"

	ecc_twistededwards "github.com/consensys/gnark-crypto/ecc/twistededwards"
)

const (
	NumInputs               = 2
	NumOutputs              = 2
	AuditDisclosureFullMask = 255
)

type JoinSplitCircuit struct {
	MerkleRoot           frontend.Variable             `gnark:",public"`
	ChainDomainHi        frontend.Variable             `gnark:",public"`
	ChainDomainLo        frontend.Variable             `gnark:",public"`
	ExpiresAtUnix        frontend.Variable             `gnark:",public"`
	Nullifiers           [NumInputs]frontend.Variable  `gnark:",public"`
	Commitments          [NumOutputs]frontend.Variable `gnark:",public"`
	UserPrivacyPolicy    frontend.Variable             `gnark:",public"`
	UserDisclosureDigest frontend.Variable             `gnark:",public"`
	FullDisclosureDigest frontend.Variable             `gnark:",public"`
	PayloadDigestHi      frontend.Variable             `gnark:",public"`
	PayloadDigestLo      frontend.Variable             `gnark:",public"`

	AssetID frontend.Variable `gnark:",secret"`

	InputAmounts    [NumInputs]frontend.Variable `gnark:",secret"`
	InputRandomness [NumInputs]frontend.Variable `gnark:",secret"`

	InputPaths       [NumInputs][MerkleDepth]frontend.Variable `gnark:",secret"`
	InputPathHelpers [NumInputs][MerkleDepth]frontend.Variable `gnark:",secret"`

	OwnerSignature     eddsa.Signature               `gnark:",secret"`
	InputSpendPubKeys  [NumInputs]eddsa.PublicKey    `gnark:",secret"`
	InputViewPubKeys   [NumInputs]eddsa.PublicKey    `gnark:",secret"`
	OutputAmounts      [NumOutputs]frontend.Variable `gnark:",secret"`
	OutputRandomness   [NumOutputs]frontend.Variable `gnark:",secret"`
	OutputSpendPubKeys [NumOutputs]eddsa.PublicKey   `gnark:",secret"`
	OutputViewPubKeys  [NumOutputs]eddsa.PublicKey   `gnark:",secret"`

	UserDisclosureBlinding frontend.Variable `gnark:",secret"`
	FullDisclosureBlinding frontend.Variable `gnark:",secret"`
}

func (c *JoinSplitCircuit) Define(api frontend.API) error {
	h, _ := mimc.NewMiMC(api)
	curve, _ := twistededwards.NewEdCurve(api, ecc_twistededwards.BN254)
	api.ToBinary(c.ChainDomainHi, 128)
	api.ToBinary(c.ChainDomainLo, 128)
	api.ToBinary(c.PayloadDigestHi, 128)
	api.ToBinary(c.PayloadDigestLo, 128)
	api.ToBinary(c.ExpiresAtUnix, 64)
	api.AssertIsDifferent(c.ExpiresAtUnix, 0)
	api.AssertIsDifferent(c.Nullifiers[0], c.Nullifiers[1])
	api.AssertIsDifferent(c.Commitments[0], c.Commitments[1])
	assertCanonicalEdDSASignature(api, curve, c.OwnerSignature)

	var totalInputAmount frontend.Variable = 0
	var totalOutputAmount frontend.Variable = 0

	for i := 0; i < NumInputs; i++ {
		assertAmountRange(api, c.InputAmounts[i])
		assertPrimeSubgroupPoint(api, curve, c.InputSpendPubKeys[i].A)
		assertPrimeSubgroupPoint(api, curve, c.InputViewPubKeys[i].A)

		h.Reset()
		h.Write(
			c.InputSpendPubKeys[i].A.X,
			c.InputSpendPubKeys[i].A.Y,
			c.InputViewPubKeys[i].A.X,
			c.InputViewPubKeys[i].A.Y,
			c.InputAmounts[i],
			c.AssetID,
			c.InputRandomness[i],
		)
		inputCommitment := h.Sum()

		currentHash := inputCommitment
		for j := 0; j < MerkleDepth; j++ {
			left := api.Select(c.InputPathHelpers[i][j], c.InputPaths[i][j], currentHash)
			right := api.Select(c.InputPathHelpers[i][j], currentHash, c.InputPaths[i][j])

			h.Reset()
			h.Write(left, right)
			currentHash = h.Sum()
		}
		api.AssertIsEqual(currentHash, c.MerkleRoot)

		h.Reset()
		h.Write(c.InputRandomness[i], c.InputSpendPubKeys[i].A.X, c.InputSpendPubKeys[i].A.Y)
		api.AssertIsEqual(h.Sum(), c.Nullifiers[i])

		totalInputAmount = api.Add(totalInputAmount, c.InputAmounts[i])
	}

	// Every transfer has an always-on full audit disclosure, so both inputs
	// must belong to the same shielded owner.
	api.AssertIsEqual(c.InputSpendPubKeys[0].A.X, c.InputSpendPubKeys[1].A.X)
	api.AssertIsEqual(c.InputSpendPubKeys[0].A.Y, c.InputSpendPubKeys[1].A.Y)
	api.AssertIsEqual(c.InputViewPubKeys[0].A.X, c.InputViewPubKeys[1].A.X)
	api.AssertIsEqual(c.InputViewPubKeys[0].A.Y, c.InputViewPubKeys[1].A.Y)

	h.Reset()
	h.Write(
		privacycrypto.HashString(privacytypes.NullifierSetV1FieldDomain),
		NumInputs,
		c.Nullifiers[0],
		c.Nullifiers[1],
	)
	nullifierSetDigest := h.Sum()
	h.Reset()
	h.Write(
		privacycrypto.HashString(privacytypes.CommitmentSetV1FieldDomain),
		NumOutputs,
		c.Commitments[0],
		c.Commitments[1],
	)
	commitmentSetDigest := h.Sum()
	h.Reset()
	h.Write(
		privacycrypto.HashString(privacytypes.TransferIntentV2FieldDomain),
		c.ChainDomainHi,
		c.ChainDomainLo,
		privacycrypto.HashString(privacytypes.JoinSplit2x2V2CircuitKindFieldDomain),
		c.MerkleRoot,
		NumInputs,
		NumOutputs,
		c.AssetID,
		nullifierSetDigest,
		commitmentSetDigest,
		c.UserDisclosureDigest,
		c.FullDisclosureDigest,
		c.PayloadDigestHi,
		c.PayloadDigestLo,
		c.ExpiresAtUnix,
	)
	transferIntent := h.Sum()
	h.Reset()
	if err := eddsa.Verify(curve, c.OwnerSignature, transferIntent, c.InputSpendPubKeys[0], &h); err != nil {
		return err
	}

	for i := 0; i < NumOutputs; i++ {
		assertAmountRange(api, c.OutputAmounts[i])
		assertPrimeSubgroupPoint(api, curve, c.OutputSpendPubKeys[i].A)
		assertPrimeSubgroupPoint(api, curve, c.OutputViewPubKeys[i].A)

		h.Reset()
		h.Write(
			c.OutputSpendPubKeys[i].A.X,
			c.OutputSpendPubKeys[i].A.Y,
			c.OutputViewPubKeys[i].A.X,
			c.OutputViewPubKeys[i].A.Y,
			c.OutputAmounts[i],
			c.AssetID,
			c.OutputRandomness[i],
		)
		calcCommitment := h.Sum()

		api.AssertIsEqual(calcCommitment, c.Commitments[i])
		totalOutputAmount = api.Add(totalOutputAmount, c.OutputAmounts[i])
	}

	api.AssertIsEqual(totalInputAmount, totalOutputAmount)

	policyBits := api.ToBinary(c.UserPrivacyPolicy, 3)
	revealAmount := policyBits[0]
	revealTo := policyBits[1]
	revealFrom := policyBits[2]

	selectedAmount := api.Select(revealAmount, c.OutputAmounts[0], 0)
	selectedAssetID := api.Select(revealAmount, c.AssetID, 0)
	selectedFromSpendX := api.Select(revealFrom, c.InputSpendPubKeys[0].A.X, 0)
	selectedFromSpendY := api.Select(revealFrom, c.InputSpendPubKeys[0].A.Y, 0)
	selectedFromViewX := api.Select(revealFrom, c.InputViewPubKeys[0].A.X, 0)
	selectedFromViewY := api.Select(revealFrom, c.InputViewPubKeys[0].A.Y, 0)
	selectedToSpendX := api.Select(revealTo, c.OutputSpendPubKeys[0].A.X, 0)
	selectedToSpendY := api.Select(revealTo, c.OutputSpendPubKeys[0].A.Y, 0)
	selectedToViewX := api.Select(revealTo, c.OutputViewPubKeys[0].A.X, 0)
	selectedToViewY := api.Select(revealTo, c.OutputViewPubKeys[0].A.Y, 0)
	userDisclosureEnabled := api.Sub(1, api.IsZero(c.UserPrivacyPolicy))

	h.Reset()
	h.Write(
		privacycrypto.HashString(privacytypes.TransferUserDisclosureV2FieldDomain),
		c.UserPrivacyPolicy,
		0,
		c.Commitments[0],
		selectedAmount,
		selectedAssetID,
		selectedFromSpendX,
		selectedFromSpendY,
		selectedFromViewX,
		selectedFromViewY,
		selectedToSpendX,
		selectedToSpendY,
		selectedToViewX,
		selectedToViewY,
		api.Select(userDisclosureEnabled, c.UserDisclosureBlinding, 0),
	)
	calcUserDisclosureDigest := h.Sum()

	api.AssertIsEqual(api.Select(userDisclosureEnabled, api.IsZero(c.UserDisclosureBlinding), 0), 0)
	api.AssertIsEqual(api.Select(userDisclosureEnabled, calcUserDisclosureDigest, 0), c.UserDisclosureDigest)

	api.AssertIsDifferent(c.FullDisclosureBlinding, 0)
	h.Reset()
	h.Write(
		privacycrypto.HashString(privacytypes.TransferFullDisclosureV2FieldDomain),
		AuditDisclosureFullMask,
		0,
		c.Commitments[0],
		c.OutputAmounts[0],
		c.AssetID,
		c.InputSpendPubKeys[0].A.X,
		c.InputSpendPubKeys[0].A.Y,
		c.InputViewPubKeys[0].A.X,
		c.InputViewPubKeys[0].A.Y,
		c.OutputSpendPubKeys[0].A.X,
		c.OutputSpendPubKeys[0].A.Y,
		c.OutputViewPubKeys[0].A.X,
		c.OutputViewPubKeys[0].A.Y,
		c.FullDisclosureBlinding,
	)
	api.AssertIsEqual(h.Sum(), c.FullDisclosureDigest)

	return nil
}
