package withdraw

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
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestAuditV2FromNormalSpendUsesFinalOwnerIntent(t *testing.T) {
	spend, view := testPubKey(11), testPubKey(13)
	note := testSecretNoteFixture(privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(spend, true), ReceiverSpendPubKeyY: pointCoordinate(spend, false),
		ReceiverViewPubKeyX: pointCoordinate(view, true), ReceiverViewPubKeyY: pointCoordinate(view, false),
		Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair"), Randomness: big.NewInt(701),
	})
	commitment, err := note.CommitmentV1()
	require.NoError(t, err)
	commitmentRaw := commitment.Bytes()
	commitmentHex := fmt.Sprintf("%x", commitmentRaw)
	root, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)
	creator := sdk.AccAddress(bytes.Repeat([]byte{4}, 20)).String()
	recipientAddress := sdk.AccAddress(bytes.Repeat([]byte{5}, 20))
	recipient := recipientAddress.String()
	legacy, err := PrepareSpendWithdraw(context.Background(), &stubMerklePathProvider{paths: map[string]*MerklePathResult{commitmentHex: {Root: root, Path: []string{"01", "02"}, PathHelper: []uint32{0, 1}}}}, &stubSpendNoteHashSigner{signature: testSignatureBytes()}, PrepareSpendWithdrawInput{
		Note: privacyscan.SecretFoundNote{Note: note}, RecipientBytes: recipientAddress, ChainID: "clairveil-test-1", ExpiresAtUnix: 2_000_000_000,
	})
	require.NoError(t, err)
	prepared, err := PrepareAuditV2FromSpend(withdrawAuditSnapshot(t), legacy, creator, recipient, "7uclair")
	require.NoError(t, err)
	defer prepared.Clear()

	full, err := BuildAuditV2Witness(prepared, legacy, func(intent *big.Int) ([]byte, error) { return signAuditOwnerIntent(t, 11, intent) })
	require.NoError(t, err)
	public, err := full.Public()
	require.NoError(t, err)
	require.Len(t, public.Vector().(fr.Vector), 23)
}

func TestAuditV2WithdrawWitnessSatisfiesP3R1CS(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to the verified P3 development bundle")
	}
	spend, view := testPubKey(11), testPubKey(13)
	note := testSecretNoteFixture(privacytypes.Note{ReceiverSpendPubKeyX: pointCoordinate(spend, true), ReceiverSpendPubKeyY: pointCoordinate(spend, false), ReceiverViewPubKeyX: pointCoordinate(view, true), ReceiverViewPubKeyY: pointCoordinate(view, false), Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair"), Randomness: big.NewInt(701)})
	commitment, err := note.CommitmentV1()
	require.NoError(t, err)
	commitmentRaw := commitment.Bytes()
	root, path, pathHelper := auditWithdrawMerklePath(t, new(big.Int).SetBytes(commitmentRaw[:]))
	creator := sdk.AccAddress(bytes.Repeat([]byte{4}, 20)).String()
	recipientAddress := sdk.AccAddress(bytes.Repeat([]byte{5}, 20))
	legacy, err := PrepareSpendWithdraw(context.Background(), &stubMerklePathProvider{paths: map[string]*MerklePathResult{fmt.Sprintf("%x", commitmentRaw): {Root: root, Path: path, PathHelper: pathHelper}}}, &stubSpendNoteHashSigner{signature: testSignatureBytes()}, PrepareSpendWithdrawInput{Note: privacyscan.SecretFoundNote{Note: note}, RecipientBytes: recipientAddress, ChainID: "clairveil-test-1", ExpiresAtUnix: 2_000_000_000})
	require.NoError(t, err)
	prepared, err := PrepareAuditV2FromSpend(withdrawAuditSnapshot(t), legacy, creator, recipientAddress.String(), "7uclair")
	require.NoError(t, err)
	defer prepared.Clear()
	full, err := BuildAuditV2Witness(prepared, legacy, func(intent *big.Int) ([]byte, error) { return signAuditOwnerIntent(t, 11, intent) })
	require.NoError(t, err)
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: dir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	require.NoError(t, err)
	r1cs, err := registry.R1CS(privacyzk.CircuitSpendAuditField)
	require.NoError(t, err)
	require.NoError(t, r1cs.IsSolved(full))
}

func withdrawAuditSnapshot(t *testing.T) privacyaudit.Snapshot {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = 103
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(raw)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: [32]byte{2}}
}

func signAuditOwnerIntent(t *testing.T, scalar byte, intent *big.Int) ([]byte, error) {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = scalar
	secret, err := privacycrypto.ImportNonzeroScalarBE32(raw)
	if err != nil {
		return nil, err
	}
	var message [32]byte
	intent.FillBytes(message[:])
	return privacycrypto.SignOwnerIntent(message, secret, bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
}

func auditWithdrawMerklePath(t *testing.T, commitment *big.Int) ([]byte, []string, []uint32) {
	t.Helper()
	siblings := privacytypes.EmptyNoteTreeRootsV1(32)[:32]
	path, helper := make([]string, len(siblings)), make([]uint32, len(siblings))
	current := new(big.Int).Set(commitment)
	for index, sibling := range siblings {
		path[index] = fmt.Sprintf("%x", sibling.FillBytes(make([]byte, 32)))
		current = privacytypes.ComputeNoteTreeNodeV1(uint32(index), current, sibling)
	}
	root, err := privacyfield.CanonicalBytesFromBigInt(current)
	require.NoError(t, err)
	return root, path, helper
}
