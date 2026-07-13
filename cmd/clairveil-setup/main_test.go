package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestSelectiveArtifactRotationInstallsValidatedSetAndRemovesBackup(t *testing.T) {
	outDir := writeValidArtifactSetFixture(t)
	before := readDirectoryFixture(t, outDir)

	checksums, err := rotateSelectiveArtifactSet(outDir, rotatedJoinSplitFixtureDefinitions(), defaultArtifactSetOps())
	require.NoError(t, err)
	require.NotEmpty(t, checksums)
	require.NotEqual(t, before, readDirectoryFixture(t, outDir))
	require.NoError(t, validateExistingArtifactSet(outDir))
	requireNoArtifactRotationRecoveryDirectories(t, outDir)
}

func TestSelectiveArtifactRotationInstallFailureRollsBackBackup(t *testing.T) {
	outDir := writeValidArtifactSetFixture(t)
	before := readDirectoryFixture(t, outDir)
	ops := defaultArtifactSetOps()
	renameCalls := 0
	ops.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("injected staged install failure")
		}
		return os.Rename(oldPath, newPath)
	}

	_, err := rotateSelectiveArtifactSet(outDir, rotatedJoinSplitFixtureDefinitions(), ops)
	require.ErrorContains(t, err, "injected staged install failure")
	require.Equal(t, before, readDirectoryFixture(t, outDir))
	require.NoError(t, validateExistingArtifactSet(outDir))
	requireNoArtifactRotationRecoveryDirectories(t, outDir)
}

func TestSelectiveArtifactRotationInstallAndRollbackFailurePreservesRecoverySets(t *testing.T) {
	outDir := writeValidArtifactSetFixture(t)
	before := readDirectoryFixture(t, outDir)
	ops := defaultArtifactSetOps()
	renameCalls := 0
	ops.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("injected staged install failure")
		}
		if renameCalls == 3 {
			return errors.New("injected rollback failure")
		}
		return os.Rename(oldPath, newPath)
	}

	_, err := rotateSelectiveArtifactSet(outDir, rotatedJoinSplitFixtureDefinitions(), ops)
	require.ErrorContains(t, err, "injected staged install failure")
	require.ErrorContains(t, err, "injected rollback failure")
	require.NoDirExists(t, outDir)

	stagingMatches, globErr := filepath.Glob(filepath.Join(filepath.Dir(outDir), "."+filepath.Base(outDir)+".staging-*"))
	require.NoError(t, globErr)
	require.Len(t, stagingMatches, 1)
	backupMatches, globErr := filepath.Glob(filepath.Join(filepath.Dir(outDir), "."+filepath.Base(outDir)+".backup-*"))
	require.NoError(t, globErr)
	require.Len(t, backupMatches, 1)

	require.ErrorContains(t, err, "recovery paths: staging="+stagingMatches[0]+" backup="+backupMatches[0])
	require.NoError(t, validateExistingArtifactSet(stagingMatches[0]))
	require.Equal(t, before, readDirectoryFixture(t, backupMatches[0]))
	require.NoError(t, validateExistingArtifactSet(backupMatches[0]))
}

func TestSelectiveArtifactRotationFailuresPreserveRetryableKnownGoodSet(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*artifactSetOps, *[]artifactDefinition)
	}{
		{
			name: "second artifact write",
			configure: func(_ *artifactSetOps, definitions *[]artifactDefinition) {
				(*definitions)[1].write = func(string) error { return errors.New("injected second artifact write failure") }
			},
		},
		{
			name: "env manifest write",
			configure: func(ops *artifactSetOps, _ *[]artifactDefinition) {
				ops.writeEnvManifest = func(string, string, map[string]string) error { return errors.New("injected env manifest failure") }
			},
		},
		{
			name: "legacy manifest write",
			configure: func(ops *artifactSetOps, _ *[]artifactDefinition) {
				ops.writeLegacyChecksumsJSON = func(string, string, map[string]string) error { return errors.New("injected legacy manifest failure") }
			},
		},
		{
			name: "structured manifest write",
			configure: func(ops *artifactSetOps, _ *[]artifactDefinition) {
				ops.writeJSONManifest = func(string, string, map[string]string) error {
					return errors.New("injected structured manifest failure")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outDir := writeValidArtifactSetFixture(t)
			before := readDirectoryFixture(t, outDir)
			ops := defaultArtifactSetOps()
			definitions := rotatedJoinSplitFixtureDefinitions()
			tc.configure(&ops, &definitions)

			_, err := rotateSelectiveArtifactSet(outDir, definitions, ops)
			require.Error(t, err)
			require.Equal(t, before, readDirectoryFixture(t, outDir))
			require.NoError(t, validateExistingArtifactSet(outDir))

			_, err = rotateSelectiveArtifactSet(outDir, rotatedJoinSplitFixtureDefinitions(), defaultArtifactSetOps())
			require.NoError(t, err)
			require.NoError(t, validateExistingArtifactSet(outDir))
		})
	}
}

