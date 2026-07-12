package circuit

import (
	"encoding/json"
	"math/big"
	"os"
	"runtime"
	"testing"
	"time"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

const runJoinSplitDisclosureBlindingFeasibility = "CLAIRVEIL_RUN_JOINSPLIT_BLINDING_FEASIBILITY"

// joinSplitDisclosureBlindingFeasibilityCircuit is deliberately test-only.
// Session 2 uses it to freeze and measure the exact Session 3A constraint
// delta without changing the production JoinSplitCircuit or its artifacts.
type joinSplitDisclosureBlindingFeasibilityCircuit JoinSplitCircuit

func (c *joinSplitDisclosureBlindingFeasibilityCircuit) Define(api frontend.API) error {
	base := (*JoinSplitCircuit)(c)
	if err := base.Define(api); err != nil {
		return err
	}

	userEnabled := api.Sub(1, api.IsZero(base.UserPrivacyPolicy))
	api.AssertIsEqual(api.Mul(api.Sub(1, userEnabled), base.UserDisclosureBlinding), 0)
	api.AssertIsEqual(
		api.Mul(userEnabled, api.IsZero(api.Sub(base.UserDisclosureBlinding, base.OutputRandomness[0]))),
		0,
	)
	api.AssertIsDifferent(base.FullDisclosureBlinding, base.OutputRandomness[0])
	api.AssertIsDifferent(base.FullDisclosureBlinding, base.UserDisclosureBlinding)
	return nil
}

func TestJoinSplitDisclosureBlindingSeparationFeasibility(t *testing.T) {
	baselineCCS := compileJoinSplitCircuitForFeasibility(t, &JoinSplitCircuit{})
	hardenedCCS := compileJoinSplitCircuitForFeasibility(t, &joinSplitDisclosureBlindingFeasibilityCircuit{})
	require.Equal(t, 99_765, baselineCCS.GetNbConstraints())
	require.Equal(t, 99_775, hardenedCCS.GetNbConstraints())
	require.Equal(t, 10, hardenedCCS.GetNbConstraints()-baselineCCS.GetNbConstraints())
	t.Logf(
		"JOIN_SPLIT_DISCLOSURE_CONSTRAINT_DELTA baseline=%d hardened=%d delta=%d",
		baselineCCS.GetNbConstraints(),
		hardenedCCS.GetNbConstraints(),
		hardenedCCS.GetNbConstraints()-baselineCCS.GetNbConstraints(),
	)

	valid := buildValidJoinSplitAssignment(t)
	requireJoinSplitSolveResult(t, baselineCCS, valid, true)
	requireJoinSplitHardenedSolveResult(t, hardenedCCS, valid, true)

	allPrivate := *buildValidJoinSplitAssignment(t)
	allPrivate.UserPrivacyPolicy = big.NewInt(int64(privacytypes.TransferPrivacyPolicyAllPrivate))
	allPrivate.UserDisclosureBlinding = big.NewInt(0)
	allPrivate.OutputRandomness[0] = big.NewInt(0)
	refreshJoinSplitDisclosureFeasibilityState(t, &allPrivate)
	requireJoinSplitSolveResult(t, baselineCCS, &allPrivate, true)
	requireJoinSplitHardenedSolveResult(t, hardenedCCS, &allPrivate, true)

	negative := []struct {
		name   string
		mutate func(*JoinSplitCircuit)
	}{
		{
			name: "DBS-01 user reuses recipient output randomness",
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.UserDisclosureBlinding = cloneBigInt(assignment.OutputRandomness[0])
			},
		},
		{
			name: "DBS-02 full reuses recipient output randomness",
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.FullDisclosureBlinding = cloneBigInt(assignment.OutputRandomness[0])
			},
		},
		{
			name: "DBS-03 full reuses user",
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.FullDisclosureBlinding = cloneBigInt(assignment.UserDisclosureBlinding)
			},
		},
		{
			name: "all-private user sentinel is non-zero",
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.UserPrivacyPolicy = big.NewInt(int64(privacytypes.TransferPrivacyPolicyAllPrivate))
				assignment.UserDisclosureBlinding = big.NewInt(59)
			},
		},
	}

	for _, tc := range negative {
		t.Run(tc.name, func(t *testing.T) {
			assignment := *buildValidJoinSplitAssignment(t)
			tc.mutate(&assignment)
			refreshJoinSplitDisclosureFeasibilityState(t, &assignment)

			// The complete digest and owner signature are refreshed. Success
			// here proves no pre-existing constraint masks the new invariant.
			requireJoinSplitSolveResult(t, baselineCCS, &assignment, true)
			requireJoinSplitHardenedSolveResult(t, hardenedCCS, &assignment, false)
		})
	}
}

