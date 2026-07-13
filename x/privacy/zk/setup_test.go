package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cosmossdk.io/log/v2"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
)

func resetZKSetupStateForTest() {
	resetDefaultArtifactRegistryForTest()
}

func TestArtifactPathUsesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)

	require.Equal(t, filepath.Join(dir, DepositR1CSFile), artifactPath(DepositR1CSFile))
	require.Equal(t, filepath.Join(dir, SpendR1CSFile), artifactPath(SpendR1CSFile))
	require.Equal(t, filepath.Join(dir, JoinSplitVKFile), artifactPath(JoinSplitVKFile))
	require.Equal(t, filepath.Join(dir, BatchJoinSplit16x32VKFile), artifactPath(BatchJoinSplit16x32VKFile))
}

func TestRuntimeRegistryDevelopmentOverrideRequiresDevelopmentEnvironment(t *testing.T) {
	t.Setenv(ZKArtifactDirEnv, t.TempDir())
	t.Setenv(ZKAllowDevelopmentArtifactOverrideEnv, "true")
	t.Setenv(ZKRuntimeEnvironmentEnv, ZKRuntimeEnvironmentProduction)
	_, err := NewRuntimeArtifactRegistry()
	require.ErrorContains(t, err, "development-only")

	t.Setenv(ZKRuntimeEnvironmentEnv, ZKRuntimeEnvironmentDevelopment)
	_, err = NewRuntimeArtifactRegistry()
	require.NoError(t, err)

	t.Setenv(ZKAllowDevelopmentArtifactOverrideEnv, "1")
	_, err = NewRuntimeArtifactRegistry()
	require.ErrorContains(t, err, "must be \"true\" or \"false\"")
}

func TestDefaultArtifactRegistryIsolatedAcrossEnvironmentChanges(t *testing.T) {
	resetDefaultArtifactRegistryForTest()
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstChecksums := validPlaceholderChecksums()
	secondChecksums := validPlaceholderChecksums()
	secondChecksums[SpendVKSHA256Env] = strings.Repeat("b", sha256.Size*2)
	require.NoError(t, writeTestManifest(firstDir, firstChecksums))
	require.NoError(t, writeTestManifest(secondDir, secondChecksums))

	t.Setenv(ZKArtifactDirEnv, firstDir)
	first, err := LoadLocalCircuitSetIdentity()
	require.NoError(t, err)
	t.Setenv(ZKArtifactDirEnv, secondDir)
	second, err := LoadLocalCircuitSetIdentity()
	require.NoError(t, err)
	require.NotEqual(t, first.Circuits[1].VerifyingKeySha256, second.Circuits[1].VerifyingKeySha256)
}

func TestRuntimeArtifactRegistryEnvironmentFingerprintIncludesBatchChecksums(t *testing.T) {
	t.Setenv(BatchJoinSplit16x32R1CSSHA256Env, strings.Repeat("a", sha256.Size*2))
	first := runtimeArtifactRegistryEnvironmentFingerprint()
	t.Setenv(BatchJoinSplit16x32R1CSSHA256Env, strings.Repeat("b", sha256.Size*2))
	second := runtimeArtifactRegistryEnvironmentFingerprint()
	require.NotEqual(t, first, second)
}

func TestBatchCompatibilityGettersUseBatchDescriptors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()
	require.NoError(t, writeTestArtifacts(dir))
	for _, filename := range []string{JoinSplitR1CSFile, JoinSplitPKFile, JoinSplitVKFile} {
		require.NoError(t, os.Remove(filepath.Join(dir, filename)))
	}

	_, err := GetBatchJoinSplit16x32R1CS()
	require.NoError(t, err)
	_, err = GetBatchJoinSplit16x32ProvingKey()
	require.NoError(t, err)
	_, err = GetBatchJoinSplit16x32VerifyingKey()
	require.NoError(t, err)
}

func TestValidateZKSetupFailsOnMissingArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()

	err := ValidateZKSetup()
	require.Error(t, err)
	require.Contains(t, err.Error(), ArtifactManifestFile)

	_, err = GetSpendR1CS()
	require.Error(t, err)
	require.Contains(t, err.Error(), ArtifactManifestFile)

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 0)
}

func TestValidateZKSetupFailsOnChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()

	data := []byte("not-a-valid-artifact")
	require.NoError(t, os.WriteFile(filepath.Join(dir, DepositR1CSFile), data, 0600))

	bad := make([]byte, 32)
	bad[0] = 0x01
	checksums := validPlaceholderChecksums()
	checksums[DepositR1CSSHA256Env] = hex.EncodeToString(bad)
	require.NoError(t, writeTestManifest(dir, checksums))

	err := ValidateZKSetup()
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestValidateZKArtifactsDoesNotPoisonCachedSetupState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()

	err := ValidateZKArtifacts()
	require.Error(t, err)

	require.NoError(t, writeTestArtifacts(dir))

	_, err = GetSpendR1CS()
	require.NoError(t, err)
}

