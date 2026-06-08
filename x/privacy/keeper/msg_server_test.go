package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const msgServerTestChainID = "clairveil-local-1"
const msgServerTestExpiry int64 = 4102444800

type mockPrivacyBankKeeper struct {
	fromAccountToModuleCalls int
	fromModuleToAccountCalls int
	errFromAccountToModule   error
	errFromModuleToAccount   error
	moduleBalances           sdk.Coins
}

var (
	depositArtifactOnce sync.Once
	depositArtifactErr  error
	depositTestR1CS     constraint.ConstraintSystem
	depositTestPK       groth16.ProvingKey
)

func (m *mockPrivacyBankKeeper) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.moduleBalances.AmountOf(denom))
}

func (m *mockPrivacyBankKeeper) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, amt sdk.Coins) error {
	m.fromAccountToModuleCalls++
	if m.errFromAccountToModule != nil {
		return m.errFromAccountToModule
	}
	m.moduleBalances = m.moduleBalances.Add(amt...)
	return nil
}

func (m *mockPrivacyBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
	m.fromModuleToAccountCalls++
	if m.errFromModuleToAccount != nil {
		return m.errFromModuleToAccount
	}
	m.moduleBalances = m.moduleBalances.Sub(amt...)
	return nil
}

func setupMsgServerKeeper() (*Keeper, sdk.Context, *mockPrivacyBankKeeper) {
	storeKey := storetypes.NewKVStoreKey(privacytypes.StoreKey)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	ctx = ctx.WithChainID(msgServerTestChainID)
	ctx = ctx.WithBlockTime(time.Unix(1700000000, 0))

	bankKeeper := &mockPrivacyBankKeeper{}
	k := NewKeeper(privacytypes.ModuleCdc, runtime.NewKVStoreService(storeKey), paramtypes.Subspace{}, bankKeeper)
	return k, ctx, bankKeeper
}

func fixedFieldBytes(v uint64) []byte {
	bz := make([]byte, fieldElementByteSize)
	binary.BigEndian.PutUint64(bz[fieldElementByteSize-8:], v)
	return bz
}

func testAddress(b byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String()
}

func ensureDepositTestArtifacts(t *testing.T) {
	t.Helper()

	depositArtifactOnce.Do(func() {
		dir, err := os.MkdirTemp("", "clairveil-keeper-zk-*")
		if err != nil {
			depositArtifactErr = err
			return
		}
		if err := os.Setenv(privacyzk.ZKArtifactDirEnv, dir); err != nil {
			depositArtifactErr = err
			return
		}

		depositCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.DepositCircuit{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		depositPK, depositVK, err := groth16.Setup(depositCS)
		if err != nil {
			depositArtifactErr = err
			return
		}
		spendCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.SpendCircuit{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		spendPK, spendVK, err := groth16.Setup(spendCS)
		if err != nil {
			depositArtifactErr = err
			return
		}
		joinSplitCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.JoinSplitCircuit{})
		if err != nil {
			depositArtifactErr = err
			return
		}
		joinSplitPK, joinSplitVK, err := groth16.Setup(joinSplitCS)
		if err != nil {
			depositArtifactErr = err
			return
		}

		depositTestR1CS = depositCS
		depositTestPK = depositPK
		depositArtifactErr = writeKeeperTestArtifacts(dir, []keeperTestArtifact{
			{privacyzk.DepositR1CSFile, depositCS},
			{privacyzk.DepositPKFile, depositPK},
			{privacyzk.DepositVKFile, depositVK},
			{privacyzk.SpendR1CSFile, spendCS},
			{privacyzk.SpendPKFile, spendPK},
			{privacyzk.SpendVKFile, spendVK},
			{privacyzk.JoinSplitR1CSFile, joinSplitCS},
			{privacyzk.JoinSplitPKFile, joinSplitPK},
			{privacyzk.JoinSplitVKFile, joinSplitVK},
		})
	})
	require.NoError(t, depositArtifactErr)
}

type keeperTestArtifact struct {
	filename string
	object   interface {
		WriteTo(io.Writer) (int64, error)
	}
}

func writeKeeperTestArtifacts(dir string, artifacts []keeperTestArtifact) error {
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

type keeperDepositArtifactProvider struct{}

func (keeperDepositArtifactProvider) DepositR1CS() (constraint.ConstraintSystem, error) {
	return depositTestR1CS, nil
}

func (keeperDepositArtifactProvider) DepositProvingKey() (groth16.ProvingKey, error) {
	return depositTestPK, nil
}

type keeperDepositProofRunner struct{}

func (keeperDepositProofRunner) ProveDeposit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, depositWitness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, provingKey, depositWitness)
}

