package circuit

import (
	"encoding/json"
	"io"
	"math/big"
	"os"
	"runtime"
	"testing"
	"time"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	ecc_twistededwards "github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/signature/eddsa"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"
)

func TestBatchJoinSplit16x32FeasibilityPublicInputOrder(t *testing.T) {
	require.Equal(t, [12]string{
		"MerkleRoot", "ChainDomainHi", "ChainDomainLo", "ExpiresAtUnix",
		"InputCount", "OutputCount", "NullifierRoot", "CommitmentRoot",
		"UserDisclosureRoot", "FullDisclosureRoot", "PayloadDigestHi", "PayloadDigestLo",
	}, privacytypes.BatchPublicInputOrderV1)

	assignment := buildBatchFeasibilityAssignment(t, 1, 1)
	expected := make([]*big.Int, 12)
	for i := range expected {
		expected[i] = big.NewInt(int64(101 + i))
	}
	assignment.MerkleRoot = expected[0]
	assignment.ChainDomainHi = expected[1]
	assignment.ChainDomainLo = expected[2]
	assignment.ExpiresAtUnix = expected[3]
	assignment.InputCount = expected[4]
	assignment.OutputCount = expected[5]
	assignment.NullifierRoot = expected[6]
	assignment.CommitmentRoot = expected[7]
	assignment.UserDisclosureRoot = expected[8]
	assignment.FullDisclosureRoot = expected[9]
	assignment.PayloadDigestHi = expected[10]
	assignment.PayloadDigestLo = expected[11]
	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	publicVector := publicWitness.Vector().(fr.Vector)
	require.Len(t, publicVector, 12)
	for i := range expected {
		require.Zero(t, expected[i].Cmp(publicVector[i].BigInt(new(big.Int))), "public input index %d", i)
	}
}

