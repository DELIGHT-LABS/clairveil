package circuit

import (
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	ecc_twistededwards "github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"
)

const (
	BatchFeasibilityMaxInputs  = int(privacytypes.BatchJoinSplitV1MaxInputs)
	BatchFeasibilityMaxOutputs = int(privacytypes.BatchJoinSplitV1MaxOutputs)
)

// BatchJoinSplit16x32FeasibilityCircuit is a capacity prototype only. It is
// deliberately not registered in the production circuit set and is not used
// by a Msg service. Its shape includes every dominant Session 2 constraint.
type BatchJoinSplit16x32FeasibilityCircuit struct {
	MerkleRoot         frontend.Variable `gnark:",public"`
	ChainDomainHi      frontend.Variable `gnark:",public"`
	ChainDomainLo      frontend.Variable `gnark:",public"`
	ExpiresAtUnix      frontend.Variable `gnark:",public"`
	InputCount         frontend.Variable `gnark:",public"`
	OutputCount        frontend.Variable `gnark:",public"`
	NullifierRoot      frontend.Variable `gnark:",public"`
	CommitmentRoot     frontend.Variable `gnark:",public"`
	UserDisclosureRoot frontend.Variable `gnark:",public"`
	FullDisclosureRoot frontend.Variable `gnark:",public"`
	PayloadDigestHi    frontend.Variable `gnark:",public"`
	PayloadDigestLo    frontend.Variable `gnark:",public"`

	AssetID frontend.Variable `gnark:",secret"`

	OwnerSpendPubKey eddsa.PublicKey `gnark:",secret"`
	OwnerViewPubKey  eddsa.PublicKey `gnark:",secret"`
	OwnerSignature   eddsa.Signature `gnark:",secret"`

	InputAmounts      [BatchFeasibilityMaxInputs]frontend.Variable              `gnark:",secret"`
	InputRandomness   [BatchFeasibilityMaxInputs]frontend.Variable              `gnark:",secret"`
	InputSpendPubKeys [BatchFeasibilityMaxInputs]eddsa.PublicKey                `gnark:",secret"`
	InputViewPubKeys  [BatchFeasibilityMaxInputs]eddsa.PublicKey                `gnark:",secret"`
	InputPaths        [BatchFeasibilityMaxInputs][MerkleDepth]frontend.Variable `gnark:",secret"`
	InputPathHelpers  [BatchFeasibilityMaxInputs][MerkleDepth]frontend.Variable `gnark:",secret"`

	OutputAmounts           [BatchFeasibilityMaxOutputs]frontend.Variable `gnark:",secret"`
	OutputRandomness        [BatchFeasibilityMaxOutputs]frontend.Variable `gnark:",secret"`
	OutputSpendPubKeys      [BatchFeasibilityMaxOutputs]eddsa.PublicKey   `gnark:",secret"`
	OutputViewPubKeys       [BatchFeasibilityMaxOutputs]eddsa.PublicKey   `gnark:",secret"`
	OutputPrivacyPolicies   [BatchFeasibilityMaxOutputs]frontend.Variable `gnark:",secret"`
	UserDisclosureBlindings [BatchFeasibilityMaxOutputs]frontend.Variable `gnark:",secret"`
	FullDisclosureBlindings [BatchFeasibilityMaxOutputs]frontend.Variable `gnark:",secret"`
}

