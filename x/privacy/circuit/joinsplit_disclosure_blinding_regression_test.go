package circuit

import (
	"encoding/json"
	"errors"
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

// joinSplitDisclosureBlindingLegacyControl is deliberately test-only. It
// preserves the pre-S4-B02 relation so every hardened negative can prove that
// no unrelated production constraint caused its rejection.
type joinSplitDisclosureBlindingLegacyControl JoinSplitCircuit

func (c *joinSplitDisclosureBlindingLegacyControl) Define(api frontend.API) error {
	return (*JoinSplitCircuit)(c).defineBase(api)
}

func TestJoinSplitCircuitEnforcesDisclosureBlindingSeparationV1(t *testing.T) {
	legacyControlCCS := compileJoinSplitCircuitForFeasibility(t, &joinSplitDisclosureBlindingLegacyControl{})
	productionCCS := compileJoinSplitCircuitForFeasibility(t, &JoinSplitCircuit{})
	require.Equal(t, 99_765, legacyControlCCS.GetNbConstraints())
	require.Equal(t, 99_775, productionCCS.GetNbConstraints())
	require.Equal(t, 10, productionCCS.GetNbConstraints()-legacyControlCCS.GetNbConstraints())
	t.Logf(
		"JOIN_SPLIT_DISCLOSURE_CONSTRAINT_DELTA legacy_control=%d production=%d delta=%d",
		legacyControlCCS.GetNbConstraints(),
		productionCCS.GetNbConstraints(),
		productionCCS.GetNbConstraints()-legacyControlCCS.GetNbConstraints(),
	)

	valid := buildValidJoinSplitAssignment(t)
	require.NoError(t, validateJoinSplitDisclosureBlindingAssignmentV1(valid))
	requireJoinSplitLegacyControlSolveResult(t, legacyControlCCS, valid, true)
	requireJoinSplitSolveResult(t, productionCCS, valid, true)

	allPrivate := *buildValidJoinSplitAssignment(t)
	allPrivate.UserPrivacyPolicy = big.NewInt(int64(privacytypes.TransferPrivacyPolicyAllPrivate))
	allPrivate.UserDisclosureBlinding = big.NewInt(0)
	allPrivate.OutputRandomness[0] = big.NewInt(0)
	refreshJoinSplitDisclosureFeasibilityState(t, &allPrivate)
	require.NoError(t, validateJoinSplitDisclosureBlindingAssignmentV1(&allPrivate))
	requireJoinSplitLegacyControlSolveResult(t, legacyControlCCS, &allPrivate, true)
	requireJoinSplitSolveResult(t, productionCCS, &allPrivate, true)

	negative := []struct {
		name      string
		errorCode privacytypes.DisclosureBlindingErrorCodeV1
		mutate    func(*JoinSplitCircuit)
	}{
		{
			name:      "DBS-01 user reuses recipient output randomness",
			errorCode: privacytypes.DisclosureBlindingErrorUserRandomnessReuseV1,
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.UserDisclosureBlinding = cloneBigInt(assignment.OutputRandomness[0])
			},
		},
		{
			name:      "DBS-02 full reuses recipient output randomness",
			errorCode: privacytypes.DisclosureBlindingErrorFullRandomnessReuseV1,
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.FullDisclosureBlinding = cloneBigInt(assignment.OutputRandomness[0])
			},
		},
		{
			name:      "DBS-03 full reuses user",
			errorCode: privacytypes.DisclosureBlindingErrorUserFullBlindingReuseV1,
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.FullDisclosureBlinding = cloneBigInt(assignment.UserDisclosureBlinding)
			},
		},
		{
			name:      "all-private user sentinel is non-zero",
			errorCode: privacytypes.DisclosureBlindingErrorAllPrivateUserSentinelV1,
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.UserPrivacyPolicy = big.NewInt(int64(privacytypes.TransferPrivacyPolicyAllPrivate))
				assignment.UserDisclosureBlinding = big.NewInt(59)
			},
		},
		{
			name:      "all-private DBS-02 remains enabled",
			errorCode: privacytypes.DisclosureBlindingErrorFullRandomnessReuseV1,
			mutate: func(assignment *JoinSplitCircuit) {
				assignment.UserPrivacyPolicy = big.NewInt(int64(privacytypes.TransferPrivacyPolicyAllPrivate))
				assignment.UserDisclosureBlinding = big.NewInt(0)
				assignment.FullDisclosureBlinding = cloneBigInt(assignment.OutputRandomness[0])
			},
		},
	}

	for _, tc := range negative {
		t.Run(tc.name, func(t *testing.T) {
			assignment := *buildValidJoinSplitAssignment(t)
			tc.mutate(&assignment)
			refreshJoinSplitDisclosureFeasibilityState(t, &assignment)

			err := validateJoinSplitDisclosureBlindingAssignmentV1(&assignment)
			require.Error(t, err)
			var invariantErr *privacytypes.DisclosureBlindingErrorV1
			require.True(t, errors.As(err, &invariantErr))
			require.Equal(t, tc.errorCode, invariantErr.Code)

			// Complete disclosure digests and the owner signature are refreshed.
			// Legacy-control success proves no pre-existing relation masks the
			// production rejection, while the compiled production R1CS rejects it.
			requireJoinSplitLegacyControlSolveResult(t, legacyControlCCS, &assignment, true)
			requireJoinSplitSolveResult(t, productionCCS, &assignment, false)
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
	LegacyControl    joinSplitDisclosureResourceMetrics `json:"legacy_control"`
	Production       joinSplitDisclosureResourceMetrics `json:"production"`
	ConstraintDelta  int                                `json:"constraint_delta"`
	BatchConstraints int                                `json:"batch_constraints_unchanged"`
}

func TestJoinSplitDisclosureBlindingSeparationResourceGate(t *testing.T) {
	if os.Getenv(runJoinSplitDisclosureBlindingFeasibility) != "1" {
		t.Skipf("set %s=1 to run the 2x2 disclosure resource gate", runJoinSplitDisclosureBlindingFeasibility)
	}

	assignment := buildValidJoinSplitAssignment(t)
	legacyAssignment := joinSplitDisclosureBlindingLegacyControl(*assignment)
	legacyControl := measureJoinSplitDisclosureCircuit(t, &joinSplitDisclosureBlindingLegacyControl{}, &legacyAssignment)
	production := measureJoinSplitDisclosureCircuit(t, &JoinSplitCircuit{}, assignment)
	report := joinSplitDisclosureResourceReport{
		GeneratedAtUTC:   time.Now().UTC().Format(time.RFC3339),
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
		GoVersion:        runtime.Version(),
		GnarkVersion:     "v0.14.0",
		Curve:            "BN254",
		Backend:          "Groth16 development setup",
		LegacyControl:    legacyControl,
		Production:       production,
		ConstraintDelta:  production.ConstraintCount - legacyControl.ConstraintCount,
		BatchConstraints: 1_111_837,
	}
	require.Equal(t, 10, report.ConstraintDelta)
	require.Equal(t, 99_775, report.Production.ConstraintCount)
	require.Equal(t, int64(164), report.LegacyControl.ProofBytes)
	require.Equal(t, int64(164), report.Production.ProofBytes)

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

func requireJoinSplitLegacyControlSolveResult(t testing.TB, ccs constraint.ConstraintSystem, assignment *JoinSplitCircuit, wantSuccess bool) {
	t.Helper()
	legacyControl := joinSplitDisclosureBlindingLegacyControl(*assignment)
	witness, err := frontend.NewWitness(&legacyControl, ecc.BN254.ScalarField())
	require.NoError(t, err)
	_, err = ccs.Solve(witness)
	if wantSuccess {
		require.NoError(t, err)
	} else {
		require.Error(t, err)
	}
}

func validateJoinSplitDisclosureBlindingAssignmentV1(assignment *JoinSplitCircuit) error {
	return privacytypes.ValidateDisclosureBlindingSeparationV1(
		privacytypes.DisclosureBlindingSeparationV1Input{
			OutputIndex:            privacytypes.TransferDisclosureRecipientOutputIndex,
			Enabled:                true,
			PrivacyPolicy:          uint32(assignment.UserPrivacyPolicy.(*big.Int).Uint64()),
			OutputRandomness:       assignment.OutputRandomness[0].(*big.Int),
			UserDisclosureBlinding: assignment.UserDisclosureBlinding.(*big.Int),
			FullDisclosureBlinding: assignment.FullDisclosureBlinding.(*big.Int),
		},
	)
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
