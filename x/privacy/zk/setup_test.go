package zk

import (
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
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
	once = sync.Once{}
	setupErr = nil
	spendProvingKey = nil
	spendVerifyingKey = nil
	spendR1CS = nil
	joinSplitProvingKey = nil
	joinSplitVerifyingKey = nil
	joinSplitR1CS = nil
}

func TestArtifactPathUsesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)

	require.Equal(t, filepath.Join(dir, SpendR1CSFile), artifactPath(SpendR1CSFile))
	require.Equal(t, filepath.Join(dir, JoinSplitVKFile), artifactPath(JoinSplitVKFile))
}

func TestValidateZKSetupFailsOnMissingArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()

	err := ValidateZKSetup()
	require.Error(t, err)
	require.Contains(t, err.Error(), SpendR1CSFile)

	_, err = GetSpendR1CS()
	require.Error(t, err)
	require.Contains(t, err.Error(), SpendR1CSFile)

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 0)
}

func TestValidateZKSetupFailsOnChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()

	data := []byte("not-a-valid-artifact")
	require.NoError(t, os.WriteFile(filepath.Join(dir, SpendR1CSFile), data, 0600))

	bad := make([]byte, 32)
	bad[0] = 0x01
	t.Setenv(SpendR1CSSHA256Env, hex.EncodeToString(bad))

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

func writeTestArtifacts(dir string) error {
	spendCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.SpendCircuit{})
	if err != nil {
		return err
	}
	spendPK, spendVK, err := groth16.Setup(spendCS)
	if err != nil {
		return err
	}

	joinSplitCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.JoinSplitCircuit{})
	if err != nil {
		return err
	}
	joinSplitPK, joinSplitVK, err := groth16.Setup(joinSplitCS)
	if err != nil {
		return err
	}

	artifacts := []struct {
		filename string
		object   interface {
			WriteTo(io.Writer) (int64, error)
		}
	}{
		{SpendR1CSFile, spendCS},
		{SpendPKFile, spendPK},
		{SpendVKFile, spendVK},
		{JoinSplitR1CSFile, joinSplitCS},
		{JoinSplitPKFile, joinSplitPK},
		{JoinSplitVKFile, joinSplitVK},
	}

	for _, artifact := range artifacts {
		file, err := os.Create(filepath.Join(dir, artifact.filename))
		if err != nil {
			return err
		}

		if _, err := artifact.object.WriteTo(file); err != nil {
			file.Close()
			return err
		}

		if err := file.Close(); err != nil {
			return err
		}
	}

	return nil
}
