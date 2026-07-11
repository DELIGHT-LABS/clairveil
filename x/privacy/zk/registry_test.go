package zk

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestArtifactRegistryLoadsOnlyRequestedCircuitRole(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestArtifacts(dir))
	for _, filename := range []string{
		DepositR1CSFile, DepositPKFile, DepositVKFile,
		SpendVKFile, JoinSplitR1CSFile, JoinSplitPKFile,
	} {
		require.NoError(t, os.Remove(filepath.Join(dir, filename)))
	}

	var mu sync.Mutex
	reads := make(map[string]int)
	registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir: dir,
		ReadFile: func(path string) ([]byte, error) {
			mu.Lock()
			reads[filepath.Base(path)]++
			mu.Unlock()
			return os.ReadFile(path)
		},
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	require.NoError(t, err)
	identity, err := registry.LocalCircuitSetIdentity()
	require.NoError(t, err)
	require.NoError(t, registry.CheckReadiness(ArtifactRoleValidator, []CircuitID{CircuitJoinSplit}, identity))
	require.NoError(t, registry.CheckReadiness(ArtifactRoleProver, []CircuitID{CircuitSpend}, nil))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, reads[ArtifactManifestFile])
	require.Equal(t, 1, reads[JoinSplitVKFile])
	require.Equal(t, 1, reads[SpendR1CSFile])
	require.Equal(t, 1, reads[SpendPKFile])
	for _, filename := range []string{
		DepositR1CSFile, DepositPKFile, DepositVKFile,
		SpendVKFile, JoinSplitR1CSFile, JoinSplitPKFile,
	} {
		require.Zero(t, reads[filename], filename)
	}
}

func TestBatchArtifactRegistryRoleReadinessLoadsOnlyRequiredArtifacts(t *testing.T) {
	t.Run("validator loads VK only", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeTestArtifacts(dir))
		require.NoError(t, os.Remove(filepath.Join(dir, BatchJoinSplit16x32R1CSFile)))
		require.NoError(t, os.Remove(filepath.Join(dir, BatchJoinSplit16x32PKFile)))

		var mu sync.Mutex
		reads := make(map[string]int)
		registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
			ArtifactDir: dir,
			ReadFile: func(path string) ([]byte, error) {
				mu.Lock()
				reads[filepath.Base(path)]++
				mu.Unlock()
				return os.ReadFile(path)
			},
			LookupEnv: func(string) (string, bool) { return "", false },
		})
		require.NoError(t, err)
		identity, err := registry.LocalCircuitSetIdentity()
		require.NoError(t, err)
		require.NoError(t, registry.CheckReadiness(ArtifactRoleValidator, []CircuitID{CircuitBatchJoinSplit16x32V1}, identity))

		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, 1, reads[ArtifactManifestFile])
		require.Equal(t, 1, reads[BatchJoinSplit16x32VKFile])
		require.Zero(t, reads[BatchJoinSplit16x32R1CSFile])
		require.Zero(t, reads[BatchJoinSplit16x32PKFile])
	})

	t.Run("prover loads R1CS and PK only", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeTestArtifacts(dir))
		require.NoError(t, os.Remove(filepath.Join(dir, BatchJoinSplit16x32VKFile)))

		var mu sync.Mutex
		reads := make(map[string]int)
		registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
			ArtifactDir: dir,
			ReadFile: func(path string) ([]byte, error) {
				mu.Lock()
				reads[filepath.Base(path)]++
				mu.Unlock()
				return os.ReadFile(path)
			},
			LookupEnv: func(string) (string, bool) { return "", false },
		})
		require.NoError(t, err)
		require.NoError(t, registry.CheckReadiness(ArtifactRoleProver, []CircuitID{CircuitBatchJoinSplit16x32V1}, nil))

		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, 1, reads[ArtifactManifestFile])
		require.Equal(t, 1, reads[BatchJoinSplit16x32R1CSFile])
		require.Equal(t, 1, reads[BatchJoinSplit16x32PKFile])
		require.Zero(t, reads[BatchJoinSplit16x32VKFile])
	})
}

func TestBatchArtifactRegistryReadinessRejectsMissingAndMismatchedArtifacts(t *testing.T) {
	tests := []struct {
		name     string
		role     ArtifactRole
		filename string
	}{
		{name: "validator missing VK", role: ArtifactRoleValidator, filename: BatchJoinSplit16x32VKFile},
		{name: "prover missing R1CS", role: ArtifactRoleProver, filename: BatchJoinSplit16x32R1CSFile},
		{name: "prover missing PK", role: ArtifactRoleProver, filename: BatchJoinSplit16x32PKFile},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, writeTestArtifacts(dir))
			require.NoError(t, os.Remove(filepath.Join(dir, tc.filename)))
			registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
				ArtifactDir: dir,
				LookupEnv:   func(string) (string, bool) { return "", false },
			})
			require.NoError(t, err)
			var identity *privacytypes.CircuitSetIdentity
			if tc.role == ArtifactRoleValidator {
				identity, err = registry.LocalCircuitSetIdentity()
				require.NoError(t, err)
			}
			err = registry.CheckReadiness(tc.role, []CircuitID{CircuitBatchJoinSplit16x32V1}, identity)
			require.ErrorContains(t, err, tc.filename)
		})
	}

	for _, tc := range []struct {
		name     string
		role     ArtifactRole
		filename string
	}{
		{name: "validator mismatched VK", role: ArtifactRoleValidator, filename: BatchJoinSplit16x32VKFile},
		{name: "prover mismatched R1CS", role: ArtifactRoleProver, filename: BatchJoinSplit16x32R1CSFile},
		{name: "prover mismatched PK", role: ArtifactRoleProver, filename: BatchJoinSplit16x32PKFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, writeTestArtifacts(dir))
			require.NoError(t, os.WriteFile(filepath.Join(dir, tc.filename), []byte("mismatch"), 0o600))
			registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
				ArtifactDir: dir,
				LookupEnv:   func(string) (string, bool) { return "", false },
			})
			require.NoError(t, err)
			var identity *privacytypes.CircuitSetIdentity
			if tc.role == ArtifactRoleValidator {
				identity, err = registry.LocalCircuitSetIdentity()
				require.NoError(t, err)
			}
			err = registry.CheckReadiness(tc.role, []CircuitID{CircuitBatchJoinSplit16x32V1}, identity)
			require.ErrorContains(t, err, "checksum mismatch")
			require.ErrorContains(t, err, tc.filename)
		})
	}
}