func TestBatchJoinSplit16x32FeasibilityActivePrefixAndSentinels(t *testing.T) {
	if testing.Short() {
		t.Skip("full prototype solve is skipped in short mode")
	}
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &BatchJoinSplit16x32FeasibilityCircuit{})
	require.NoError(t, err)
	assertSolve := func(t *testing.T, assignment *BatchJoinSplit16x32FeasibilityCircuit, wantSuccess bool) {
		t.Helper()
		witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
		require.NoError(t, err)
		_, err = ccs.Solve(witness)
		if wantSuccess {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	assignment := buildBatchFeasibilityAssignment(t, 3, 4)
	assertSolve(t, assignment, true)

	t.Run("disabled input randomness", func(t *testing.T) {
		tampered := *assignment
		tampered.InputRandomness[3] = big.NewInt(1)
		assertSolve(t, &tampered, false)
	})
	t.Run("disabled output key", func(t *testing.T) {
		tampered := *assignment
		other := scalarMulBase(big.NewInt(97))
		assignPubKey(&tampered.OutputSpendPubKeys[4], other)
		assertSolve(t, &tampered, false)
	})
	t.Run("count zero", func(t *testing.T) {
		tampered := *assignment
		tampered.InputCount = big.NewInt(0)
		assertSolve(t, &tampered, false)
	})
	t.Run("duplicate active nullifier", func(t *testing.T) {
		tampered := *buildBatchFeasibilityAssignment(t, 3, 4)
		tampered.InputAmounts[1] = new(big.Int).Set(tampered.InputAmounts[0].(*big.Int))
		tampered.InputRandomness[1] = new(big.Int).Set(tampered.InputRandomness[0].(*big.Int))
		refreshBatchFeasibilityPublicState(t, &tampered, 3, 4)
		assertSolve(t, &tampered, false)
	})
	t.Run("duplicate active commitment", func(t *testing.T) {
		tampered := *buildBatchFeasibilityAssignment(t, 3, 4)
		tampered.OutputAmounts[0] = big.NewInt(10)
		tampered.OutputAmounts[1] = big.NewInt(10)
		tampered.OutputAmounts[2] = big.NewInt(1)
		tampered.OutputRandomness[1] = new(big.Int).Set(tampered.OutputRandomness[0].(*big.Int))
		refreshBatchFeasibilityPublicState(t, &tampered, 3, 4)
		assertSolve(t, &tampered, false)
	})
	t.Run("value conservation", func(t *testing.T) {
		tampered := *buildBatchFeasibilityAssignment(t, 3, 4)
		tampered.OutputAmounts[0] = big.NewInt(22)
		refreshBatchFeasibilityPublicState(t, &tampered, 3, 4)
		assertSolve(t, &tampered, false)
	})
	t.Run("user blinding reuses note randomness", func(t *testing.T) {
		tampered := *buildBatchFeasibilityAssignment(t, 3, 4)
		tampered.UserDisclosureBlindings[0] = new(big.Int).Set(tampered.OutputRandomness[0].(*big.Int))
		refreshBatchFeasibilityPublicState(t, &tampered, 3, 4)
		assertSolve(t, &tampered, false)
	})
	t.Run("full blinding reuses note randomness", func(t *testing.T) {
		tampered := *buildBatchFeasibilityAssignment(t, 3, 4)
		tampered.FullDisclosureBlindings[0] = new(big.Int).Set(tampered.OutputRandomness[0].(*big.Int))
		refreshBatchFeasibilityPublicState(t, &tampered, 3, 4)
		assertSolve(t, &tampered, false)
	})
	t.Run("full blinding reuses user blinding", func(t *testing.T) {
		tampered := *buildBatchFeasibilityAssignment(t, 3, 4)
		tampered.FullDisclosureBlindings[0] = new(big.Int).Set(tampered.UserDisclosureBlindings[0].(*big.Int))
		refreshBatchFeasibilityPublicState(t, &tampered, 3, 4)
		assertSolve(t, &tampered, false)
	})
}

type batchActiveCommitmentNonZeroCircuit struct {
	Enabled    frontend.Variable `gnark:",secret"`
	Commitment frontend.Variable `gnark:",secret"`
}

func (c *batchActiveCommitmentNonZeroCircuit) Define(api frontend.API) error {
	api.AssertIsBoolean(c.Enabled)
	assertEnabledNonZero(api, c.Enabled, c.Commitment)
	return nil
}

func TestBatchJoinSplit16x32ActiveInputCommitmentMustBeNonZero(t *testing.T) {
	require.NoError(t, test.IsSolved(
		&batchActiveCommitmentNonZeroCircuit{},
		&batchActiveCommitmentNonZeroCircuit{Enabled: 1, Commitment: 1},
		ecc.BN254.ScalarField(),
	))
	require.Error(t, test.IsSolved(
		&batchActiveCommitmentNonZeroCircuit{},
		&batchActiveCommitmentNonZeroCircuit{Enabled: 1, Commitment: 0},
		ecc.BN254.ScalarField(),
	))
	require.NoError(t, test.IsSolved(
		&batchActiveCommitmentNonZeroCircuit{},
		&batchActiveCommitmentNonZeroCircuit{Enabled: 0, Commitment: 0},
		ecc.BN254.ScalarField(),
	))
}

type batchFeasibilityShapeResult struct {
	Inputs                 int       `json:"inputs"`
	Outputs                int       `json:"outputs"`
	WitnessBuildMillis     float64   `json:"witness_build_ms"`
	FirstProveMillis       float64   `json:"first_prove_ms"`
	WarmProveSamplesMillis []float64 `json:"warm_prove_samples_ms"`
	WarmProveMeanMillis    float64   `json:"warm_prove_mean_ms"`
	VerifySamplesMillis    []float64 `json:"verify_samples_ms"`
}

type batchFeasibilityReport struct {
	GeneratedAtUTC              string                        `json:"generated_at_utc"`
	GOOS                        string                        `json:"goos"`
	GOARCH                      string                        `json:"goarch"`
	GoVersion                   string                        `json:"go_version"`
	GnarkVersion                string                        `json:"gnark_version"`
	Curve                       string                        `json:"curve"`
	Backend                     string                        `json:"backend"`
	ConstraintCount             int                           `json:"constraint_count"`
	JoinSplit2x2ConstraintCount int                           `json:"joinsplit_2x2_constraint_count"`
	DominantGadgetCounts        map[string]int                `json:"dominant_gadget_counts"`
	SubgroupMeasurement         batchSubgroupMeasurement      `json:"subgroup_measurement"`
	CompileMillis               float64                       `json:"compile_ms"`
	SetupMillis                 float64                       `json:"setup_ms"`
	R1CSBytes                   int64                         `json:"r1cs_bytes"`
	ProvingKeyBytes             int64                         `json:"proving_key_bytes"`
	VerifyingKeyBytes           int64                         `json:"verifying_key_bytes"`
	ProofBytes                  int64                         `json:"proof_bytes"`
	SamplesPerShape             int                           `json:"samples_per_shape"`
	Shapes                      []batchFeasibilityShapeResult `json:"shapes"`
	JoinSplit2x2FirstProveMS    float64                       `json:"joinsplit_2x2_first_prove_ms"`
	JoinSplit2x2WarmSamplesMS   []float64                     `json:"joinsplit_2x2_warm_prove_samples_ms"`
	JoinSplit2x2WarmMeanMS      float64                       `json:"joinsplit_2x2_warm_prove_mean_ms"`
}

type batchSubgroupMeasurement struct {
	PointCount                int     `json:"point_count"`
	BaselineConstraints       int     `json:"on_curve_non_identity_constraints"`
	WithSubgroupConstraints   int     `json:"with_prime_subgroup_constraints"`
	IncrementalConstraints    int     `json:"prime_subgroup_incremental_constraints"`
	BaselineCompileMillis     float64 `json:"baseline_compile_ms"`
	WithSubgroupCompileMillis float64 `json:"with_subgroup_compile_ms"`
}

type batchPointBaselineCircuit struct {
	Points [67]eddsa.PublicKey `gnark:",secret"`
}

func (c *batchPointBaselineCircuit) Define(api frontend.API) error {
	curve, err := twistededwards.NewEdCurve(api, ecc_twistededwards.BN254)
	if err != nil {
		return err
	}
	for i := range c.Points {
		curve.AssertIsOnCurve(c.Points[i].A)
		api.AssertIsDifferent(c.Points[i].A.X, 0)
	}
	return nil
}

type batchPointSubgroupCircuit struct {
	Points [67]eddsa.PublicKey `gnark:",secret"`
}

func (c *batchPointSubgroupCircuit) Define(api frontend.API) error {
	curve, err := twistededwards.NewEdCurve(api, ecc_twistededwards.BN254)
	if err != nil {
		return err
	}
	for i := range c.Points {
		assertPrimeSubgroupPoint(api, curve, c.Points[i].A)
	}
	return nil
}

// TestBatchJoinSplit16x32FullShapeResourceGate is intentionally opt-in because
// it performs a development Groth16 setup and 15 proof samples (three for each
// of four batch shapes plus three for JoinSplit2x2). Session 2 runs it
// explicitly and records the emitted JSON; CI still compiles this code.
func TestBatchJoinSplit16x32FullShapeResourceGate(t *testing.T) {
	if os.Getenv("CLAIRVEIL_RUN_BATCH_FEASIBILITY") != "1" {
		t.Skip("set CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 to run the full resource gate")
	}

	report := batchFeasibilityReport{
		GeneratedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		GoVersion:       runtime.Version(),
		GnarkVersion:    "v0.14.0",
		Curve:           "BN254",
		Backend:         "Groth16",
		SamplesPerShape: 3,
		DominantGadgetCounts: map[string]int{
			"active_prefix_one_hot_values":    48,
			"active_input_commitment_nonzero": 16,
			"amount_range_checks":             48,
			"note_commitment_hashes":          48,
			"note_nullifier_hashes":           16,
			"independent_merkle_node_hashes":  512,
			"prime_subgroup_point_checks":     67,
			"active_pair_distinctness_checks": 616,
			"user_disclosure_hashes":          32,
			"user_disclosure_leaf_hashes":     32,
			"full_disclosure_hashes":          32,
			"blinding_inequality_checks":      96,
			"vector_leaf_hashes":              112,
			"vector_internal_node_hashes":     108,
			"vector_root_hashes":              4,
			"owner_eddsa_verifiers":           1,
		},
	}

	started := time.Now()
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &BatchJoinSplit16x32FeasibilityCircuit{})
	require.NoError(t, err)
	report.CompileMillis = durationMillis(time.Since(started))
	report.ConstraintCount = ccs.GetNbConstraints()
	report.R1CSBytes = serializedSize(t, ccs)

	report.SubgroupMeasurement.PointCount = 67
	started = time.Now()
	pointBaselineCCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &batchPointBaselineCircuit{})
	require.NoError(t, err)
	report.SubgroupMeasurement.BaselineCompileMillis = durationMillis(time.Since(started))
	report.SubgroupMeasurement.BaselineConstraints = pointBaselineCCS.GetNbConstraints()
	started = time.Now()
	pointSubgroupCCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &batchPointSubgroupCircuit{})
	require.NoError(t, err)
	report.SubgroupMeasurement.WithSubgroupCompileMillis = durationMillis(time.Since(started))
	report.SubgroupMeasurement.WithSubgroupConstraints = pointSubgroupCCS.GetNbConstraints()
	report.SubgroupMeasurement.IncrementalConstraints = pointSubgroupCCS.GetNbConstraints() - pointBaselineCCS.GetNbConstraints()
	require.Positive(t, report.SubgroupMeasurement.IncrementalConstraints)

	started = time.Now()
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)
	report.SetupMillis = durationMillis(time.Since(started))
	report.ProvingKeyBytes = serializedSize(t, pk)
	report.VerifyingKeyBytes = serializedSize(t, vk)

	for _, shape := range [][2]int{{1, 1}, {3, 4}, {8, 16}, {16, 32}} {
		assignment := buildBatchFeasibilityAssignment(t, shape[0], shape[1])
		started = time.Now()
		fullWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
		require.NoError(t, err)
		witnessBuild := time.Since(started)

		publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
		require.NoError(t, err)
		shapeResult := batchFeasibilityShapeResult{
			Inputs: shape[0], Outputs: shape[1], WitnessBuildMillis: durationMillis(witnessBuild),
		}
		for sample := 0; sample < report.SamplesPerShape; sample++ {
			started = time.Now()
			proof, err := groth16.Prove(ccs, pk, fullWitness)
			require.NoError(t, err)
			proveMillis := durationMillis(time.Since(started))
			if sample == 0 {
				shapeResult.FirstProveMillis = proveMillis
			} else {
				shapeResult.WarmProveSamplesMillis = append(shapeResult.WarmProveSamplesMillis, proveMillis)
			}
			if report.ProofBytes == 0 {
				report.ProofBytes = serializedSize(t, proof)
			}
			started = time.Now()
			require.NoError(t, groth16.Verify(proof, vk, publicWitness))
			shapeResult.VerifySamplesMillis = append(shapeResult.VerifySamplesMillis, durationMillis(time.Since(started)))
		}
		shapeResult.WarmProveMeanMillis = meanMillis(shapeResult.WarmProveSamplesMillis)
		report.Shapes = append(report.Shapes, shapeResult)
	}

	baselineAssignment := buildValidJoinSplitAssignment(t)
	baselineCCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &JoinSplitCircuit{})
	require.NoError(t, err)
	report.JoinSplit2x2ConstraintCount = baselineCCS.GetNbConstraints()
	baselinePK, _, err := groth16.Setup(baselineCCS)
	require.NoError(t, err)
	baselineWitness, err := frontend.NewWitness(baselineAssignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	for sample := 0; sample < report.SamplesPerShape; sample++ {
		started = time.Now()
		_, err = groth16.Prove(baselineCCS, baselinePK, baselineWitness)
		require.NoError(t, err)
		proveMillis := durationMillis(time.Since(started))
		if sample == 0 {
			report.JoinSplit2x2FirstProveMS = proveMillis
		} else {
			report.JoinSplit2x2WarmSamplesMS = append(report.JoinSplit2x2WarmSamplesMS, proveMillis)
		}
	}
	report.JoinSplit2x2WarmMeanMS = meanMillis(report.JoinSplit2x2WarmSamplesMS)
	require.Len(t, report.Shapes, 4)
	maxShape := report.Shapes[len(report.Shapes)-1]
	require.Equal(t, BatchFeasibilityMaxInputs, maxShape.Inputs)
	require.Equal(t, BatchFeasibilityMaxOutputs, maxShape.Outputs)
	require.Less(t, maxShape.WarmProveMeanMillis/float64(maxShape.Outputs), report.JoinSplit2x2WarmMeanMS)
	require.Positive(t, report.R1CSBytes)
	require.Positive(t, report.ProvingKeyBytes)
	require.Positive(t, report.VerifyingKeyBytes)
	require.Equal(t, int64(164), report.ProofBytes)

	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	t.Logf("BATCH_FEASIBILITY_REPORT=%s", encoded)
}

