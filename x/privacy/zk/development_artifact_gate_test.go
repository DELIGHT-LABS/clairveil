package zk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	developmentArtifactGateEnv                = "CLAIRVEIL_RUN_BATCH_DEVELOPMENT_ARTIFACT_GATE"
	batchJoinSplit16x32ConstraintCountV1      = 1_111_837
	batchJoinSplit16x32PublicInputSchemaSHAV1 = "5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333"
)

// TestBatchDevelopmentArtifactRoleReadinessGate validates artifacts generated
// outside the repository by cmd/clairveil-setup. It is opt-in because decoding
// the production 16x32 R1CS and proving key requires substantial memory.
func TestBatchDevelopmentArtifactRoleReadinessGate(t *testing.T) {
	if strings.TrimSpace(os.Getenv(developmentArtifactGateEnv)) != "1" {
		t.Skipf("set %s=1 with %s pointing at development artifacts", developmentArtifactGateEnv, ZKArtifactDirEnv)
	}
	artifactDir := strings.TrimSpace(os.Getenv(ZKArtifactDirEnv))
	require.NotEmpty(t, artifactDir)

	validatorReads := map[string]int{}
	validatorRegistry, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir:        artifactDir,
		RuntimeEnvironment: ZKRuntimeEnvironmentDevelopment,
		LookupEnv:          func(string) (string, bool) { return "", false },
		ReadFile: func(path string) ([]byte, error) {
			validatorReads[filepath.Base(path)]++
			return os.ReadFile(path)
		},
	})
	require.NoError(t, err)
	identity, err := validatorRegistry.LocalCircuitSetIdentity()
	require.NoError(t, err)
	require.NoError(t, privacytypes.ValidateCircuitSetIdentity(identity))
	require.Equal(t, batchJoinSplit16x32PublicInputSchemaSHAV1, identity.Circuits[len(identity.Circuits)-1].PublicInputSchemaSha256)
	for name := range validatorReads {
		validatorReads[name] = 0
	}
	require.NoError(t, validatorRegistry.CheckReadiness(
		ArtifactRoleValidator,
		[]CircuitID{CircuitBatchJoinSplit16x32V1},
		identity,
	))
	require.Equal(t, 1, validatorReads[BatchJoinSplit16x32VKFile])
	require.Zero(t, validatorReads[BatchJoinSplit16x32R1CSFile])
	require.Zero(t, validatorReads[BatchJoinSplit16x32PKFile])

	proverReads := map[string]int{}
	proverRegistry, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir:        artifactDir,
		RuntimeEnvironment: ZKRuntimeEnvironmentDevelopment,
		LookupEnv:          func(string) (string, bool) { return "", false },
		ReadFile: func(path string) ([]byte, error) {
			proverReads[filepath.Base(path)]++
			return os.ReadFile(path)
		},
	})
	require.NoError(t, err)
	require.NoError(t, proverRegistry.CheckReadiness(
		ArtifactRoleProver,
		[]CircuitID{CircuitBatchJoinSplit16x32V1},
		identity,
	))
	require.Equal(t, 1, proverReads[BatchJoinSplit16x32R1CSFile])
	require.Equal(t, 1, proverReads[BatchJoinSplit16x32PKFile])
	require.Zero(t, proverReads[BatchJoinSplit16x32VKFile])
	batchR1CS, err := proverRegistry.R1CS(CircuitBatchJoinSplit16x32V1)
	require.NoError(t, err)
	require.Equal(t, batchJoinSplit16x32ConstraintCountV1, batchR1CS.GetNbConstraints())
}
