package zk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestValidateLocalVerifierArtifactsLoadsOnlyVerifyingKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()
	require.NoError(t, writeTestArtifacts(dir))
	for _, descriptor := range DefaultArtifactDescriptors() {
		if descriptor.ArtifactType != "verifying_key" {
			require.NoError(t, os.Remove(filepath.Join(dir, descriptor.Filename)))
		}
	}
	require.NoError(t, ValidateLocalVerifierArtifacts())
}

func TestValidateLocalVerifierIdentityRejectsConsensusMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()
	require.NoError(t, writeTestArtifacts(dir))
	identity, err := LoadLocalCircuitSetIdentity()
	require.NoError(t, err)
	identity.Circuits[1].VerifyingKeySha256 = strings.Repeat("f", 64)

	err = ValidateLocalVerifierIdentity(identity)
	require.ErrorContains(t, err, "does not match consensus")
}

func TestValidateLocalVerifierIdentityRejectsEnvironmentOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ZKArtifactDirEnv, dir)
	resetZKSetupStateForTest()
	require.NoError(t, writeTestArtifacts(dir))
	identity, err := LoadLocalCircuitSetIdentity()
	require.NoError(t, err)
	t.Setenv(SpendVKSHA256Env, strings.Repeat("f", 64))

	err = ValidateLocalVerifierIdentity(identity)
	require.ErrorContains(t, err, "cannot override consensus checksum")
}

func TestCircuitSetIdentityRequiresCanonicalCircuitOrder(t *testing.T) {
	checksums := validPlaceholderChecksums()
	identity, err := CircuitSetIdentityFromChecksums(checksums)
	require.NoError(t, err)
	require.NoError(t, privacytypes.ValidateCircuitSetIdentity(identity))
	identity.Circuits[0], identity.Circuits[1] = identity.Circuits[1], identity.Circuits[0]
	require.ErrorContains(t, privacytypes.ValidateCircuitSetIdentity(identity), "circuit_id")
}
