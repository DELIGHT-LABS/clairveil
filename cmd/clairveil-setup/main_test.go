package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func TestChecksumEnvironmentOrderMatchesCanonicalArtifactDescriptors(t *testing.T) {
	descriptors := zk.DefaultArtifactDescriptors()
	require.Len(t, descriptors, 12)

	want := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		want[i] = descriptor.ChecksumEnv
	}
	require.Equal(t, want, checksumEnvironmentOrder())
}

func TestWriteEnvManifestIncludesBatchArtifactsInCanonicalOrder(t *testing.T) {
	checksums := make(map[string]string)
	want := fmt.Sprintf("%s=%s\n", zk.ZKArtifactDirEnv, "/tmp/privacy-artifacts")
	for i, checksumEnv := range checksumEnvironmentOrder() {
		checksums[checksumEnv] = fmt.Sprintf("checksum-%02d", i)
		want += fmt.Sprintf("%s=%s\n", checksumEnv, checksums[checksumEnv])
	}

	path := filepath.Join(t.TempDir(), "privacy_zk_checksums.env")
	require.NoError(t, writeEnvManifest(path, "/tmp/privacy-artifacts", checksums))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}