func testDepositMsg(t *testing.T, creator, amountStr string, amount *big.Int, denom string, encryptedNote []byte) *privacytypes.MsgDeposit {
	t.Helper()
	ensureDepositTestArtifacts(t)

	spendPubKey := testKeeperScalarMulBase(big.NewInt(17))
	viewPubKey := testKeeperScalarMulBase(big.NewInt(19))
	spendX, spendY := testKeeperPointBigInts(spendPubKey)
	viewX, viewY := testKeeperPointBigInts(viewPubKey)
	note, err := privacytypes.NewNote(spendX, spendY, viewX, viewY, amount, denom, "test")
	require.NoError(t, err)

	proof, err := privacydeposit.BuildDepositProof(*note, keeperDepositArtifactProvider{}, keeperDepositProofRunner{})
	require.NoError(t, err)

	commitmentBytes := fixedFieldBytesFromBigInt(t, note.ComputeCommitment())
	return privacytypes.NewMsgDeposit(creator, amountStr, commitmentBytes, encryptedNote, proof)
}

func testKeeperScalarMulBase(scalar *big.Int) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)
	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&base, scalar)
	return pubKey
}

func testKeeperPointBigInts(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := new(big.Int)
	y := new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	return x, y
}

func fixedFieldBytesFromBigInt(t *testing.T, value *big.Int) []byte {
	t.Helper()
	bz := value.Bytes()
	require.LessOrEqual(t, len(bz), fieldElementByteSize)
	out := make([]byte, fieldElementByteSize)
	copy(out[fieldElementByteSize-len(bz):], bz)
	return out
}

func TestMsgServerDepositSuccess(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	msg := testDepositMsg(t, testAddress(0x11), "1uclair", big.NewInt(1), "uclair", []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)
	require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, uint64(1), k.GetLeafCount(ctx))

	snapshot, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, "uclair", snapshot.Denom)
	require.Equal(t, "1", snapshot.ModuleBalance.String())
	require.Equal(t, "1", snapshot.TotalDeposited.String())
	require.Equal(t, "0", snapshot.TotalWithdrawn.String())
	require.Equal(t, "0", snapshot.ApprovedAdjustment.String())
	require.Equal(t, "1", snapshot.ExpectedModuleBalance.String())
	require.True(t, snapshot.InvariantHolds)

	root := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
	require.NotEmpty(t, root)
	require.True(t, k.CheckHistoricalRoot(ctx, root))
}

func TestMsgServerDepositEmitsExpectedEvent(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	msg := testDepositMsg(t, testAddress(0x13), "1uclair", big.NewInt(1), "uclair", []byte{0xaa, 0xbb})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)

	var depositEvent sdk.Event
	found := false
	for _, event := range ctx.EventManager().Events() {
		if event.Type == privacytypes.EventTypeDeposit {
			depositEvent = event
			found = true
			break
		}
	}
	require.True(t, found)

	creatorAttr, ok := depositEvent.GetAttribute(privacytypes.AttributeKeyCreator)
	require.True(t, ok)
	require.Equal(t, msg.Creator, creatorAttr.Value)

	commitmentAttr, ok := depositEvent.GetAttribute(privacytypes.AttributeKeyCommitment)
	require.True(t, ok)
	require.Equal(t, fmt.Sprintf("%x", msg.NoteCommitment), commitmentAttr.Value)

	encryptedAttr, ok := depositEvent.GetAttribute(privacytypes.AttributeKeyEncryptedNote)
	require.True(t, ok)
	require.Equal(t, fmt.Sprintf("%x", msg.EncryptedNote), encryptedAttr.Value)
}

func TestMsgServerDepositRejectsInvalidCommitmentBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	msg := privacytypes.NewMsgDeposit(testAddress(0x22), "1uclair", []byte{0x01}, []byte{0x01}, []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "note commitment must be canonical 32-byte field bytes")
	require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, uint64(0), k.GetLeafCount(ctx))
}

func TestMsgServerDepositRejectsFullTreeBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)
	k.SetLeafCount(ctx, MaxMerkleLeaves)

	msg := privacytypes.NewMsgDeposit(testAddress(0x24), "1uclair", fixedFieldBytes(3), []byte{0x01}, []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not enough merkle tree capacity for deposit output")
	require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))
}

func TestMsgServerDepositRejectsUnsafeMissingRootBeforeBank(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)
	k.SetLeafCount(ctx, MaxMerkleRebuildLeaves+1)

	msg := privacytypes.NewMsgDeposit(testAddress(0x25), "1uclair", fixedFieldBytes(4), []byte{0x01}, []byte{0x01})

	_, err := server.Deposit(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cached root is required")
	require.NotContains(t, err.Error(), "not enough merkle tree capacity")
	require.Equal(t, 0, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, MaxMerkleRebuildLeaves+1, k.GetLeafCount(ctx))
}

func TestMsgServerWithdrawRejectsRootNotFoundBeforeZK(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x33),
		[]byte{0x01},
		fixedFieldBytes(10),
		fixedFieldBytes(11),
		"1uclair",
		testAddress(0x44),
		msgServerTestChainID,
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "withdraw root was not found in the historical merkle roots")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
}

