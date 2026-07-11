package zk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestFromChecksumsBuildsDescriptors(t *testing.T) {
	manifest := ManifestFromChecksums(
		"/tmp/privacy-artifacts",
		"2026-04-15T00:00:00Z",
		map[string]string{
			SpendR1CSSHA256Env:   "spend-r1cs",
			JoinSplitVKSHA256Env: "joinsplit-vk",
		},
	)

	require.Equal(t, CircuitConfigSchemaVersion, manifest.SchemaVersion)
	require.Equal(t, ActiveCircuitSetID, manifest.ActiveSetID)
	require.Equal(t, CircuitCurve, manifest.Curve)
	require.Len(t, manifest.Artifacts, 12)
	require.Equal(t, "deposit", manifest.Artifacts[0].CircuitID)
	require.Equal(t, "spend-r1cs", manifest.Artifacts[3].SHA256)
	require.Equal(t, "joinsplit-vk", manifest.Artifacts[8].SHA256)
	require.Equal(t, "batch-joinsplit-16x32-v1", manifest.Artifacts[9].CircuitID)
}

func TestLoadArtifactManifestSupportsStructuredManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ArtifactManifestFile)
	checksums := make(map[string]string)
	for _, descriptor := range DefaultArtifactDescriptors() {
		checksums[descriptor.ChecksumEnv] = strings.Repeat("a", 64)
	}
	manifest := ManifestFromChecksums(dir, "2026-04-15T00:00:00Z", checksums)

	bz, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bz, 0o600))

	loaded, err := LoadArtifactManifest(path)
	require.NoError(t, err)
	require.Equal(t, manifest.SchemaVersion, loaded.SchemaVersion)
	require.Len(t, loaded.Artifacts, 12)
	require.Equal(t, strings.Repeat("a", 64), loaded.Artifacts[0].SHA256)
}

func TestLoadArtifactManifestRejectsLegacyChecksumsJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LegacyChecksumsJSONFile)
	legacy := legacyChecksumsManifest{
		GeneratedAt: "2026-04-15T00:00:00Z",
		Curve:       CircuitCurve,
		ArtifactDir: dir,
		Checksums: map[string]string{
			SpendR1CSSHA256Env: "spend-r1cs",
		},
	}

	bz, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bz, 0o600))

	_, err = LoadArtifactManifest(path)
	require.ErrorContains(t, err, "legacy artifact manifests are not accepted")
}

func TestLoadArtifactManifestRejectsUnknownStructuredFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ArtifactManifestFile)
	checksums := validPlaceholderChecksums()
	manifest := ManifestFromChecksums(dir, "", checksums)
	bz, err := json.Marshal(manifest)
	require.NoError(t, err)
	bz = append(bz[:len(bz)-1], []byte(`,"unexpected":true}`)...)
	require.NoError(t, os.WriteFile(path, bz, 0o600))

	_, err = LoadArtifactManifest(path)
	require.ErrorContains(t, err, "unknown field")
}

func TestResolveRuntimeArtifactManifestFallsBackToEnvChecksums(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(SpendR1CSSHA256Env, "spend-r1cs")

	manifest, source, err := ResolveRuntimeArtifactManifest()
	require.NoError(t, err)
	require.Equal(t, ChecksumSourceEnv, source)
	require.Equal(t, dir, manifest.ArtifactDir)
	require.Len(t, manifest.Artifacts, 12)
	require.Equal(t, "spend-r1cs", manifest.Artifacts[3].SHA256)
}

func TestDefaultArtifactDescriptorsHaveCanonicalCountAndOrder(t *testing.T) {
	require.Equal(t, []ArtifactDescriptor{
		{CircuitID: "deposit", ArtifactType: "r1cs", Filename: DepositR1CSFile, ChecksumEnv: DepositR1CSSHA256Env},
		{CircuitID: "deposit", ArtifactType: "proving_key", Filename: DepositPKFile, ChecksumEnv: DepositPKSHA256Env},
		{CircuitID: "deposit", ArtifactType: "verifying_key", Filename: DepositVKFile, ChecksumEnv: DepositVKSHA256Env},
		{CircuitID: "spend", ArtifactType: "r1cs", Filename: SpendR1CSFile, ChecksumEnv: SpendR1CSSHA256Env},
		{CircuitID: "spend", ArtifactType: "proving_key", Filename: SpendPKFile, ChecksumEnv: SpendPKSHA256Env},
		{CircuitID: "spend", ArtifactType: "verifying_key", Filename: SpendVKFile, ChecksumEnv: SpendVKSHA256Env},
		{CircuitID: "joinsplit", ArtifactType: "r1cs", Filename: JoinSplitR1CSFile, ChecksumEnv: JoinSplitR1CSSHA256Env},
		{CircuitID: "joinsplit", ArtifactType: "proving_key", Filename: JoinSplitPKFile, ChecksumEnv: JoinSplitPKSHA256Env},
		{CircuitID: "joinsplit", ArtifactType: "verifying_key", Filename: JoinSplitVKFile, ChecksumEnv: JoinSplitVKSHA256Env},
		{CircuitID: "batch-joinsplit-16x32-v1", ArtifactType: "r1cs", Filename: BatchJoinSplit16x32R1CSFile, ChecksumEnv: BatchJoinSplit16x32R1CSSHA256Env},
		{CircuitID: "batch-joinsplit-16x32-v1", ArtifactType: "proving_key", Filename: BatchJoinSplit16x32PKFile, ChecksumEnv: BatchJoinSplit16x32PKSHA256Env},
		{CircuitID: "batch-joinsplit-16x32-v1", ArtifactType: "verifying_key", Filename: BatchJoinSplit16x32VKFile, ChecksumEnv: BatchJoinSplit16x32VKSHA256Env},
	}, DefaultArtifactDescriptors())
}