func TestParseZKPreflightMode(t *testing.T) {
	mode, err := ParseZKPreflightMode("")
	require.NoError(t, err)
	require.Equal(t, ZKPreflightWarn, mode)

	mode, err = ParseZKPreflightMode("strict")
	require.NoError(t, err)
	require.Equal(t, ZKPreflightStrict, mode)

	_, err = ParseZKPreflightMode("bogus")
	require.Error(t, err)
}

func TestRunPreflightWarnAllowsMissingArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(ZKPreflightModeEnv, string(ZKPreflightWarn))
	resetZKSetupStateForTest()

	err := RunPreflight(log.NewNopLogger())
	require.NoError(t, err)
}

func TestRunPreflightStrictRejectsMissingArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(ZKPreflightModeEnv, string(ZKPreflightStrict))
	resetZKSetupStateForTest()

	err := RunPreflight(log.NewNopLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "privacy zk preflight failed")
}

func TestRunProverPreflightChecksOnlySelectedCircuitArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(ZKPreflightModeEnv, string(ZKPreflightStrict))
	require.NoError(t, writeTestArtifacts(dir))
	for _, filename := range []string{DepositR1CSFile, DepositPKFile, DepositVKFile, SpendVKFile, JoinSplitVKFile} {
		require.NoError(t, os.Remove(filepath.Join(dir, filename)))
	}

	err := RunProverPreflight(log.NewNopLogger(), []CircuitID{CircuitSpend, CircuitJoinSplit})
	require.NoError(t, err)
	registry, err := DefaultArtifactRegistry()
	require.NoError(t, err)
	registry.cacheMu.Lock()
	require.NotNil(t, registry.cache["spend:r1cs"].value)
	require.NotNil(t, registry.cache["spend:proving_key"].value)
	require.NotNil(t, registry.cache["joinsplit:r1cs"].value)
	require.NotNil(t, registry.cache["joinsplit:proving_key"].value)
	registry.cacheMu.Unlock()
}

func TestRunPreflightStrictRejectsBatchManifestChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	t.Setenv(ZKPreflightModeEnv, string(ZKPreflightStrict))
	resetZKSetupStateForTest()

	require.NoError(t, writeTestArtifacts(dir))

	manifest, _, err := ResolveRuntimeArtifactManifest()
	require.NoError(t, err)
	descriptors := manifest.Artifacts
	badChecksum := make([]byte, 32)
	badChecksum[0] = 0x01
	descriptors[9].SHA256 = hex.EncodeToString(badChecksum)
	manifest.Artifacts = descriptors
	bz, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ArtifactManifestFile), bz, 0o600))

	err = RunPreflight(log.NewNopLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "privacy zk preflight failed")
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestRunPreflightStrictRejectsIncompleteManifestChecksums(t *testing.T) {
	testCases := []struct {
		name              string
		updateDescriptors func([]ArtifactDescriptor) []ArtifactDescriptor
		want              string
	}{
		{
			name: "missing descriptor",
			updateDescriptors: func(descriptors []ArtifactDescriptor) []ArtifactDescriptor {
				return descriptors[1:]
			},
			want: "artifact manifest must contain exactly 12 artifacts",
		},
		{
			name: "empty sha256",
			updateDescriptors: func(descriptors []ArtifactDescriptor) []ArtifactDescriptor {
				descriptors[0].SHA256 = ""
				return descriptors
			},
			want: "artifact manifest sha256 for privacy_deposit_r1cs.bin must be a 64-character hex string",
		},
		{
			name: "malformed sha256",
			updateDescriptors: func(descriptors []ArtifactDescriptor) []ArtifactDescriptor {
				descriptors[0].SHA256 = "not-a-sha256"
				return descriptors
			},
			want: "artifact manifest sha256 for privacy_deposit_r1cs.bin must be a 64-character hex string",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(ZKArtifactDirEnv, dir)
			t.Setenv(ZKPreflightModeEnv, string(ZKPreflightStrict))
			resetZKSetupStateForTest()

			require.NoError(t, os.WriteFile(filepath.Join(dir, DepositR1CSFile), []byte("present"), 0o600))

			checksums := validPlaceholderChecksums()
			manifest := ManifestFromChecksums(dir, "", checksums)
			manifest.Artifacts = testCase.updateDescriptors(manifest.Artifacts)
			bz, err := json.Marshal(manifest)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, ArtifactManifestFile), bz, 0o600))

			err = RunPreflight(log.NewNopLogger())
			require.Error(t, err)
			require.Contains(t, err.Error(), "privacy zk preflight failed")
			require.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestExpectedChecksumRejectsEnvironmentOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)

	envChecksum := strings.Repeat("a", sha256.Size*2)
	t.Setenv(DepositR1CSSHA256Env, envChecksum)

	checksums := validPlaceholderChecksums()
	checksums[DepositR1CSSHA256Env] = strings.Repeat("b", sha256.Size*2)
	require.NoError(t, writeTestManifest(dir, checksums))

	_, err := expectedChecksum(DepositR1CSFile)
	require.ErrorContains(t, err, "cannot override manifest checksum")
}