func TestMsgServerWithdrawRejectsUsedNullifierBeforeZK(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(20)
	nullifier := fixedFieldBytes(21)
	k.SetHistoricalRoot(ctx, root)
	k.SetNullifier(ctx, nullifier)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x55),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x66),
		msgServerTestChainID,
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "withdraw nullifier was already used")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
}

func TestMsgServerWithdrawRejectsInvalidRecipientBeforeZK(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(23)
	nullifier := fixedFieldBytes(24)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x57),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		"invalid-recipient",
		msgServerTestChainID,
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "recipient address")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerWithdrawRejectsChainIDMismatch(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(26)
	nullifier := fixedFieldBytes(27)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x59),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x6a),
		"wrong-chain",
		msgServerTestExpiry,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chain id mismatch")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerWithdrawRejectsExpiredPayload(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(29)
	nullifier := fixedFieldBytes(30)
	k.SetHistoricalRoot(ctx, root)

	expiredAt := ctx.BlockTime().Add(-time.Second).Unix()
	msg := privacytypes.NewMsgWithdraw(
		testAddress(0x5b),
		[]byte{0x01},
		root,
		nullifier,
		"1uclair",
		testAddress(0x6c),
		msgServerTestChainID,
		expiredAt,
	)

	_, err := server.Withdraw(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "withdraw payload has expired")
	require.Equal(t, 0, bankKeeper.fromModuleToAccountCalls)
	require.False(t, k.HasNullifier(ctx, nullifier))
}

func TestMsgServerTransferRejectsRootNotFoundBeforeZK(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	msg := privacytypes.NewMsgTransfer(
		testAddress(0x77),
		[]byte{0x01},
		fixedFieldBytes(30),
		[][]byte{fixedFieldBytes(31), fixedFieldBytes(32)},
		[][]byte{fixedFieldBytes(33), fixedFieldBytes(34)},
		[][]byte{{0x01}, {0x02}},
	)

	_, err := server.Transfer(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transfer root was not found in the historical merkle roots")
}

func TestMsgServerTransferRejectsInvalidNullifierCountBeforeZK(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	server := NewMsgServerImpl(*k)

	root := fixedFieldBytes(40)
	k.SetHistoricalRoot(ctx, root)

	msg := privacytypes.NewMsgTransfer(
		testAddress(0x88),
		[]byte{0x01},
		root,
		[][]byte{fixedFieldBytes(41)},
		[][]byte{fixedFieldBytes(42), fixedFieldBytes(43)},
		[][]byte{{0x01}, {0x02}},
	)

	_, err := server.Transfer(sdk.WrapSDKContext(ctx), msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transfer requires exactly 2 nullifiers")
}

func TestMsgServerTransferRejectsInsufficientBatchCapacityBeforeProof(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves-1)

	root := fixedFieldBytes(50)
	auditPubKey := fixedFieldBytes(51)
	k.SetHistoricalRoot(ctx, root)
	k.SetAuditMasterPubkey(ctx, auditPubKey)

	err := msgServer{Keeper: *k}.executeShieldedTransfer(ctx, shieldedTransferRequest{
		relayer:                     testAddress(0x89),
		proof:                       []byte{0x01},
		root:                        root,
		nullifiers:                  [][]byte{fixedFieldBytes(52), fixedFieldBytes(53)},
		newCommitments:              [][]byte{fixedFieldBytes(54), fixedFieldBytes(55)},
		cipherTexts:                 [][]byte{{0x01}, {0x02}},
		auditDisclosureTargetPubKey: auditPubKey,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not enough merkle tree capacity for transfer outputs")
	require.Equal(t, MaxMerkleLeaves-1, k.GetLeafCount(ctx))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(52)))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(53)))
}

func TestMsgServerTransferRejectsOverflowAsTreeStateError(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves+1)

	root := fixedFieldBytes(60)
	auditPubKey := fixedFieldBytes(61)
	k.SetHistoricalRoot(ctx, root)
	k.SetAuditMasterPubkey(ctx, auditPubKey)

	err := msgServer{Keeper: *k}.executeShieldedTransfer(ctx, shieldedTransferRequest{
		relayer:                     testAddress(0x8a),
		proof:                       []byte{0x01},
		root:                        root,
		nullifiers:                  [][]byte{fixedFieldBytes(62), fixedFieldBytes(63)},
		newCommitments:              [][]byte{fixedFieldBytes(64), fixedFieldBytes(65)},
		cipherTexts:                 [][]byte{{0x01}, {0x02}},
		auditDisclosureTargetPubKey: auditPubKey,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max capacity")
	require.NotContains(t, err.Error(), "not enough merkle tree capacity")
	require.Equal(t, MaxMerkleLeaves+1, k.GetLeafCount(ctx))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(62)))
	require.False(t, k.HasNullifier(ctx, fixedFieldBytes(63)))
}