func (c *BatchJoinSplit16x32FeasibilityCircuit) Define(api frontend.API) error {
	h, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}
	curve, err := twistededwards.NewEdCurve(api, ecc_twistededwards.BN254)
	if err != nil {
		return err
	}

	api.ToBinary(c.ChainDomainHi, 128)
	api.ToBinary(c.ChainDomainLo, 128)
	api.ToBinary(c.PayloadDigestHi, 128)
	api.ToBinary(c.PayloadDigestLo, 128)
	api.ToBinary(c.ExpiresAtUnix, 64)
	api.AssertIsDifferent(c.ExpiresAtUnix, 0)
	api.AssertIsDifferent(c.AssetID, 0)

	inputEnabled := exactActivePrefix(api, c.InputCount, BatchFeasibilityMaxInputs)
	outputEnabled := exactActivePrefix(api, c.OutputCount, BatchFeasibilityMaxOutputs)

	assertPrimeSubgroupPoint(api, curve, c.OwnerSpendPubKey.A)
	assertPrimeSubgroupPoint(api, curve, c.OwnerViewPubKey.A)
	assertCanonicalEdDSASignature(api, curve, c.OwnerSignature)

	nullifiers := make([]frontend.Variable, BatchFeasibilityMaxInputs)
	commitments := make([]frontend.Variable, BatchFeasibilityMaxOutputs)
	userDigests := make([]frontend.Variable, BatchFeasibilityMaxOutputs)
	fullDigests := make([]frontend.Variable, BatchFeasibilityMaxOutputs)
	var totalInput frontend.Variable = 0
	var totalOutput frontend.Variable = 0

	noteCommitmentDomain := privacytypes.DomainFieldV1(privacytypes.NoteCommitmentV1FieldDomain)
	noteNullifierDomain := privacytypes.DomainFieldV1(privacytypes.NoteNullifierV1FieldDomain)
	treeNodeDomain := privacytypes.DomainFieldV1(privacytypes.NoteTreeNodeV1FieldDomain)

	for i := 0; i < BatchFeasibilityMaxInputs; i++ {
		enabled := inputEnabled[i]
		disabled := api.Sub(1, enabled)
		assertAmountRange(api, c.InputAmounts[i])

		// Active inputs share one owner; disabled key slots use that same owner
		// as their canonical key sentinel.
		api.AssertIsEqual(c.InputSpendPubKeys[i].A.X, c.OwnerSpendPubKey.A.X)
		api.AssertIsEqual(c.InputSpendPubKeys[i].A.Y, c.OwnerSpendPubKey.A.Y)
		api.AssertIsEqual(c.InputViewPubKeys[i].A.X, c.OwnerViewPubKey.A.X)
		api.AssertIsEqual(c.InputViewPubKeys[i].A.Y, c.OwnerViewPubKey.A.Y)
		api.AssertIsEqual(api.Mul(disabled, c.InputAmounts[i]), 0)
		api.AssertIsEqual(api.Mul(disabled, c.InputRandomness[i]), 0)

		inputCommitment := batchCircuitHash(&h,
			noteCommitmentDomain,
			c.InputSpendPubKeys[i].A.X, c.InputSpendPubKeys[i].A.Y,
			c.InputViewPubKeys[i].A.X, c.InputViewPubKeys[i].A.Y,
			c.InputAmounts[i], c.AssetID, c.InputRandomness[i],
		)
		assertEnabledNonZero(api, enabled, inputCommitment)

		current := inputCommitment
		for level := 0; level < MerkleDepth; level++ {
			helper := c.InputPathHelpers[i][level]
			api.AssertIsBoolean(helper)
			api.AssertIsEqual(api.Mul(disabled, helper), 0)
			api.AssertIsEqual(api.Mul(disabled, c.InputPaths[i][level]), 0)
			left := api.Select(helper, c.InputPaths[i][level], current)
			right := api.Select(helper, current, c.InputPaths[i][level])
			current = batchCircuitHash(&h, treeNodeDomain, level, left, right)
		}
		api.AssertIsEqual(api.Mul(enabled, api.Sub(current, c.MerkleRoot)), 0)

		nullifier := batchCircuitHash(&h,
			noteNullifierDomain,
			inputCommitment,
			c.InputRandomness[i],
			c.InputSpendPubKeys[i].A.X,
			c.InputSpendPubKeys[i].A.Y,
		)
		api.AssertIsEqual(api.Mul(enabled, api.IsZero(nullifier)), 0)
		nullifiers[i] = api.Select(enabled, nullifier, 0)
		totalInput = api.Add(totalInput, api.Mul(enabled, c.InputAmounts[i]))
	}

	for i := 0; i < BatchFeasibilityMaxInputs; i++ {
		for j := i + 1; j < BatchFeasibilityMaxInputs; j++ {
			bothEnabled := api.Mul(inputEnabled[i], inputEnabled[j])
			api.AssertIsEqual(api.Mul(bothEnabled, api.IsZero(api.Sub(nullifiers[i], nullifiers[j]))), 0)
		}
	}

	userDisclosureDomain := privacytypes.DomainFieldV1(privacytypes.BatchUserDisclosureV2DomainLabel)
	userDisclosureLeafDomain := privacytypes.DomainFieldV1(privacytypes.BatchUserDisclosureLeafV1DomainLabel)
	fullDisclosureDomain := privacytypes.DomainFieldV1(privacytypes.BatchFullDisclosureV2DomainLabel)
	for i := 0; i < BatchFeasibilityMaxOutputs; i++ {
		enabled := outputEnabled[i]
		disabled := api.Sub(1, enabled)
		assertAmountRange(api, c.OutputAmounts[i])
		assertPrimeSubgroupPoint(api, curve, c.OutputSpendPubKeys[i].A)
		assertPrimeSubgroupPoint(api, curve, c.OutputViewPubKeys[i].A)

		api.AssertIsEqual(api.Mul(disabled, c.OutputAmounts[i]), 0)
		api.AssertIsEqual(api.Mul(disabled, c.OutputRandomness[i]), 0)
		api.AssertIsEqual(api.Mul(disabled, c.OutputPrivacyPolicies[i]), 0)
		api.AssertIsEqual(api.Mul(disabled, c.UserDisclosureBlindings[i]), 0)
		api.AssertIsEqual(api.Mul(disabled, c.FullDisclosureBlindings[i]), 0)
		api.AssertIsEqual(api.Mul(disabled, api.Sub(c.OutputSpendPubKeys[i].A.X, c.OwnerSpendPubKey.A.X)), 0)
		api.AssertIsEqual(api.Mul(disabled, api.Sub(c.OutputSpendPubKeys[i].A.Y, c.OwnerSpendPubKey.A.Y)), 0)
		api.AssertIsEqual(api.Mul(disabled, api.Sub(c.OutputViewPubKeys[i].A.X, c.OwnerViewPubKey.A.X)), 0)
		api.AssertIsEqual(api.Mul(disabled, api.Sub(c.OutputViewPubKeys[i].A.Y, c.OwnerViewPubKey.A.Y)), 0)

		commitment := batchCircuitHash(&h,
			noteCommitmentDomain,
			c.OutputSpendPubKeys[i].A.X, c.OutputSpendPubKeys[i].A.Y,
			c.OutputViewPubKeys[i].A.X, c.OutputViewPubKeys[i].A.Y,
			c.OutputAmounts[i], c.AssetID, c.OutputRandomness[i],
		)
		api.AssertIsEqual(api.Mul(enabled, api.IsZero(commitment)), 0)
		commitments[i] = api.Select(enabled, commitment, 0)
		totalOutput = api.Add(totalOutput, api.Mul(enabled, c.OutputAmounts[i]))

		policyBits := api.ToBinary(c.OutputPrivacyPolicies[i], 3)
		discloseAmount := policyBits[0]
		discloseTo := policyBits[1]
		discloseFrom := policyBits[2]
		userEnabled := api.Sub(1, api.IsZero(c.OutputPrivacyPolicies[i]))
		activeUserEnabled := api.Mul(enabled, userEnabled)
		api.AssertIsEqual(api.Mul(activeUserEnabled, api.IsZero(c.UserDisclosureBlindings[i])), 0)
		api.AssertIsEqual(api.Mul(api.Sub(1, activeUserEnabled), c.UserDisclosureBlindings[i]), 0)
		api.AssertIsEqual(api.Mul(activeUserEnabled, api.IsZero(api.Sub(c.UserDisclosureBlindings[i], c.OutputRandomness[i]))), 0)

		userDigest := batchCircuitHash(&h,
			userDisclosureDomain,
			i,
			commitment,
			c.OutputPrivacyPolicies[i],
			c.OutputPrivacyPolicies[i],
			api.Select(discloseAmount, c.OutputAmounts[i], 0),
			api.Select(discloseFrom, c.OwnerSpendPubKey.A.X, 0),
			api.Select(discloseFrom, c.OwnerSpendPubKey.A.Y, 0),
			api.Select(discloseFrom, c.OwnerViewPubKey.A.X, 0),
			api.Select(discloseFrom, c.OwnerViewPubKey.A.Y, 0),
			api.Select(discloseTo, c.OutputSpendPubKeys[i].A.X, 0),
			api.Select(discloseTo, c.OutputSpendPubKeys[i].A.Y, 0),
			api.Select(discloseTo, c.OutputViewPubKeys[i].A.X, 0),
			api.Select(discloseTo, c.OutputViewPubKeys[i].A.Y, 0),
			c.AssetID,
			c.UserDisclosureBlindings[i],
		)
		api.AssertIsEqual(api.Mul(activeUserEnabled, api.IsZero(userDigest)), 0)
		rawUserDigest := api.Select(activeUserEnabled, userDigest, 0)
		userValue := batchCircuitHash(&h,
			userDisclosureLeafDomain,
			i,
			enabled,
			c.OutputPrivacyPolicies[i],
			rawUserDigest,
		)
		api.AssertIsEqual(api.Mul(enabled, api.IsZero(userValue)), 0)
		userDigests[i] = api.Select(enabled, userValue, 0)

		api.AssertIsEqual(api.Mul(enabled, api.IsZero(c.FullDisclosureBlindings[i])), 0)
		api.AssertIsEqual(api.Mul(enabled, api.IsZero(api.Sub(c.FullDisclosureBlindings[i], c.OutputRandomness[i]))), 0)
		api.AssertIsEqual(api.Mul(enabled, api.IsZero(api.Sub(c.FullDisclosureBlindings[i], c.UserDisclosureBlindings[i]))), 0)
		fullDigest := batchCircuitHash(&h,
			fullDisclosureDomain,
			i,
			commitment,
			c.OutputAmounts[i],
			c.AssetID,
			c.OwnerSpendPubKey.A.X, c.OwnerSpendPubKey.A.Y,
			c.OwnerViewPubKey.A.X, c.OwnerViewPubKey.A.Y,
			c.OutputSpendPubKeys[i].A.X, c.OutputSpendPubKeys[i].A.Y,
			c.OutputViewPubKeys[i].A.X, c.OutputViewPubKeys[i].A.Y,
			c.FullDisclosureBlindings[i],
		)
		api.AssertIsEqual(api.Mul(enabled, api.IsZero(fullDigest)), 0)
		fullDigests[i] = api.Select(enabled, fullDigest, 0)
	}

	for i := 0; i < BatchFeasibilityMaxOutputs; i++ {
		for j := i + 1; j < BatchFeasibilityMaxOutputs; j++ {
			bothEnabled := api.Mul(outputEnabled[i], outputEnabled[j])
			api.AssertIsEqual(api.Mul(bothEnabled, api.IsZero(api.Sub(commitments[i], commitments[j]))), 0)
		}
	}
	api.AssertIsEqual(totalInput, totalOutput)

	computedNullifierRoot := batchVectorRootCircuit(api, &h, privacytypes.BatchVectorNullifierV1, c.InputCount, nullifiers, inputEnabled)
	computedCommitmentRoot := batchVectorRootCircuit(api, &h, privacytypes.BatchVectorCommitmentV1, c.OutputCount, commitments, outputEnabled)
	computedUserRoot := batchVectorRootCircuit(api, &h, privacytypes.BatchVectorUserDisclosureV1, c.OutputCount, userDigests, outputEnabled)
	computedFullRoot := batchVectorRootCircuit(api, &h, privacytypes.BatchVectorFullDisclosureV1, c.OutputCount, fullDigests, outputEnabled)
	api.AssertIsEqual(computedNullifierRoot, c.NullifierRoot)
	api.AssertIsEqual(computedCommitmentRoot, c.CommitmentRoot)
	api.AssertIsEqual(computedUserRoot, c.UserDisclosureRoot)
	api.AssertIsEqual(computedFullRoot, c.FullDisclosureRoot)

	intent := batchCircuitHash(&h,
		privacytypes.DomainFieldV1(privacytypes.BatchTransferIntentV1DomainLabel),
		c.ChainDomainHi, c.ChainDomainLo,
		privacytypes.DomainFieldV1("clairveil.batch-joinsplit-16x32.v1"),
		c.MerkleRoot,
		c.InputCount, c.OutputCount,
		c.AssetID,
		c.NullifierRoot, c.CommitmentRoot,
		c.UserDisclosureRoot, c.FullDisclosureRoot,
		c.PayloadDigestHi, c.PayloadDigestLo,
		c.ExpiresAtUnix,
	)
	h.Reset()
	if err := eddsa.Verify(curve, c.OwnerSignature, intent, c.OwnerSpendPubKey, &h); err != nil {
		return err
	}
	return nil
}