type testArtifactFixtureFile struct {
	filename string
	data     []byte
}

type testArtifactFixture struct {
	files     []testArtifactFixtureFile
	checksums map[string]string
}

var (
	testArtifactFixtureOnce   sync.Once
	cachedTestArtifactFixture *testArtifactFixture
	cachedTestArtifactError   error
)

func writeTestArtifacts(dir string) error {
	fixture, err := loadTestArtifactFixture()
	if err != nil {
		return err
	}
	for _, file := range fixture.files {
		if err := os.WriteFile(filepath.Join(dir, file.filename), file.data, 0o666); err != nil {
			return err
		}
	}
	return writeTestManifest(dir, fixture.checksums)
}

func loadTestArtifactFixture() (*testArtifactFixture, error) {
	// Artifact tests need isolated writable files, but not a fresh trusted
	// setup. Cache immutable encodings while recreating files and manifests in
	// every test directory.
	testArtifactFixtureOnce.Do(func() {
		cachedTestArtifactFixture, cachedTestArtifactError = buildTestArtifactFixture()
	})
	return cachedTestArtifactFixture, cachedTestArtifactError
}

func buildTestArtifactFixture() (*testArtifactFixture, error) {
	depositCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.DepositCircuit{})
	if err != nil {
		return nil, err
	}
	depositPK, depositVK, err := groth16.Setup(depositCS)
	if err != nil {
		return nil, err
	}

	spendCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.SpendCircuit{})
	if err != nil {
		return nil, err
	}
	spendPK, spendVK, err := groth16.Setup(spendCS)
	if err != nil {
		return nil, err
	}

	joinSplitCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.JoinSplitCircuit{})
	if err != nil {
		return nil, err
	}
	joinSplitPK, joinSplitVK, err := groth16.Setup(joinSplitCS)
	if err != nil {
		return nil, err
	}

	artifacts := []struct {
		filenames []string
		object    interface {
			WriteTo(io.Writer) (int64, error)
		}
	}{
		{[]string{DepositR1CSFile}, depositCS},
		{[]string{DepositPKFile}, depositPK},
		{[]string{DepositVKFile}, depositVK},
		{[]string{SpendR1CSFile}, spendCS},
		{[]string{SpendPKFile}, spendPK},
		{[]string{SpendVKFile}, spendVK},
		// Registry tests exercise lifecycle semantics rather than the batch
		// constraint set. Reusing the valid JoinSplit encoding here avoids a
		// 1.1M-constraint setup for every test while preserving R1CS/PK/VK
		// decoding, checksumming, role selection, and lazy loading behavior.
		{[]string{JoinSplitR1CSFile, BatchJoinSplit16x32R1CSFile}, joinSplitCS},
		{[]string{JoinSplitPKFile, BatchJoinSplit16x32PKFile}, joinSplitPK},
		{[]string{JoinSplitVKFile, BatchJoinSplit16x32VKFile}, joinSplitVK},
	}

	encodedByFilename := make(map[string][]byte, len(DefaultArtifactDescriptors()))
	for _, artifact := range artifacts {
		var encoded bytes.Buffer
		if _, err := artifact.object.WriteTo(&encoded); err != nil {
			return nil, err
		}
		data := append([]byte(nil), encoded.Bytes()...)
		for _, filename := range artifact.filenames {
			encodedByFilename[filename] = data
		}
	}

	descriptors := DefaultArtifactDescriptors()
	fixture := &testArtifactFixture{
		files:     make([]testArtifactFixtureFile, 0, len(descriptors)),
		checksums: make(map[string]string, len(descriptors)),
	}
	for _, descriptor := range descriptors {
		data, ok := encodedByFilename[descriptor.Filename]
		if !ok {
			return nil, fmt.Errorf("missing cached test artifact %s", descriptor.Filename)
		}
		fixture.files = append(fixture.files, testArtifactFixtureFile{
			filename: descriptor.Filename,
			data:     data,
		})
		sum := sha256.Sum256(data)
		fixture.checksums[descriptor.ChecksumEnv] = hex.EncodeToString(sum[:])
	}
	return fixture, nil
}

func validPlaceholderChecksums() map[string]string {
	checksums := make(map[string]string)
	for _, descriptor := range DefaultArtifactDescriptors() {
		checksums[descriptor.ChecksumEnv] = strings.Repeat("a", sha256.Size*2)
	}
	return checksums
}

func writeTestManifest(dir string, checksums map[string]string) error {
	manifest := ManifestFromChecksums(dir, "", checksums)
	bz, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ArtifactManifestFile), bz, 0o600)
}
