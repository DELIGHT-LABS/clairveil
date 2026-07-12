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

func TestParseSetupCircuit(t *testing.T) {
	for _, value := range []string{
		setupCircuitAll,
		setupCircuitDeposit,
		setupCircuitSpend,
		setupCircuitJoinSplit,
		setupCircuitBatchJoinSplit16x32V1,
	} {
		got, err := parseSetupCircuit("  " + value + "  ")
		require.NoError(t, err)
		require.Equal(t, value, got)
	}
	_, err := parseSetupCircuit("unknown")
	require.ErrorContains(t, err, "unsupported circuit")
}

func TestSelectiveJoinSplitRotationPreservesOtherArtifactIdentity(t *testing.T) {
	outDir := t.TempDir()
	for i, descriptor := range zk.DefaultArtifactDescriptors() {
		require.NoError(t, os.WriteFile(
			filepath.Join(outDir, descriptor.Filename),
			[]byte(fmt.Sprintf("artifact-%02d-before", i)),
			0o600,
		))
	}

	beforeChecksums, err := checksumArtifactSet(outDir)
	require.NoError(t, err)
	require.NoError(t, writeJSONManifest(filepath.Join(outDir, zk.ArtifactManifestFile), outDir, beforeChecksums))
	require.NoError(t, validateExistingArtifactSet(outDir))
	beforeManifest, err := zk.LoadArtifactManifest(filepath.Join(outDir, zk.ArtifactManifestFile))
	require.NoError(t, err)

	for _, filename := range []string{zk.JoinSplitR1CSFile, zk.JoinSplitPKFile, zk.JoinSplitVKFile} {
		require.NoError(t, os.WriteFile(filepath.Join(outDir, filename), []byte("rotated-"+filename), 0o600))
	}
	require.ErrorContains(t, validateExistingArtifactSet(outDir), "does not match")

	afterChecksums, err := checksumArtifactSet(outDir)
	require.NoError(t, err)
	for _, descriptor := range zk.DefaultArtifactDescriptors() {
		if descriptor.CircuitID == setupCircuitJoinSplit {
			require.NotEqual(t, beforeChecksums[descriptor.ChecksumEnv], afterChecksums[descriptor.ChecksumEnv])
			continue
		}
		require.Equal(t, beforeChecksums[descriptor.ChecksumEnv], afterChecksums[descriptor.ChecksumEnv])
	}

	require.NoError(t, writeJSONManifest(filepath.Join(outDir, zk.ArtifactManifestFile), outDir, afterChecksums))
	require.NoError(t, validateExistingArtifactSet(outDir))
	afterManifest, err := zk.LoadArtifactManifest(filepath.Join(outDir, zk.ArtifactManifestFile))
	require.NoError(t, err)
	require.Len(t, beforeManifest.CircuitSetIdentity.Circuits, 4)
	require.Len(t, afterManifest.CircuitSetIdentity.Circuits, 4)
	for i, beforeCircuit := range beforeManifest.CircuitSetIdentity.Circuits {
		afterCircuit := afterManifest.CircuitSetIdentity.Circuits[i]
		require.Equal(t, beforeCircuit.CircuitId, afterCircuit.CircuitId)
		require.Equal(t, beforeCircuit.PublicInputSchemaSha256, afterCircuit.PublicInputSchemaSha256)
		if beforeCircuit.CircuitId == setupCircuitJoinSplit {
			require.NotEqual(t, beforeCircuit.VerifyingKeySha256, afterCircuit.VerifyingKeySha256)
			continue
		}
		require.Equal(t, beforeCircuit.VerifyingKeySha256, afterCircuit.VerifyingKeySha256)
	}
}
