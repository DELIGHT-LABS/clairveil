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
	joinSplitArtifactRotationGateEnv          = "CLAIRVEIL_RUN_JOINSPLIT_ARTIFACT_ROTATION_GATE"
	previousArtifactDirEnv                    = "CLAIRVEIL_PRIVACY_PREVIOUS_ZK_ARTIFACT_DIR"
	batchJoinSplit16x32ConstraintCountV1      = 1_111_837
	batchJoinSplit16x32PublicInputSchemaSHAV1 = "5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333"
	joinSplitConstraintCountS4B02             = 99_775
	joinSplitPublicInputSchemaSHAV2           = "4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82"
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

// TestJoinSplitDevelopmentArtifactRotationGate validates a real JoinSplit-only
// development rotation against the previous complete artifact set. It remains
// opt-in because decoding the proving artifacts is a resource gate, not an
// ordinary unit test.
func TestJoinSplitDevelopmentArtifactRotationGate(t *testing.T) {
	if strings.TrimSpace(os.Getenv(joinSplitArtifactRotationGateEnv)) != "1" {
		t.Skipf("set %s=1 with current and previous artifact directories", joinSplitArtifactRotationGateEnv)
	}
	currentDir := strings.TrimSpace(os.Getenv(ZKArtifactDirEnv))
	previousDir := strings.TrimSpace(os.Getenv(previousArtifactDirEnv))
	require.NotEmpty(t, currentDir)
	require.NotEmpty(t, previousDir)

	currentManifest, err := LoadArtifactManifest(filepath.Join(currentDir, ArtifactManifestFile))
	require.NoError(t, err)
	previousManifest, err := LoadArtifactManifest(filepath.Join(previousDir, ArtifactManifestFile))
	require.NoError(t, err)
	require.Equal(t, previousManifest.ActiveSetID, currentManifest.ActiveSetID)
	require.Equal(t, previousManifest.SchemaVersion, currentManifest.SchemaVersion)
	require.Equal(t, previousManifest.Curve, currentManifest.Curve)
	require.Len(t, currentManifest.Artifacts, len(previousManifest.Artifacts))
	for i, previous := range previousManifest.Artifacts {
		current := currentManifest.Artifacts[i]
		require.Equal(t, previous.CircuitID, current.CircuitID)
		require.Equal(t, previous.ArtifactType, current.ArtifactType)
		require.Equal(t, previous.Filename, current.Filename)
		require.Equal(t, previous.ChecksumEnv, current.ChecksumEnv)
		if current.CircuitID == string(CircuitJoinSplit) {
			require.NotEqual(t, previous.SHA256, current.SHA256)
			continue
		}
		require.Equal(t, previous.SHA256, current.SHA256)
		require.FileExists(t, filepath.Join(currentDir, current.Filename))
		require.FileExists(t, filepath.Join(previousDir, previous.Filename))
	}

	currentIdentity := currentManifest.CircuitSetIdentity
	previousIdentity := previousManifest.CircuitSetIdentity
	require.Equal(t, joinSplitPublicInputSchemaSHAV2, currentIdentity.Circuits[2].PublicInputSchemaSha256)
	for i, previous := range previousIdentity.Circuits {
		current := currentIdentity.Circuits[i]
		require.Equal(t, previous.CircuitId, current.CircuitId)
		require.Equal(t, previous.PublicInputSchemaSha256, current.PublicInputSchemaSha256)
		if current.CircuitId == string(CircuitJoinSplit) {
			require.NotEqual(t, previous.VerifyingKeySha256, current.VerifyingKeySha256)
			continue
		}
		require.Equal(t, previous.VerifyingKeySha256, current.VerifyingKeySha256)
	}

	validatorReads := map[string]int{}
	validatorRegistry := newDevelopmentArtifactRegistryForGate(t, currentDir, func(path string) ([]byte, error) {
		validatorReads[filepath.Base(path)]++
		return os.ReadFile(path)
	})
	require.NoError(t, validatorRegistry.CheckReadiness(
		ArtifactRoleValidator,
		[]CircuitID{CircuitJoinSplit},
		currentIdentity,
	))
	require.Equal(t, 1, validatorReads[JoinSplitVKFile])
	require.Zero(t, validatorReads[JoinSplitR1CSFile])
	require.Zero(t, validatorReads[JoinSplitPKFile])
	require.ErrorContains(t, validatorRegistry.CheckReadiness(
		ArtifactRoleValidator,
		[]CircuitID{CircuitJoinSplit},
		previousIdentity,
	), "does not match consensus")

	proverReads := map[string]int{}
	proverRegistry := newDevelopmentArtifactRegistryForGate(t, currentDir, func(path string) ([]byte, error) {
		proverReads[filepath.Base(path)]++
		return os.ReadFile(path)
	})
	require.NoError(t, proverRegistry.CheckReadiness(
		ArtifactRoleProver,
		[]CircuitID{CircuitJoinSplit},
		currentIdentity,
	))
	require.Equal(t, 1, proverReads[JoinSplitR1CSFile])
	require.Equal(t, 1, proverReads[JoinSplitPKFile])
	require.Zero(t, proverReads[JoinSplitVKFile])
	joinSplitR1CS, err := proverRegistry.R1CS(CircuitJoinSplit)
	require.NoError(t, err)
	require.Equal(t, joinSplitConstraintCountS4B02, joinSplitR1CS.GetNbConstraints())

	for _, oldFilename := range []string{JoinSplitR1CSFile, JoinSplitPKFile, JoinSplitVKFile} {
		registry := newDevelopmentArtifactRegistryForGate(t, currentDir, func(path string) ([]byte, error) {
			if filepath.Base(path) == oldFilename {
				return os.ReadFile(filepath.Join(previousDir, oldFilename))
			}
			return os.ReadFile(path)
		})
		role := ArtifactRoleProver
		if oldFilename == JoinSplitVKFile {
			role = ArtifactRoleValidator
		}
		err := registry.CheckReadiness(role, []CircuitID{CircuitJoinSplit}, currentIdentity)
		require.ErrorContains(t, err, "checksum mismatch")
	}
}

func newDevelopmentArtifactRegistryForGate(
	t *testing.T,
	artifactDir string,
	readFile func(string) ([]byte, error),
) *ArtifactRegistry {
	t.Helper()
	registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir:        artifactDir,
		RuntimeEnvironment: ZKRuntimeEnvironmentDevelopment,
		LookupEnv:          func(string) (string, bool) { return "", false },
		ReadFile:           readFile,
	})
	require.NoError(t, err)
	return registry
}