type joinSplitDisclosureResourceMetrics struct {
	ConstraintCount   int     `json:"constraint_count"`
	CompileMillis     float64 `json:"compile_ms"`
	SetupMillis       float64 `json:"setup_ms"`
	WitnessMillis     float64 `json:"witness_ms"`
	ProveMillis       float64 `json:"prove_ms"`
	VerifyMillis      float64 `json:"verify_ms"`
	R1CSBytes         int64   `json:"r1cs_bytes"`
	ProvingKeyBytes   int64   `json:"proving_key_bytes"`
	VerifyingKeyBytes int64   `json:"verifying_key_bytes"`
	ProofBytes        int64   `json:"proof_bytes"`
}

type joinSplitDisclosureResourceReport struct {
	GeneratedAtUTC   string                             `json:"generated_at_utc"`
	GOOS             string                             `json:"goos"`
	GOARCH           string                             `json:"goarch"`
	GoVersion        string                             `json:"go_version"`
	GnarkVersion     string                             `json:"gnark_version"`
	Curve            string                             `json:"curve"`
	Backend          string                             `json:"backend"`
	Baseline         joinSplitDisclosureResourceMetrics `json:"baseline"`
	Hardened         joinSplitDisclosureResourceMetrics `json:"hardened"`
	ConstraintDelta  int                                `json:"constraint_delta"`
	BatchConstraints int                                `json:"batch_constraints_unchanged"`
}

func TestJoinSplitDisclosureBlindingSeparationResourceGate(t *testing.T) {
	if os.Getenv(runJoinSplitDisclosureBlindingFeasibility) != "1" {
		t.Skipf("set %s=1 to run the 2x2 disclosure resource gate", runJoinSplitDisclosureBlindingFeasibility)
	}

	assignment := buildValidJoinSplitAssignment(t)
	baseline := measureJoinSplitDisclosureCircuit(t, &JoinSplitCircuit{}, assignment)
	hardenedAssignment := joinSplitDisclosureBlindingFeasibilityCircuit(*assignment)
	hardened := measureJoinSplitDisclosureCircuit(t, &joinSplitDisclosureBlindingFeasibilityCircuit{}, &hardenedAssignment)
	report := joinSplitDisclosureResourceReport{
		GeneratedAtUTC:   time.Now().UTC().Format(time.RFC3339),
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
		GoVersion:        runtime.Version(),
		GnarkVersion:     "v0.14.0",
		Curve:            "BN254",
		Backend:          "Groth16 development setup",
		Baseline:         baseline,
		Hardened:         hardened,
		ConstraintDelta:  hardened.ConstraintCount - baseline.ConstraintCount,
		BatchConstraints: 1_111_837,
	}
	require.Positive(t, report.ConstraintDelta)
	require.Equal(t, int64(164), report.Baseline.ProofBytes)
	require.Equal(t, int64(164), report.Hardened.ProofBytes)

	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	t.Logf("JOIN_SPLIT_DISCLOSURE_RESOURCE_REPORT=%s", encoded)
}

func compileJoinSplitCircuitForFeasibility(t testing.TB, circuit frontend.Circuit) constraint.ConstraintSystem {
	t.Helper()
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, circuit)
	require.NoError(t, err)
	return ccs
}

