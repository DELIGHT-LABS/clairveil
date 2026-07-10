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
	require.Len(t, manifest.Artifacts, 9)
	require.Equal(t, "deposit", manifest.Artifacts[0].CircuitID)
	require.Equal(t, "spend-r1cs", manifest.Artifacts[3].SHA256)
	require.Equal(t, "joinsplit-vk", manifest.Artifacts[8].SHA256)
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
	require.Len(t, loaded.Artifacts, 9)
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

func TestResolveRuntimeArtifactManifestFallsBackToEnvChecksums(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(SpendR1CSSHA256Env, "spend-r1cs")

	manifest, source, err := ResolveRuntimeArtifactManifest()
	require.NoError(t, err)
	require.Equal(t, ChecksumSourceEnv, source)
	require.Equal(t, dir, manifest.ArtifactDir)
	require.Len(t, manifest.Artifacts, 9)
	require.Equal(t, "spend-r1cs", manifest.Artifacts[3].SHA256)
}