func TestArtifactRegistryConcurrentLazyLoadReadsArtifactOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestArtifacts(dir))
	var artifactReads atomic.Int64
	registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir: dir,
		ReadFile: func(path string) ([]byte, error) {
			if filepath.Base(path) == SpendR1CSFile {
				artifactReads.Add(1)
			}
			return os.ReadFile(path)
		},
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	require.NoError(t, err)

	const callers = 32
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, loadErr := registry.R1CS(CircuitSpend)
			errs <- loadErr
		}()
	}
	wg.Wait()
	close(errs)
	for loadErr := range errs {
		require.NoError(t, loadErr)
	}
	require.Equal(t, int64(1), artifactReads.Load())
}

func TestArtifactRegistriesAreIsolated(t *testing.T) {
	validDir := t.TempDir()
	require.NoError(t, writeTestArtifacts(validDir))
	valid, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir: validDir,
		LookupEnv:   func(string) (string, bool) { return "", false },
	})
	require.NoError(t, err)
	invalid, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir: t.TempDir(),
		LookupEnv:   func(string) (string, bool) { return "", false },
	})
	require.NoError(t, err)

	_, err = invalid.ProvingKey(CircuitSpend)
	require.ErrorContains(t, err, ArtifactManifestFile)
	_, err = valid.ProvingKey(CircuitSpend)
	require.NoError(t, err)
}

func TestArtifactRegistryDevelopmentOverrideIsExplicitAndNeverAppliesToValidator(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestArtifacts(dir))
	depositPK, err := os.ReadFile(filepath.Join(dir, DepositPKFile))
	require.NoError(t, err)
	depositPKSum := sha256.Sum256(depositPK)
	depositPKChecksum := hex.EncodeToString(depositPKSum[:])
	depositVK, err := os.ReadFile(filepath.Join(dir, DepositVKFile))
	require.NoError(t, err)
	depositVKSum := sha256.Sum256(depositVK)
	depositVKChecksum := hex.EncodeToString(depositVKSum[:])

	lookup := func(name string) (string, bool) {
		if name == SpendPKSHA256Env {
			return depositPKChecksum, true
		}
		return "", false
	}
	readWithDevelopmentPK := func(path string) ([]byte, error) {
		if filepath.Base(path) == SpendPKFile {
			return append([]byte(nil), depositPK...), nil
		}
		return os.ReadFile(path)
	}

	_, err = NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir:              dir,
		RuntimeEnvironment:       ZKRuntimeEnvironmentProduction,
		AllowDevelopmentOverride: true,
	})
	require.ErrorContains(t, err, "development-only")

	strict, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir: dir,
		ReadFile:    readWithDevelopmentPK,
		LookupEnv:   lookup,
	})
	require.NoError(t, err)
	_, err = strict.ProvingKey(CircuitSpend)
	require.ErrorContains(t, err, "cannot override manifest checksum")

	development, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir:              dir,
		ReadFile:                 readWithDevelopmentPK,
		LookupEnv:                lookup,
		RuntimeEnvironment:       ZKRuntimeEnvironmentDevelopment,
		AllowDevelopmentOverride: true,
	})
	require.NoError(t, err)
	_, err = development.ProvingKey(CircuitSpend)
	require.NoError(t, err)

	identity, err := development.LocalCircuitSetIdentity()
	require.NoError(t, err)
	// The same development registry cannot use an environment checksum to
	// alter validator consensus identity.
	validatorLookup, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir: dir,
		ReadFile: func(path string) ([]byte, error) {
			if filepath.Base(path) == SpendVKFile {
				return append([]byte(nil), depositVK...), nil
			}
			return os.ReadFile(path)
		},
		LookupEnv: func(name string) (string, bool) {
			if name == SpendVKSHA256Env {
				return depositVKChecksum, true
			}
			return "", false
		},
		RuntimeEnvironment:       ZKRuntimeEnvironmentDevelopment,
		AllowDevelopmentOverride: true,
	})
	require.NoError(t, err)
	require.ErrorContains(t, validatorLookup.CheckReadiness(ArtifactRoleValidator, []CircuitID{CircuitSpend}, identity), "cannot override consensus checksum")
}
