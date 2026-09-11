package transfer

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func TestAuditV2FromPreparedPayloadReusesNormalTransferWitness(t *testing.T) {
	input, paths, signer, _, _ := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), paths, signer, input)
	require.NoError(t, err)

	prepared, err := PrepareAuditV2FromPreparedPayload(transferAuditSnapshot(t), payload)
	require.NoError(t, err)
	defer prepared.Clear()

	full, err := BuildAuditV2WitnessFromPreparedPayload(prepared, payload, func(_ *big.Int) ([]byte, error) {
		return testSignatureBytes(t), nil
	})
	require.NoError(t, err)
	public, err := full.Public()
	require.NoError(t, err)
	require.Len(t, public.Vector().(fr.Vector), 23)
}

// Output 1 is a normal 2x2 output, not a change-only slot. The audit-v2
// preparation path therefore preserves the protocol contract that either
// output may belong to an independently selected recipient.
func TestPrepareAuditV2TransferAllowsThirdPartySecondOutput(t *testing.T) {
	input, paths, _, _, _ := testBuildTransferMessageDeps(t)
	thirdSpendScalar, thirdSpend := testScalarAndPubKey(211)
	thirdViewScalar, thirdView := testScalarAndPubKey(223)
	_ = thirdSpendScalar
	_ = thirdViewScalar
	normalInput := PrepareJoinSplitInput{
		Inputs: input.Inputs, RecipientSpendPubKey: input.RecipientSpendPubKey, RecipientViewPubKey: input.RecipientViewPubKey,
		TransferAmount: input.TransferAmount, SenderSpendPubKey: thirdSpend, SenderViewPubKey: thirdView,
	}

	prepared, _, err := PrepareAuditV2Transfer(
		context.Background(), paths, transferAuditSnapshot(t), "clair1auditv2thirdparty", 1800,
		normalInput, "uclair", AuditV2DisclosureConfig{DisableSelfViewDisclosure: true},
		func(intent *big.Int) ([]byte, error) { return signAuditTransferIntent(t, intent) },
	)
	require.NoError(t, err)
	defer prepared.Clear()
	require.Len(t, prepared.OutputEffects(), 2)
}

func TestAuditV2TransferWitnessSatisfiesP3R1CS(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to the verified P3 development bundle")
	}
	input, paths, signer, _, _ := testBuildTransferMessageDeps(t)
	paths.paths = auditTransferMerklePaths(t, input.Inputs)
	payload, err := BuildPreparedTransferPayload(context.Background(), paths, signer, input)
	require.NoError(t, err)
	prepared, err := PrepareAuditV2FromPreparedPayload(transferAuditSnapshot(t), payload)
	require.NoError(t, err)
	defer prepared.Clear()
	full, err := BuildAuditV2WitnessFromPreparedPayload(prepared, payload, func(intent *big.Int) ([]byte, error) { return signAuditTransferIntent(t, intent) })
	require.NoError(t, err)
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: dir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	require.NoError(t, err)
	r1cs, err := registry.R1CS(privacyzk.CircuitJoinSplitAuditField)
	require.NoError(t, err)
	require.NoError(t, r1cs.IsSolved(full))
}

func transferAuditSnapshot(t *testing.T) privacyaudit.Snapshot {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = 97
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(raw)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: [32]byte{2}}
}

func signAuditTransferIntent(t *testing.T, intent *big.Int) ([]byte, error) {
	t.Helper()
	secret := testSecretScalar(t, big.NewInt(61))
	var message [32]byte
	intent.FillBytes(message[:])
	return privacycrypto.SignOwnerIntent(message, secret, bytes.NewReader(bytes.Repeat([]byte{2}, 64)))
}

// auditTransferMerklePaths supplies two distinct leaves with one shared,
// depth-32 root. The ordinary fixture's abbreviated path is adequate for
// host-boundary tests but cannot satisfy the actual R1CS relation.
func auditTransferMerklePaths(t *testing.T, inputs [2]privacyscan.SecretFoundNote) map[string]*MerklePathResult {
	t.Helper()
	commitments := [2]*big.Int{}
	for i := range inputs {
		commitment, err := inputs[i].Note.CommitmentV1()
		require.NoError(t, err)
		commitmentRaw := commitment.Bytes()
		commitments[i] = new(big.Int).SetBytes(commitmentRaw[:])
	}
	empty := privacytypes.EmptyNoteTreeRootsV1(32)[:32]
	paths := make(map[string]*MerklePathResult, len(inputs))
	var expectedRoot []byte
	for i := range inputs {
		path := make([]string, len(empty))
		helpers := make([]uint32, len(empty))
		path[0] = fmt.Sprintf("%x", commitments[1-i].FillBytes(make([]byte, 32)))
		helpers[0] = uint32(i)
		current := new(big.Int).Set(commitments[i])
		if i == 0 {
			current = privacytypes.ComputeNoteTreeNodeV1(0, current, commitments[1])
		} else {
			current = privacytypes.ComputeNoteTreeNodeV1(0, commitments[0], current)
		}
		for level := 1; level < len(empty); level++ {
			path[level] = fmt.Sprintf("%x", empty[level].FillBytes(make([]byte, 32)))
			current = privacytypes.ComputeNoteTreeNodeV1(uint32(level), current, empty[level])
		}
		root := current.FillBytes(make([]byte, 32))
		if expectedRoot == nil {
			expectedRoot = root
		} else {
			require.Equal(t, expectedRoot, root)
		}
		commitmentRaw := commitments[i].FillBytes(make([]byte, 32))
		paths[fmt.Sprintf("%x", commitmentRaw)] = &MerklePathResult{Root: root, Path: path, PathHelper: helpers}
	}
	return paths
}