func buildBatchFeasibilityAssignment(t testing.TB, inputCount, outputCount int) *BatchJoinSplit16x32FeasibilityCircuit {
	t.Helper()
	require.GreaterOrEqual(t, inputCount, 1)
	require.LessOrEqual(t, inputCount, BatchFeasibilityMaxInputs)
	require.GreaterOrEqual(t, outputCount, 1)
	require.LessOrEqual(t, outputCount, BatchFeasibilityMaxOutputs)

	ownerSpendScalar := big.NewInt(17)
	ownerViewScalar := big.NewInt(19)
	ownerSpendKey := scalarMulBase(ownerSpendScalar)
	ownerViewKey := scalarMulBase(ownerViewScalar)
	ownerSpendX, ownerSpendY := pointBigInts(ownerSpendKey)
	ownerViewX, ownerViewY := pointBigInts(ownerViewKey)
	assetID := big.NewInt(11)
	chainDomain, err := privacytypes.ComputeChainDomainV1("clairveil-feasibility-1", "privacy-note-v1")
	require.NoError(t, err)

	assignment := &BatchJoinSplit16x32FeasibilityCircuit{
		ChainDomainHi:   chainDomain.Hi,
		ChainDomainLo:   chainDomain.Lo,
		ExpiresAtUnix:   big.NewInt(2_000_000_000),
		InputCount:      big.NewInt(int64(inputCount)),
		OutputCount:     big.NewInt(int64(outputCount)),
		PayloadDigestHi: big.NewInt(1234567),
		PayloadDigestLo: big.NewInt(7654321),
		AssetID:         assetID,
	}
	assignPubKey(&assignment.OwnerSpendPubKey, ownerSpendKey)
	assignPubKey(&assignment.OwnerViewPubKey, ownerViewKey)

	inputLeaves := make([]*big.Int, inputCount)
	nullifierValues := zeroBigIntVector(BatchFeasibilityMaxInputs)
	for i := 0; i < BatchFeasibilityMaxInputs; i++ {
		assignPubKey(&assignment.InputSpendPubKeys[i], ownerSpendKey)
		assignPubKey(&assignment.InputViewPubKeys[i], ownerViewKey)
		assignment.InputAmounts[i] = big.NewInt(0)
		assignment.InputRandomness[i] = big.NewInt(0)
		for level := 0; level < MerkleDepth; level++ {
			assignment.InputPaths[i][level] = big.NewInt(0)
			assignment.InputPathHelpers[i][level] = big.NewInt(0)
		}
		if i >= inputCount {
			continue
		}
		amount := big.NewInt(7)
		randomness := big.NewInt(int64(13 + i))
		assignment.InputAmounts[i] = amount
		assignment.InputRandomness[i] = randomness
		commitment := privacytypes.ComputeNoteCommitmentV1(
			ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
			amount, assetID, randomness,
		)
		inputLeaves[i] = commitment
		nullifierValues[i] = privacytypes.ComputeNoteNullifierV1(commitment, randomness, ownerSpendX, ownerSpendY)
	}

	root, paths, helpers := batchFeasibilityTreeAndPaths(inputLeaves)
	assignment.MerkleRoot = root
	for i := 0; i < inputCount; i++ {
		for level := 0; level < MerkleDepth; level++ {
			assignment.InputPaths[i][level] = paths[i][level]
			assignment.InputPathHelpers[i][level] = helpers[i][level]
		}
	}

	commitmentValues := zeroBigIntVector(BatchFeasibilityMaxOutputs)
	userDigestValues := zeroBigIntVector(BatchFeasibilityMaxOutputs)
	userPolicies := make([]uint32, BatchFeasibilityMaxOutputs)
	fullDigestValues := zeroBigIntVector(BatchFeasibilityMaxOutputs)
	for i := 0; i < BatchFeasibilityMaxOutputs; i++ {
		assignPubKey(&assignment.OutputSpendPubKeys[i], ownerSpendKey)
		assignPubKey(&assignment.OutputViewPubKeys[i], ownerViewKey)
		assignment.OutputAmounts[i] = big.NewInt(0)
		assignment.OutputRandomness[i] = big.NewInt(0)
		assignment.OutputPrivacyPolicies[i] = big.NewInt(0)
		assignment.UserDisclosureBlindings[i] = big.NewInt(0)
		assignment.FullDisclosureBlindings[i] = big.NewInt(0)
		if i >= outputCount {
			continue
		}
		amount := big.NewInt(0)
		if i == 0 {
			amount = big.NewInt(int64(inputCount * 7))
		}
		randomness := big.NewInt(int64(1_000 + i))
		policy := uint32(privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom)
		userPolicies[i] = policy
		userBlinding := big.NewInt(int64(2_000 + i))
		fullBlinding := big.NewInt(int64(3_000 + i))
		assignment.OutputAmounts[i] = amount
		assignment.OutputRandomness[i] = randomness
		assignment.OutputPrivacyPolicies[i] = new(big.Int).SetUint64(uint64(policy))
		assignment.UserDisclosureBlindings[i] = userBlinding
		assignment.FullDisclosureBlindings[i] = fullBlinding

		commitment := privacytypes.ComputeNoteCommitmentV1(
			ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
			amount, assetID, randomness,
		)
		commitmentValues[i] = commitment
		userDigest, err := privacytypes.ComputeBatchUserDisclosureDigestV1(privacytypes.BatchUserDisclosureV1Input{
			OutputIndex: uint32(i), Commitment: commitment, Policy: policy, DisclosedFieldBitmap: policy,
			SelectedAmount:        amount,
			SelectedFromSpendKeyX: ownerSpendX, SelectedFromSpendKeyY: ownerSpendY,
			SelectedFromViewKeyX: ownerViewX, SelectedFromViewKeyY: ownerViewY,
			SelectedToSpendKeyX: ownerSpendX, SelectedToSpendKeyY: ownerSpendY,
			SelectedToViewKeyX: ownerViewX, SelectedToViewKeyY: ownerViewY,
			AssetID: assetID, UserDisclosureBlinding: userBlinding,
		})
		require.NoError(t, err)
		userDigestValues[i] = userDigest
		fullDigest, err := privacytypes.ComputeBatchFullDisclosureDigestV1(privacytypes.BatchFullDisclosureV1Input{
			OutputIndex: uint32(i), Commitment: commitment, Amount: amount, AssetID: assetID,
			SenderSpendKeyX: ownerSpendX, SenderSpendKeyY: ownerSpendY,
			SenderViewKeyX: ownerViewX, SenderViewKeyY: ownerViewY,
			RecipientSpendKeyX: ownerSpendX, RecipientSpendKeyY: ownerSpendY,
			RecipientViewKeyX: ownerViewX, RecipientViewKeyY: ownerViewY,
			FullDisclosureBlinding: fullBlinding,
		})
		require.NoError(t, err)
		fullDigestValues[i] = fullDigest
	}

	assignment.NullifierRoot, err = privacytypes.ComputeBatchVectorRootV1(
		privacytypes.BatchVectorNullifierV1, uint32(inputCount), nullifierValues,
	)
	require.NoError(t, err)
	assignment.CommitmentRoot, err = privacytypes.ComputeBatchVectorRootV1(
		privacytypes.BatchVectorCommitmentV1, uint32(outputCount), commitmentValues,
	)
	require.NoError(t, err)
	assignment.UserDisclosureRoot, err = privacytypes.ComputeBatchUserDisclosureVectorRootV1(
		uint32(outputCount), userPolicies, userDigestValues,
	)
	require.NoError(t, err)
	assignment.FullDisclosureRoot, err = privacytypes.ComputeBatchVectorRootV1(
		privacytypes.BatchVectorFullDisclosureV1, uint32(outputCount), fullDigestValues,
	)
	require.NoError(t, err)

	intent, err := privacytypes.ComputeBatchTransferIntentV1(privacytypes.BatchTransferIntentV1Input{
		ChainDomainHi: chainDomain.Hi, ChainDomainLo: chainDomain.Lo,
		MerkleRoot: root, InputCount: uint32(inputCount), OutputCount: uint32(outputCount),
		AssetID: assetID, NullifierRoot: assignment.NullifierRoot.(*big.Int),
		CommitmentRoot:     assignment.CommitmentRoot.(*big.Int),
		UserDisclosureRoot: assignment.UserDisclosureRoot.(*big.Int),
		FullDisclosureRoot: assignment.FullDisclosureRoot.(*big.Int),
		PayloadDigestHi:    assignment.PayloadDigestHi.(*big.Int),
		PayloadDigestLo:    assignment.PayloadDigestLo.(*big.Int),
		ExpiresAtUnix:      assignment.ExpiresAtUnix.(*big.Int).Int64(),
	})
	require.NoError(t, err)
	assignment.OwnerSignature = signSpendMessage(t, intent, ownerSpendScalar, ownerSpendKey)
	return assignment
}

