package batchtransfer

import (
	"bytes"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
	"github.com/stretchr/testify/require"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func TestAuditV2FromPreparedPayloadReusesNormalBatchWitness(t *testing.T) {
	payload := testPayload(t)
	payload.Creator = "clair1batchaudit"
	prepared, err := PrepareAuditV2FromPreparedPayload(batchAuditSnapshot(t), payload)
	require.NoError(t, err)
	defer prepared.Clear()

	full, err := BuildAuditV2Witness(prepared, payload, func(_ *big.Int) ([]byte, error) {
		return append([]byte(nil), payload.OwnerSignature...), nil
	})
	require.NoError(t, err)
	public, err := full.Public()
	require.NoError(t, err)
	require.Len(t, public.Vector().(fr.Vector), 23)
}

func TestAuditV2WitnessFromV2PayloadDoesNotRequireLegacyStoredSignature(t *testing.T) {
	legacy := testPayload(t)
	payload, err := BuildPreparedAuditV2BatchTransferPayload(&PreparedBatchTransfer{
		Root: legacy.Root, AssetID: legacy.AssetID, Inputs: legacy.Inputs, Outputs: legacy.Outputs,
	}, BuildPreparedAuditV2BatchTransferPayloadInput{
		Creator: "clair1batchaudit", ChainID: legacy.ChainID, ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), DisableSelfViewDisclosure: true,
	})
	require.NoError(t, err)
	prepared, err := PrepareAuditV2FromPreparedAuditV2Payload(batchAuditSnapshot(t), payload)
	require.NoError(t, err)
	defer prepared.Clear()

	full, err := BuildAuditV2WitnessFromPayload(prepared, payload, func(_ *big.Int) ([]byte, error) {
		return append([]byte(nil), legacy.OwnerSignature...), nil
	})
	require.NoError(t, err)
	public, err := full.Public()
	require.NoError(t, err)
	require.Len(t, public.Vector().(fr.Vector), 23)
}

func TestAuditV2BatchWitnessSatisfiesP3R1CS(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to the verified P3 development bundle")
	}
	payload := testPayload(t)
	payload.Creator = "clair1batchaudit"
	prepared, err := PrepareAuditV2FromPreparedPayload(batchAuditSnapshot(t), payload)
	require.NoError(t, err)
	defer prepared.Clear()
	owner, err := cryptoeddsa.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	require.NoError(t, err)
	full, err := BuildAuditV2Witness(prepared, payload, func(intent *big.Int) ([]byte, error) {
		return owner.Sign(intent.FillBytes(make([]byte, 32)), mimc.NewMiMC())
	})
	require.NoError(t, err)
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: dir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	require.NoError(t, err)
	r1cs, err := registry.R1CS(privacyzk.CircuitBatchJoinSplitAuditField)
	require.NoError(t, err)
	require.NoError(t, r1cs.IsSolved(full))
}

func batchAuditSnapshot(t *testing.T) privacyaudit.Snapshot {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = 101
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(raw)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: [32]byte{2}}
}
