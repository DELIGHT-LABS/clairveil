package zk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	joinSplitHardenedConstraintCount          = 99_775
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

	currentSnapshot, previousSnapshot, err := validateJoinSplitOnlyArtifactRotation(currentDir, previousDir)
	require.NoError(t, err)
	currentManifest := currentSnapshot.manifest
	previousManifest := previousSnapshot.manifest
	require.Equal(t, previousManifest.ActiveSetID, currentManifest.ActiveSetID)
	require.Equal(t, previousManifest.SchemaVersion, currentManifest.SchemaVersion)
	require.Equal(t, previousManifest.Curve, currentManifest.Curve)
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
	require.Equal(t, joinSplitHardenedConstraintCount, joinSplitR1CS.GetNbConstraints())

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

type verifiedArtifactSnapshot struct {
	manifest *RuntimeArtifactManifest
	digests  map[string]string
}

func validateJoinSplitOnlyArtifactRotation(currentDir, previousDir string) (*verifiedArtifactSnapshot, *verifiedArtifactSnapshot, error) {
	current, err := loadVerifiedArtifactSnapshot(currentDir)
	if err != nil {
		return nil, nil, fmt.Errorf("current artifact snapshot: %w", err)
	}
	previous, err := loadVerifiedArtifactSnapshot(previousDir)
	if err != nil {
		return nil, nil, fmt.Errorf("previous artifact snapshot: %w", err)
	}
	if len(current.manifest.Artifacts) != len(previous.manifest.Artifacts) {
		return nil, nil, fmt.Errorf("current and previous artifact descriptor counts differ")
	}
	for i, currentDescriptor := range current.manifest.Artifacts {
		previousDescriptor := previous.manifest.Artifacts[i]
		if currentDescriptor.CircuitID != previousDescriptor.CircuitID ||
			currentDescriptor.ArtifactType != previousDescriptor.ArtifactType ||
			currentDescriptor.Filename != previousDescriptor.Filename ||
			currentDescriptor.ChecksumEnv != previousDescriptor.ChecksumEnv {
			return nil, nil, fmt.Errorf("current and previous artifact descriptor %d differ", i)
		}
		currentDigest := current.digests[currentDescriptor.Filename]
		previousDigest := previous.digests[previousDescriptor.Filename]
		if currentDescriptor.CircuitID == string(CircuitJoinSplit) {
			if currentDigest == previousDigest {
				return nil, nil, fmt.Errorf("JoinSplit artifact did not rotate: %s", currentDescriptor.Filename)
			}
			continue
		}
		if currentDigest != previousDigest {
			return nil, nil, fmt.Errorf("non-JoinSplit artifact changed during JoinSplit-only rotation: %s", currentDescriptor.Filename)
		}
	}
	return current, previous, nil
}

func loadVerifiedArtifactSnapshot(artifactDir string) (*verifiedArtifactSnapshot, error) {
	manifest, err := LoadArtifactManifest(filepath.Join(artifactDir, ArtifactManifestFile))
	if err != nil {
		return nil, err
	}
	digests := make(map[string]string, len(manifest.Artifacts))
	for _, descriptor := range manifest.Artifacts {
		digest, err := sha256ArtifactFile(filepath.Join(artifactDir, descriptor.Filename))
		if err != nil {
			return nil, fmt.Errorf("failed to hash required artifact %s: %w", descriptor.Filename, err)
		}
		if digest != descriptor.SHA256 {
			return nil, fmt.Errorf("actual sha256 for %s does not match manifest: got %s want %s", descriptor.Filename, digest, descriptor.SHA256)
		}
		digests[descriptor.Filename] = digest
	}
	return &verifiedArtifactSnapshot{manifest: manifest, digests: digests}, nil
}