func refreshBatchFeasibilityPublicState(t testing.TB, assignment *BatchJoinSplit16x32FeasibilityCircuit, inputCount, outputCount int) {
	t.Helper()
	assetID := assignment.AssetID.(*big.Int)
	ownerSpendX := assignment.OwnerSpendPubKey.A.X.(*big.Int)
	ownerSpendY := assignment.OwnerSpendPubKey.A.Y.(*big.Int)
	ownerViewX := assignment.OwnerViewPubKey.A.X.(*big.Int)
	ownerViewY := assignment.OwnerViewPubKey.A.Y.(*big.Int)

	inputLeaves := make([]*big.Int, inputCount)
	nullifierValues := zeroBigIntVector(BatchFeasibilityMaxInputs)
	for i := 0; i < inputCount; i++ {
		amount := assignment.InputAmounts[i].(*big.Int)
		randomness := assignment.InputRandomness[i].(*big.Int)
		commitment := privacytypes.ComputeNoteCommitmentV1(ownerSpendX, ownerSpendY, ownerViewX, ownerViewY, amount, assetID, randomness)
		inputLeaves[i] = commitment
		nullifierValues[i] = privacytypes.ComputeNoteNullifierV1(commitment, randomness, ownerSpendX, ownerSpendY)
	}
	root, paths, helpers := batchFeasibilityTreeAndPaths(inputLeaves)
	assignment.MerkleRoot = root
	for i := 0; i < inputCount; i++ {
		for level := 0; level < MerkleDepth; level++ {
			assignment.InputPaths[i][level] = paths[i][level]
			assignment.InputPathHelpers[i][level] = helpers[i][level]
		}
	}

	commitmentValues := zeroBigIntVector(BatchFeasibilityMaxOutputs)
	userRawDigests := zeroBigIntVector(BatchFeasibilityMaxOutputs)
	userPolicies := make([]uint32, BatchFeasibilityMaxOutputs)
	fullDigestValues := zeroBigIntVector(BatchFeasibilityMaxOutputs)
	for i := 0; i < outputCount; i++ {
		amount := assignment.OutputAmounts[i].(*big.Int)
		randomness := assignment.OutputRandomness[i].(*big.Int)
		spendX := assignment.OutputSpendPubKeys[i].A.X.(*big.Int)
		spendY := assignment.OutputSpendPubKeys[i].A.Y.(*big.Int)
		viewX := assignment.OutputViewPubKeys[i].A.X.(*big.Int)
		viewY := assignment.OutputViewPubKeys[i].A.Y.(*big.Int)
		policy := uint32(assignment.OutputPrivacyPolicies[i].(*big.Int).Uint64())
		commitment := privacytypes.ComputeNoteCommitmentV1(spendX, spendY, viewX, viewY, amount, assetID, randomness)
		commitmentValues[i] = commitment
		userPolicies[i] = policy
		userDigest, err := privacytypes.ComputeBatchUserDisclosureDigestV1(privacytypes.BatchUserDisclosureV1Input{
			OutputIndex: uint32(i), Commitment: commitment, Policy: policy, DisclosedFieldBitmap: policy,
			SelectedAmount:        amount,
			SelectedFromSpendKeyX: ownerSpendX, SelectedFromSpendKeyY: ownerSpendY,
			SelectedFromViewKeyX: ownerViewX, SelectedFromViewKeyY: ownerViewY,
			SelectedToSpendKeyX: spendX, SelectedToSpendKeyY: spendY,
			SelectedToViewKeyX: viewX, SelectedToViewKeyY: viewY,
			AssetID: assetID, UserDisclosureBlinding: assignment.UserDisclosureBlindings[i].(*big.Int),
		})
		require.NoError(t, err)
		userRawDigests[i] = userDigest
		fullDigest, err := privacytypes.ComputeBatchFullDisclosureDigestV1(privacytypes.BatchFullDisclosureV1Input{
			OutputIndex: uint32(i), Commitment: commitment, Amount: amount, AssetID: assetID,
			SenderSpendKeyX: ownerSpendX, SenderSpendKeyY: ownerSpendY,
			SenderViewKeyX: ownerViewX, SenderViewKeyY: ownerViewY,
			RecipientSpendKeyX: spendX, RecipientSpendKeyY: spendY,
			RecipientViewKeyX: viewX, RecipientViewKeyY: viewY,
			FullDisclosureBlinding: assignment.FullDisclosureBlindings[i].(*big.Int),
		})
		require.NoError(t, err)
		fullDigestValues[i] = fullDigest
	}

	var err error
	assignment.NullifierRoot, err = privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorNullifierV1, uint32(inputCount), nullifierValues)
	require.NoError(t, err)
	assignment.CommitmentRoot, err = privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorCommitmentV1, uint32(outputCount), commitmentValues)
	require.NoError(t, err)
	assignment.UserDisclosureRoot, err = privacytypes.ComputeBatchUserDisclosureVectorRootV1(uint32(outputCount), userPolicies, userRawDigests)
	require.NoError(t, err)
	assignment.FullDisclosureRoot, err = privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorFullDisclosureV1, uint32(outputCount), fullDigestValues)
	require.NoError(t, err)

	intent, err := privacytypes.ComputeBatchTransferIntentV1(privacytypes.BatchTransferIntentV1Input{
		ChainDomainHi: assignment.ChainDomainHi.(*big.Int), ChainDomainLo: assignment.ChainDomainLo.(*big.Int),
		MerkleRoot: root, InputCount: uint32(inputCount), OutputCount: uint32(outputCount), AssetID: assetID,
		NullifierRoot: assignment.NullifierRoot.(*big.Int), CommitmentRoot: assignment.CommitmentRoot.(*big.Int),
		UserDisclosureRoot: assignment.UserDisclosureRoot.(*big.Int), FullDisclosureRoot: assignment.FullDisclosureRoot.(*big.Int),
		PayloadDigestHi: assignment.PayloadDigestHi.(*big.Int), PayloadDigestLo: assignment.PayloadDigestLo.(*big.Int),
		ExpiresAtUnix: assignment.ExpiresAtUnix.(*big.Int).Int64(),
	})
	require.NoError(t, err)
	ownerSpendScalar := big.NewInt(17)
	assignment.OwnerSignature = signSpendMessage(t, intent, ownerSpendScalar, scalarMulBase(ownerSpendScalar))
}

