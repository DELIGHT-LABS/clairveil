package circuit

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/stretchr/testify/require"

	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	runJoinSplitArtifactProofRotationGate = "CLAIRVEIL_RUN_JOINSPLIT_ARTIFACT_PROOF_ROTATION_GATE"
	previousJoinSplitArtifactDirEnv       = "CLAIRVEIL_PRIVACY_PREVIOUS_ZK_ARTIFACT_DIR"
)

func TestJoinSplitOldAndNewProofIdentitiesAreMutuallyExclusive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(runJoinSplitArtifactProofRotationGate)) != "1" {
		t.Skipf("set %s=1 with current and previous artifact directories", runJoinSplitArtifactProofRotationGate)
	}
	currentDir := strings.TrimSpace(os.Getenv(privacyzk.ZKArtifactDirEnv))
	previousDir := strings.TrimSpace(os.Getenv(previousJoinSplitArtifactDirEnv))
	require.NotEmpty(t, currentDir)
	require.NotEmpty(t, previousDir)

	assignment := buildValidJoinSplitAssignment(t)
	fullWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)

	previousR1CS := readJoinSplitConstraintSystem(t, previousDir)
	require.Equal(t, 99_765, previousR1CS.GetNbConstraints())
	previousPK := readJoinSplitProvingKey(t, previousDir)
	previousVK := readJoinSplitVerifyingKey(t, previousDir)
	previousProof, err := groth16.Prove(previousR1CS, previousPK, fullWitness)
	require.NoError(t, err)
	require.NoError(t, groth16.Verify(previousProof, previousVK, publicWitness))
	previousR1CS = nil
	previousPK = nil
	runtime.GC()

	currentR1CS := readJoinSplitConstraintSystem(t, currentDir)
	require.Equal(t, 99_775, currentR1CS.GetNbConstraints())
	currentPK := readJoinSplitProvingKey(t, currentDir)
	currentVK := readJoinSplitVerifyingKey(t, currentDir)
	currentProof, err := groth16.Prove(currentR1CS, currentPK, fullWitness)
	require.NoError(t, err)
	require.NoError(t, groth16.Verify(currentProof, currentVK, publicWitness))
	require.Error(t, groth16.Verify(previousProof, currentVK, publicWitness))
	require.Error(t, groth16.Verify(currentProof, previousVK, publicWitness))
}

func readJoinSplitConstraintSystem(t testing.TB, dir string) constraint.ConstraintSystem {
	t.Helper()
	result := groth16.NewCS(ecc.BN254)
	readJoinSplitArtifact(t, filepath.Join(dir, privacyzk.JoinSplitR1CSFile), result)
	return result
}

func readJoinSplitProvingKey(t testing.TB, dir string) groth16.ProvingKey {
	t.Helper()
	result := groth16.NewProvingKey(ecc.BN254)
	readJoinSplitArtifact(t, filepath.Join(dir, privacyzk.JoinSplitPKFile), result)
	return result
}

func readJoinSplitVerifyingKey(t testing.TB, dir string) groth16.VerifyingKey {
	t.Helper()
	result := groth16.NewVerifyingKey(ecc.BN254)
	readJoinSplitArtifact(t, filepath.Join(dir, privacyzk.JoinSplitVKFile), result)
	return result
}

func readJoinSplitArtifact(t testing.TB, path string, artifact interface {
	ReadFrom(io.Reader) (int64, error)
}) {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	_, err = artifact.ReadFrom(file)
	require.NoError(t, err)
}