func exactActivePrefix(api frontend.API, count frontend.Variable, capacity int) []frontend.Variable {
	oneHot := make([]frontend.Variable, capacity)
	var oneHotSum frontend.Variable = 0
	for value := 1; value <= capacity; value++ {
		oneHot[value-1] = api.IsZero(api.Sub(count, value))
		oneHotSum = api.Add(oneHotSum, oneHot[value-1])
	}
	api.AssertIsEqual(oneHotSum, 1)

	enabled := make([]frontend.Variable, capacity)
	var suffix frontend.Variable = 0
	for i := capacity - 1; i >= 0; i-- {
		suffix = api.Add(suffix, oneHot[i])
		enabled[i] = suffix
		api.AssertIsBoolean(enabled[i])
	}
	return enabled
}

func batchCircuitHash(h *mimc.MiMC, inputs ...frontend.Variable) frontend.Variable {
	h.Reset()
	h.Write(inputs...)
	return h.Sum()
}

func assertEnabledNonZero(api frontend.API, enabled, value frontend.Variable) {
	api.AssertIsEqual(api.Mul(enabled, api.IsZero(value)), 0)
}

func batchVectorRootCircuit(
	api frontend.API,
	h *mimc.MiMC,
	kind privacytypes.BatchVectorKindV1,
	count frontend.Variable,
	values []frontend.Variable,
	enabled []frontend.Variable,
) frontend.Variable {
	leafDomain := privacytypes.DomainFieldV1(batchVectorDomainLabel(kind, "leaf"))
	nodeDomain := privacytypes.DomainFieldV1(batchVectorDomainLabel(kind, "node"))
	rootDomain := privacytypes.DomainFieldV1(batchVectorDomainLabel(kind, "root"))
	layer := make([]frontend.Variable, len(values))
	for i := range values {
		layer[i] = batchCircuitHash(h, leafDomain, i, enabled[i], values[i])
	}
	for level := 0; len(layer) > 1; level++ {
		next := make([]frontend.Variable, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = batchCircuitHash(h, nodeDomain, level, layer[i], layer[i+1])
		}
		layer = next
	}
	return batchCircuitHash(h, rootDomain, len(values), count, layer[0])
}

func batchVectorDomainLabel(kind privacytypes.BatchVectorKindV1, part string) string {
	return "clairveil.batch-vector." + string(kind) + "." + part + ".v1"
}