func requireJoinSplitSolveResult(t testing.TB, ccs constraint.ConstraintSystem, assignment *JoinSplitCircuit, wantSuccess bool) {
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

func requireJoinSplitHardenedSolveResult(t testing.TB, ccs constraint.ConstraintSystem, assignment *JoinSplitCircuit, wantSuccess bool) {
	t.Helper()
	hardened := joinSplitDisclosureBlindingFeasibilityCircuit(*assignment)
	witness, err := frontend.NewWitness(&hardened, ecc.BN254.ScalarField())
	require.NoError(t, err)
	_, err = ccs.Solve(witness)
	if wantSuccess {
		require.NoError(t, err)
	} else {
		require.Error(t, err)
	}
}

func refreshJoinSplitDisclosureFeasibilityState(t testing.TB, assignment *JoinSplitCircuit) {
	t.Helper()
	outputSpendX := assignment.OutputSpendPubKeys[0].A.X.(*big.Int)
	outputSpendY := assignment.OutputSpendPubKeys[0].A.Y.(*big.Int)
	outputViewX := assignment.OutputViewPubKeys[0].A.X.(*big.Int)
	outputViewY := assignment.OutputViewPubKeys[0].A.Y.(*big.Int)
	ownerSpendX := assignment.InputSpendPubKeys[0].A.X.(*big.Int)
	ownerSpendY := assignment.InputSpendPubKeys[0].A.Y.(*big.Int)
	ownerViewX := assignment.InputViewPubKeys[0].A.X.(*big.Int)
	ownerViewY := assignment.InputViewPubKeys[0].A.Y.(*big.Int)

	commitment := privacytypes.ComputeNoteCommitmentV1(
		outputSpendX,
		outputSpendY,
		outputViewX,
		outputViewY,
		assignment.OutputAmounts[0].(*big.Int),
		assignment.AssetID.(*big.Int),
		assignment.OutputRandomness[0].(*big.Int),
	)
	assignment.Commitments[0] = commitment
	commitmentBytes := mustCanonicalFieldBytesFromBigIntForTest(t, commitment)
	policy := uint32(assignment.UserPrivacyPolicy.(*big.Int).Uint64())
	userDigest, err := privacytypes.ComputeTransferDisclosureDigestBytes(
		policy,
		privacytypes.TransferDisclosureRecipientOutputIndex,
		commitmentBytes,
		assignment.OutputAmounts[0].(*big.Int),
		assignment.AssetID.(*big.Int),
		ownerSpendX,
		ownerSpendY,
		ownerViewX,
		ownerViewY,
		outputSpendX,
		outputSpendY,
		outputViewX,
		outputViewY,
		assignment.UserDisclosureBlinding.(*big.Int),
	)
	require.NoError(t, err)
	assignment.UserDisclosureDigest = new(big.Int).SetBytes(userDigest)
	fullDigest, err := privacytypes.ComputeFullTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		commitmentBytes,
		assignment.OutputAmounts[0].(*big.Int),
		assignment.AssetID.(*big.Int),
		ownerSpendX,
		ownerSpendY,
		ownerViewX,
		ownerViewY,
		outputSpendX,
		outputSpendY,
		outputViewX,
		outputViewY,
		assignment.FullDisclosureBlinding.(*big.Int),
	)
	require.NoError(t, err)
	assignment.FullDisclosureDigest = new(big.Int).SetBytes(fullDigest)

	intent, err := privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi:        assignment.ChainDomainHi.(*big.Int),
		ChainDomainLo:        assignment.ChainDomainLo.(*big.Int),
		MerkleRoot:           assignment.MerkleRoot.(*big.Int),
		AssetID:              assignment.AssetID.(*big.Int),
		Nullifiers:           [2]*big.Int{assignment.Nullifiers[0].(*big.Int), assignment.Nullifiers[1].(*big.Int)},
		Commitments:          [2]*big.Int{assignment.Commitments[0].(*big.Int), assignment.Commitments[1].(*big.Int)},
		UserDisclosureDigest: assignment.UserDisclosureDigest.(*big.Int),
		FullDisclosureDigest: assignment.FullDisclosureDigest.(*big.Int),
		PayloadDigestHi:      assignment.PayloadDigestHi.(*big.Int),
		PayloadDigestLo:      assignment.PayloadDigestLo.(*big.Int),
		ExpiresAtUnix:        assignment.ExpiresAtUnix.(*big.Int).Int64(),
	})
	require.NoError(t, err)
	ownerScalar := big.NewInt(17)
	assignment.OwnerSignature = signSpendMessage(t, intent, ownerScalar, scalarMulBase(ownerScalar))
}

func measureJoinSplitDisclosureCircuit(
	t testing.TB,
	circuit frontend.Circuit,
	assignment frontend.Circuit,
) joinSplitDisclosureResourceMetrics {
	t.Helper()
	started := time.Now()
	ccs := compileJoinSplitCircuitForFeasibility(t, circuit)
	metrics := joinSplitDisclosureResourceMetrics{
		ConstraintCount: ccs.GetNbConstraints(),
		CompileMillis:   durationMillis(time.Since(started)),
		R1CSBytes:       serializedSize(t, ccs),
	}
	started = time.Now()
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)
	metrics.SetupMillis = durationMillis(time.Since(started))
	metrics.ProvingKeyBytes = serializedSize(t, pk)
	metrics.VerifyingKeyBytes = serializedSize(t, vk)

	started = time.Now()
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	metrics.WitnessMillis = durationMillis(time.Since(started))
	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	started = time.Now()
	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(t, err)
	metrics.ProveMillis = durationMillis(time.Since(started))
	metrics.ProofBytes = serializedSize(t, proof)
	started = time.Now()
	require.NoError(t, groth16.Verify(proof, vk, publicWitness))
	metrics.VerifyMillis = durationMillis(time.Since(started))
	return metrics
}
