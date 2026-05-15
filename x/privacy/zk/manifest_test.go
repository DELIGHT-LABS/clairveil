package zk

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	require.Len(t, manifest.Artifacts, 6)
	require.Equal(t, "spend-r1cs", manifest.Artifacts[0].SHA256)
	require.Equal(t, "joinsplit-vk", manifest.Artifacts[5].SHA256)
}

func TestLoadArtifactManifestSupportsStructuredManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ArtifactManifestFile)
	manifest := RuntimeArtifactManifest{
		SchemaVersion: CircuitConfigSchemaVersion,
		GeneratedAt:   "2026-04-15T00:00:00Z",
		Curve:         CircuitCurve,
		ActiveSetID:   ActiveCircuitSetID,
		ArtifactDir:   dir,
		Artifacts: []ArtifactDescriptor{
			{
				CircuitID:    "spend",
				ArtifactType: "r1cs",
				Filename:     SpendR1CSFile,
				ChecksumEnv:  SpendR1CSSHA256Env,
				SHA256:       "abcd",
			},
		},
	}

	bz, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bz, 0o600))

	loaded, err := LoadArtifactManifest(path)
	require.NoError(t, err)
	require.Equal(t, manifest.SchemaVersion, loaded.SchemaVersion)
	require.Len(t, loaded.Artifacts, 1)
	require.Equal(t, "abcd", loaded.Artifacts[0].SHA256)
}

func TestLoadArtifactManifestSupportsLegacyChecksumsJSON(t *testing.T) {
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

	loaded, err := LoadArtifactManifest(path)
	require.NoError(t, err)
	require.Equal(t, CircuitConfigSchemaVersion, loaded.SchemaVersion)
	require.Len(t, loaded.Artifacts, 6)
	require.Equal(t, "spend-r1cs", loaded.Artifacts[0].SHA256)
}

func TestResolveRuntimeArtifactManifestFallsBackToEnvChecksums(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(SpendR1CSSHA256Env, "spend-r1cs")

	manifest, source, err := ResolveRuntimeArtifactManifest()
	require.NoError(t, err)
	require.Equal(t, ChecksumSourceEnv, source)
	require.Equal(t, dir, manifest.ArtifactDir)
	require.Len(t, manifest.Artifacts, 6)
	require.Equal(t, "spend-r1cs", manifest.Artifacts[0].SHA256)
}