func TestSelectiveArtifactRotationReportsBackupCleanupFailure(t *testing.T) {
	outDir := writeValidArtifactSetFixture(t)
	before := readDirectoryFixture(t, outDir)
	ops := defaultArtifactSetOps()
	ops.removeAll = func(path string) error {
		if strings.Contains(filepath.Base(path), ".backup-") {
			return errors.New("injected backup cleanup failure")
		}
		return os.RemoveAll(path)
	}

	checksums, err := rotateSelectiveArtifactSet(outDir, rotatedJoinSplitFixtureDefinitions(), ops)
	require.ErrorContains(t, err, "installed staged artifact set but failed to remove backup artifact set")
	require.ErrorContains(t, err, "remove it manually")
	require.NotEmpty(t, checksums)
	require.NotEqual(t, before, readDirectoryFixture(t, outDir))
	require.NoError(t, validateExistingArtifactSet(outDir))

	backupMatches, globErr := filepath.Glob(filepath.Join(filepath.Dir(outDir), "."+filepath.Base(outDir)+".backup-*"))
	require.NoError(t, globErr)
	require.Len(t, backupMatches, 1)
	require.Equal(t, before, readDirectoryFixture(t, backupMatches[0]))
	require.NoError(t, os.RemoveAll(backupMatches[0]))

	_, err = rotateSelectiveArtifactSet(outDir, rotatedJoinSplitFixtureDefinitions(), defaultArtifactSetOps())
	require.NoError(t, err)
	require.NoError(t, validateExistingArtifactSet(outDir))
}

func TestSelectiveArtifactRotationReportsStagingCleanupFailure(t *testing.T) {
	outDir := writeValidArtifactSetFixture(t)
	before := readDirectoryFixture(t, outDir)
	definitions := rotatedJoinSplitFixtureDefinitions()
	definitions[0].write = func(string) error { return errors.New("injected artifact write failure") }
	ops := defaultArtifactSetOps()
	ops.removeAll = func(path string) error {
		if strings.Contains(filepath.Base(path), ".staging-") {
			return errors.New("injected staging cleanup failure")
		}
		return os.RemoveAll(path)
	}

	_, err := rotateSelectiveArtifactSet(outDir, definitions, ops)
	require.ErrorContains(t, err, "injected artifact write failure")
	require.ErrorContains(t, err, "remove staging artifact set")
	require.ErrorContains(t, err, "injected staging cleanup failure")
	require.Equal(t, before, readDirectoryFixture(t, outDir))
	require.NoError(t, validateExistingArtifactSet(outDir))

	stagingMatches, globErr := filepath.Glob(filepath.Join(filepath.Dir(outDir), "."+filepath.Base(outDir)+".staging-*"))
	require.NoError(t, globErr)
	require.Len(t, stagingMatches, 1)
	require.NoError(t, os.RemoveAll(stagingMatches[0]))
}

func writeValidArtifactSetFixture(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	for i, descriptor := range zk.DefaultArtifactDescriptors() {
		require.NoError(t, os.WriteFile(
			filepath.Join(outDir, descriptor.Filename),
			[]byte(fmt.Sprintf("artifact-%02d-before", i)),
			0o600,
		))
	}
	checksums, err := checksumArtifactSet(outDir)
	require.NoError(t, err)
	require.NoError(t, writeEnvManifest(filepath.Join(outDir, "privacy_zk_checksums.env"), outDir, checksums))
	require.NoError(t, writeLegacyChecksumsJSON(filepath.Join(outDir, zk.LegacyChecksumsJSONFile), outDir, checksums))
	require.NoError(t, writeJSONManifest(filepath.Join(outDir, zk.ArtifactManifestFile), outDir, checksums))
	require.NoError(t, validateExistingArtifactSet(outDir))
	return outDir
}

func rotatedJoinSplitFixtureDefinitions() []artifactDefinition {
	filenames := []string{zk.JoinSplitR1CSFile, zk.JoinSplitPKFile, zk.JoinSplitVKFile}
	definitions := make([]artifactDefinition, 0, len(filenames))
	for _, filename := range filenames {
		filename := filename
		definitions = append(definitions, artifactDefinition{
			filename: filename,
			write: func(outDir string) error {
				return os.WriteFile(filepath.Join(outDir, filename), []byte("rotated-"+filename), 0o600)
			},
		})
	}
	return definitions
}

func readDirectoryFixture(t *testing.T, dir string) map[string]string {
	t.Helper()
	result := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relative] = string(content)
		return nil
	}))
	return result
}

func requireNoArtifactRotationRecoveryDirectories(t *testing.T, outDir string) {
	t.Helper()
	for _, kind := range []string{"staging", "backup"} {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(outDir), "."+filepath.Base(outDir)+"."+kind+"-*"))
		require.NoError(t, err)
		require.Empty(t, matches)
	}
}
