package privacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func setupPrivacyGenesisKeeper() (*keeper.Keeper, sdk.Context) {
	storeKey := storetypes.NewKVStoreKey(privacytypes.StoreKey)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	k := keeper.NewKeeper(privacytypes.ModuleCdc, runtime.NewKVStoreService(storeKey), paramtypes.Subspace{}, nil)
	return k, ctx
}

func fixedFieldBytesFromUint64(v uint64) []byte {
	bz := make([]byte, 32)
	binary.BigEndian.PutUint64(bz[24:], v)
	return bz
}

func TestGenesisRoundTrip(t *testing.T) {
	k, ctx := setupPrivacyGenesisKeeper()
	identity := setupGenesisCircuitIdentity(t)
	require.NoError(t, k.SetCircuitSetIdentity(ctx, identity))
	_, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)

	commitments := [][]byte{
		fixedFieldBytesFromUint64(1),
		fixedFieldBytesFromUint64(2),
		fixedFieldBytesFromUint64(3),
	}

	for _, commitment := range commitments {
		require.NoError(t, k.AppendCommitment(ctx, commitment))
	}

	nullifiers := [][]byte{
		fixedFieldBytesFromUint64(11),
		fixedFieldBytesFromUint64(12),
	}
	for _, nullifier := range nullifiers {
		k.SetNullifier(ctx, nullifier)
	}
	require.NoError(t, k.RecordReserveDeposit(ctx, sdk.NewInt64Coin("uclair", 25)))
	require.NoError(t, k.RecordReserveWithdraw(ctx, sdk.NewInt64Coin("uclair", 4)))
	_, err = k.AllocatePrivacyGlobalSequence(ctx)
	require.NoError(t, err)

	exported := ExportGenesis(ctx, *k)
	require.NotNil(t, exported)
	require.NoError(t, exported.Validate())
	require.Equal(t, commitments, exported.Commitments)
	require.ElementsMatch(t, nullifiers, exported.Nullifiers)
	require.NotEmpty(t, exported.HistoricalRoots)
	require.Len(t, exported.AssetRegistry, 1)
	require.Equal(t, uint64(1), exported.PrivacyGlobalSequence)
	require.Len(t, exported.MerkleRootSnapshots, len(commitments))
	require.Equal(t, "25", exported.ReserveBalances[0].TotalDeposited)
	require.Equal(t, "4", exported.ReserveBalances[0].TotalWithdrawn)

	restoredKeeper, restoredCtx := setupPrivacyGenesisKeeper()
	InitGenesis(restoredCtx, *restoredKeeper, *exported)

	restoredExport := ExportGenesis(restoredCtx, *restoredKeeper)
	require.Equal(t, exported, restoredExport)
	require.Equal(t, uint64(len(exported.Commitments)), restoredKeeper.GetLeafCount(restoredCtx))

	for _, commitment := range exported.Commitments {
		_, found, err := restoredKeeper.GetCommitmentIndex(restoredCtx, commitment)
		require.NoError(t, err)
		require.True(t, found)
	}

	for _, nullifier := range exported.Nullifiers {
		require.True(t, restoredKeeper.HasNullifier(restoredCtx, nullifier))
	}

	for _, root := range exported.HistoricalRoots {
		require.True(t, restoredKeeper.CheckHistoricalRoot(restoredCtx, root))
	}
}

func TestInitGenesisPanicsWithInvalidState(t *testing.T) {
	k, ctx := setupPrivacyGenesisKeeper()

	state := privacytypes.GenesisState{
		Commitments: [][]byte{{0x01}},
	}

	require.Panics(t, func() {
		InitGenesis(ctx, *k, state)
	})
}

func TestInitGenesisPanicsWithForgedHistoricalRoot(t *testing.T) {
	k, ctx := setupPrivacyGenesisKeeper()
	identity := setupGenesisCircuitIdentity(t)

	state := *privacytypes.DefaultGenesis(identity)
	state.Commitments = [][]byte{fixedFieldBytesFromUint64(1)}
	state.HistoricalRoots = [][]byte{fixedFieldBytesFromUint64(99)}
	state.MerkleRootSnapshots = []*privacytypes.MerkleRootSnapshotV1{{
		Root: fixedFieldBytesFromUint64(99), LeafCount: 1, Height: 0,
	}}

	require.PanicsWithError(t, "failed to initialize privacy historical roots: genesis historical root at index 0 does not match any commitment prefix root", func() {
		InitGenesis(ctx, *k, state)
	})
}

func TestInitGenesisRejectsCircuitIdentityMismatchBeforeStateWrites(t *testing.T) {
	k, ctx := setupPrivacyGenesisKeeper()
	identity := setupGenesisCircuitIdentity(t)
	identity.Circuits[0].VerifyingKeySha256 = strings.Repeat("f", 64)
	state := *privacytypes.DefaultGenesis(identity)
	state.Commitments = [][]byte{fixedFieldBytesFromUint64(1)}
	state.MerkleRootSnapshots = []*privacytypes.MerkleRootSnapshotV1{{
		Root: fixedFieldBytesFromUint64(1), LeafCount: 1, Height: 0,
	}}

	require.Panics(t, func() { InitGenesis(ctx, *k, state) })
	require.Equal(t, uint64(0), k.GetLeafCount(ctx))
	_, found, err := k.GetCircuitSetIdentity(ctx)
	require.NoError(t, err)
	require.False(t, found)
}

func setupGenesisCircuitIdentity(t *testing.T) *privacytypes.CircuitSetIdentity {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(privacyzk.ZKArtifactDirEnv, dir)
	cs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.DepositCircuit{})
	require.NoError(t, err)
	_, vk, err := groth16.Setup(cs)
	require.NoError(t, err)
	var encoded bytes.Buffer
	_, err = vk.WriteTo(&encoded)
	require.NoError(t, err)
	vkBytes := encoded.Bytes()
	vkSum := sha256.Sum256(vkBytes)
	vkChecksum := hex.EncodeToString(vkSum[:])

	checksums := make(map[string]string)
	for _, descriptor := range privacyzk.DefaultArtifactDescriptors() {
		checksums[descriptor.ChecksumEnv] = strings.Repeat("0", 64)
	}
	for _, circuitID := range privacytypes.RequiredCircuitIdentityOrder {
		var filename, checksumEnv string
		switch circuitID {
		case "deposit":
			filename, checksumEnv = privacyzk.DepositVKFile, privacyzk.DepositVKSHA256Env
		case "spend":
			filename, checksumEnv = privacyzk.SpendVKFile, privacyzk.SpendVKSHA256Env
		case "joinsplit":
			filename, checksumEnv = privacyzk.JoinSplitVKFile, privacyzk.JoinSplitVKSHA256Env
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, filename), vkBytes, 0o600))
		checksums[checksumEnv] = vkChecksum
	}
	manifest := privacyzk.ManifestFromChecksums(dir, "", checksums)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, privacyzk.ArtifactManifestFile), manifestBytes, 0o600))
	return privacytypes.CloneCircuitSetIdentity(manifest.CircuitSetIdentity)
}