func batchFeasibilityTreeAndPaths(leaves []*big.Int) (*big.Int, [][MerkleDepth]*big.Int, [][MerkleDepth]*big.Int) {
	paths := make([][MerkleDepth]*big.Int, len(leaves))
	helpers := make([][MerkleDepth]*big.Int, len(leaves))
	layer := make(map[uint64]*big.Int, len(leaves))
	for i, leaf := range leaves {
		layer[uint64(i)] = new(big.Int).Set(leaf)
	}
	for level := 0; level < MerkleDepth; level++ {
		empty := privacytypes.EmptyNoteTreeRootV1(uint32(level))
		for original := range leaves {
			index := uint64(original) >> level
			siblingIndex := index ^ 1
			sibling, ok := layer[siblingIndex]
			if !ok {
				sibling = empty
			}
			paths[original][level] = new(big.Int).Set(sibling)
			helpers[original][level] = new(big.Int).SetUint64(index & 1)
		}

		next := make(map[uint64]*big.Int, (len(layer)+1)/2)
		maxIndex := uint64(0)
		for index := range layer {
			if index > maxIndex {
				maxIndex = index
			}
		}
		for parent := uint64(0); parent <= maxIndex/2; parent++ {
			left, ok := layer[parent*2]
			if !ok {
				left = empty
			}
			right, ok := layer[parent*2+1]
			if !ok {
				right = empty
			}
			next[parent] = privacytypes.ComputeNoteTreeNodeV1(uint32(level), left, right)
		}
		layer = next
	}
	return layer[0], paths, helpers
}

func zeroBigIntVector(size int) []*big.Int {
	values := make([]*big.Int, size)
	for i := range values {
		values[i] = big.NewInt(0)
	}
	return values
}

type countWriter struct{ n int64 }

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func serializedSize(t testing.TB, value interface {
	WriteTo(io.Writer) (int64, error)
}) int64 {
	t.Helper()
	w := &countWriter{}
	reported, err := value.WriteTo(w)
	require.NoError(t, err)
	require.Equal(t, reported, w.n)
	return w.n
}

func durationMillis(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func meanMillis(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