func sha256ArtifactFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func TestJoinSplitArtifactRotationSnapshotValidation(t *testing.T) {
	t.Run("accepts JoinSplit-only rotation", func(t *testing.T) {
		previousDir := writeArtifactSnapshotFixture(t, "previous", nil)
		currentDir := writeArtifactSnapshotFixture(t, "current", nil)
		_, _, err := validateJoinSplitOnlyArtifactRotation(currentDir, previousDir)
		require.NoError(t, err)
	})

	for _, filename := range []string{DepositR1CSFile, SpendPKFile, BatchJoinSplit16x32VKFile} {
		t.Run("rejects stale manifest after "+filename+" byte change", func(t *testing.T) {
			previousDir := writeArtifactSnapshotFixture(t, "previous", nil)
			currentDir := writeArtifactSnapshotFixture(t, "current", nil)
			require.NoError(t, os.WriteFile(filepath.Join(currentDir, filename), []byte("tampered"), 0o600))
			_, _, err := validateJoinSplitOnlyArtifactRotation(currentDir, previousDir)
			require.ErrorContains(t, err, "actual sha256")
		})
	}

	t.Run("rejects manifest-backed non-JoinSplit byte change", func(t *testing.T) {
		previousDir := writeArtifactSnapshotFixture(t, "previous", nil)
		currentDir := writeArtifactSnapshotFixture(t, "current", map[string]string{DepositR1CSFile: "changed non-JoinSplit r1cs"})
		_, _, err := validateJoinSplitOnlyArtifactRotation(currentDir, previousDir)
		require.ErrorContains(t, err, "non-JoinSplit artifact changed")
	})

	t.Run("rejects missing required artifact", func(t *testing.T) {
		previousDir := writeArtifactSnapshotFixture(t, "previous", nil)
		currentDir := writeArtifactSnapshotFixture(t, "current", nil)
		require.NoError(t, os.Remove(filepath.Join(currentDir, SpendVKFile)))
		_, _, err := validateJoinSplitOnlyArtifactRotation(currentDir, previousDir)
		require.ErrorContains(t, err, "failed to hash required artifact")
	})

	for _, tc := range []struct {
		name   string
		mutate func(*RuntimeArtifactManifest)
		want   string
	}{
		{name: "missing descriptor", mutate: func(manifest *RuntimeArtifactManifest) {
			manifest.Artifacts = manifest.Artifacts[:len(manifest.Artifacts)-1]
		}, want: "must contain exactly"},
		{name: "duplicate descriptor", mutate: func(manifest *RuntimeArtifactManifest) {
			manifest.Artifacts = append(manifest.Artifacts, manifest.Artifacts[0])
		}, want: "must contain exactly"},
		{name: "unknown descriptor", mutate: func(manifest *RuntimeArtifactManifest) {
			manifest.Artifacts[0].Filename = "unknown.bin"
		}, want: "does not match the canonical descriptor"},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			previousDir := writeArtifactSnapshotFixture(t, "previous", nil)
			currentDir := writeArtifactSnapshotFixture(t, "current", nil)
			manifest := readArtifactSnapshotFixtureManifest(t, currentDir)
			tc.mutate(&manifest)
			writeArtifactSnapshotFixtureManifest(t, currentDir, manifest)
			_, _, err := validateJoinSplitOnlyArtifactRotation(currentDir, previousDir)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func writeArtifactSnapshotFixture(t *testing.T, joinSplitVersion string, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	checksums := make(map[string]string, len(DefaultArtifactDescriptors()))
	for _, descriptor := range DefaultArtifactDescriptors() {
		content := "stable:" + descriptor.Filename
		if descriptor.CircuitID == string(CircuitJoinSplit) {
			content = joinSplitVersion + ":" + descriptor.Filename
		}
		if override, ok := overrides[descriptor.Filename]; ok {
			content = override
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, descriptor.Filename), []byte(content), 0o600))
		digest := sha256.Sum256([]byte(content))
		checksums[descriptor.ChecksumEnv] = hex.EncodeToString(digest[:])
	}
	manifest := ManifestFromChecksums(dir, "2026-07-13T00:00:00Z", checksums)
	writeArtifactSnapshotFixtureManifest(t, dir, manifest)
	return dir
}

func readArtifactSnapshotFixtureManifest(t *testing.T, dir string) RuntimeArtifactManifest {
	t.Helper()
	bz, err := os.ReadFile(filepath.Join(dir, ArtifactManifestFile))
	require.NoError(t, err)
	var manifest RuntimeArtifactManifest
	require.NoError(t, json.Unmarshal(bz, &manifest))
	return manifest
}

func writeArtifactSnapshotFixtureManifest(t *testing.T, dir string, manifest RuntimeArtifactManifest) {
	t.Helper()
	bz, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ArtifactManifestFile), append(bz, '\n'), 0o600))
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
