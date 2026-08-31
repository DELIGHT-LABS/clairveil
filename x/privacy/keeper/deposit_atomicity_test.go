package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const cacheAwareDepositBankStoreKey = "privacy_deposit_test_bank"

type cacheAwareDepositBankKeeper struct {
	storeService             corestore.KVStoreService
	getBalanceCalls          int
	fromAccountToModuleCalls int
	lastAccountSender        sdk.AccAddress
	lastAccountModule        string
	lastAccountAmount        sdk.Coins
	errFromAccountToModule   error
	redirectAccountToModule  sdk.AccAddress
}

func cacheAwareDepositBalanceKey(address sdk.AccAddress, denom string) []byte {
	key := make([]byte, 3, 3+len(address)+len(denom))
	key[0] = 0x01
	binary.BigEndian.PutUint16(key[1:], uint16(len(address)))
	key = append(key, address...)
	key = append(key, denom...)
	return key
}

func (b *cacheAwareDepositBankKeeper) balance(ctx context.Context, address sdk.AccAddress, denom string) (sdkmath.Int, error) {
	bz, err := b.storeService.OpenKVStore(ctx).Get(cacheAwareDepositBalanceKey(address, denom))
	if err != nil {
		return sdkmath.Int{}, err
	}
	if len(bz) == 0 {
		return sdkmath.ZeroInt(), nil
	}
	amount, ok := sdkmath.NewIntFromString(string(bz))
	if !ok || amount.IsNegative() {
		return sdkmath.Int{}, fmt.Errorf("cache-aware bank balance is corrupt")
	}
	return amount, nil
}

func (b *cacheAwareDepositBankKeeper) setBalance(ctx context.Context, address sdk.AccAddress, denom string, amount sdkmath.Int) error {
	if amount.IsNegative() {
		return fmt.Errorf("cache-aware bank balance must not be negative")
	}
	store := b.storeService.OpenKVStore(ctx)
	key := cacheAwareDepositBalanceKey(address, denom)
	if amount.IsZero() {
		return store.Delete(key)
	}
	return store.Set(key, []byte(amount.String()))
}

func (b *cacheAwareDepositBankKeeper) GetBalance(ctx context.Context, address sdk.AccAddress, denom string) sdk.Coin {
	b.getBalanceCalls++
	amount, err := b.balance(ctx, address, denom)
	if err != nil {
		panic(err)
	}
	return sdk.NewCoin(denom, amount)
}

func (b *cacheAwareDepositBankKeeper) transfer(ctx context.Context, from, to sdk.AccAddress, amount sdk.Coins) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	for _, coin := range amount {
		balance, err := b.balance(ctx, from, coin.Denom)
		if err != nil {
			return err
		}
		if balance.LT(coin.Amount) {
			return errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient funds: available %s, required %s", balance, coin.Amount)
		}
	}
	if bytes.Equal(from, to) {
		return nil
	}
	for _, coin := range amount {
		fromBalance, err := b.balance(ctx, from, coin.Denom)
		if err != nil {
			return err
		}
		toBalance, err := b.balance(ctx, to, coin.Denom)
		if err != nil {
			return err
		}
		if err := b.setBalance(ctx, from, coin.Denom, fromBalance.Sub(coin.Amount)); err != nil {
			return err
		}
		if err := b.setBalance(ctx, to, coin.Denom, toBalance.Add(coin.Amount)); err != nil {
			return err
		}
	}
	return nil
}

func (b *cacheAwareDepositBankKeeper) SendCoinsFromAccountToModule(
	ctx context.Context,
	sender sdk.AccAddress,
	recipientModule string,
	amount sdk.Coins,
) error {
	b.fromAccountToModuleCalls++
	b.lastAccountSender = append(sdk.AccAddress(nil), sender...)
	b.lastAccountModule = recipientModule
	b.lastAccountAmount = append(sdk.Coins(nil), amount...)
	if b.errFromAccountToModule != nil {
		return b.errFromAccountToModule
	}
	if recipientModule != privacytypes.ModuleName {
		return fmt.Errorf("unexpected recipient module %q", recipientModule)
	}
	recipient := authtypes.NewModuleAddress(recipientModule)
	if len(b.redirectAccountToModule) != 0 {
		recipient = b.redirectAccountToModule
	}
	return b.transfer(ctx, sender, recipient, amount)
}

func (b *cacheAwareDepositBankKeeper) SendCoinsFromModuleToAccount(
	ctx context.Context,
	senderModule string,
	recipient sdk.AccAddress,
	amount sdk.Coins,
) error {
	if senderModule != privacytypes.ModuleName {
		return fmt.Errorf("unexpected sender module %q", senderModule)
	}
	return b.transfer(ctx, authtypes.NewModuleAddress(senderModule), recipient, amount)
}

func (b *cacheAwareDepositBankKeeper) setTestBalance(
	t testing.TB,
	ctx sdk.Context,
	address sdk.AccAddress,
	denom string,
	amount int64,
) {
	t.Helper()
	require.NoError(t, b.setBalance(ctx, address, denom, sdkmath.NewInt(amount)))
}

func (b *cacheAwareDepositBankKeeper) testBalance(
	t testing.TB,
	ctx sdk.Context,
	address sdk.AccAddress,
	denom string,
) sdkmath.Int {
	t.Helper()
	amount, err := b.balance(ctx, address, denom)
	require.NoError(t, err)
	return amount
}

func setupCacheAwareDepositKeeper(t testing.TB) (*Keeper, sdk.Context, *cacheAwareDepositBankKeeper) {
	t.Helper()
	privacyKey := storetypes.NewKVStoreKey(privacytypes.StoreKey)
	bankKey := storetypes.NewKVStoreKey(cacheAwareDepositBankStoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_deposit_test")
	ctx := testutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			privacytypes.StoreKey:         privacyKey,
			cacheAwareDepositBankStoreKey: bankKey,
		},
		map[string]*storetypes.TransientStoreKey{
			"transient_deposit_test": transientKey,
		},
		nil,
	)
	ctx = ctx.WithChainID(msgServerTestChainID)
	ctx = ctx.WithBlockHeight(71)
	ctx = ctx.WithBlockTime(time.Unix(1700000000, 0))
	ctx = ctx.WithTxBytes([]byte("cache-aware-deposit-test"))

	bankKeeper := &cacheAwareDepositBankKeeper{storeService: runtime.NewKVStoreService(bankKey)}
	k := NewKeeper(
		privacytypes.ModuleCdc,
		runtime.NewKVStoreService(privacyKey),
		paramtypes.Subspace{},
		bankKeeper,
	)
	_, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)
	return k, ctx, bankKeeper
}

func requireNoCommittedDeposit(
	t testing.TB,
	k *Keeper,
	ctx sdk.Context,
	bankKeeper *cacheAwareDepositBankKeeper,
	actor, funder sdk.AccAddress,
	actorBalance, funderBalance int64,
	commitment []byte,
) {
	t.Helper()
	moduleAddress := authtypes.NewModuleAddress(privacytypes.ModuleName)
	require.Equal(t, sdkmath.NewInt(actorBalance), bankKeeper.testBalance(t, ctx, actor, "uclair"))
	require.Equal(t, sdkmath.NewInt(funderBalance), bankKeeper.testBalance(t, ctx, funder, "uclair"))
	require.True(t, bankKeeper.testBalance(t, ctx, moduleAddress, "uclair").IsZero())
	require.Zero(t, k.GetLeafCount(ctx))
	exists, err := k.HasCommitment(ctx, commitment)
	require.NoError(t, err)
	require.False(t, exists)
	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Zero(t, sequence)
	require.Empty(t, ctx.EventManager().Events())
}

func TestDepositWithFunderMovesOnlyFunderBalance(t *testing.T) {
	k, ctx, bankKeeper := setupCacheAwareDepositKeeper(t)
	actorString := testAddress(0x71)
	actor, err := sdk.AccAddressFromBech32(actorString)
	require.NoError(t, err)
	funder := sdk.AccAddress(make([]byte, 20))
	for i := range funder {
		funder[i] = 0x72
	}
	moduleAddress := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bankKeeper.setTestBalance(t, ctx, actor, "uclair", 50)
	bankKeeper.setTestBalance(t, ctx, funder, "uclair", 20)
	msg := testDepositMsg(t, actorString, "7uclair", big.NewInt(7), "uclair", []byte{0x71})

	_, err = k.DepositWithFunder(ctx, msg, funder)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(50), bankKeeper.testBalance(t, ctx, actor, "uclair"))
	require.Equal(t, sdkmath.NewInt(13), bankKeeper.testBalance(t, ctx, funder, "uclair"))
	require.Equal(t, sdkmath.NewInt(7), bankKeeper.testBalance(t, ctx, moduleAddress, "uclair"))
	require.Equal(t, funder, bankKeeper.lastAccountSender)
	require.Equal(t, privacytypes.ModuleName, bankKeeper.lastAccountModule)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uclair", 7)), bankKeeper.lastAccountAmount)

	snapshot, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, "7", snapshot.TotalDeposited.String())
	require.Equal(t, "7", snapshot.ModuleBalance.String())
	require.True(t, snapshot.InvariantHolds)
	require.Equal(t, uint64(1), k.GetLeafCount(ctx))
	requireExactDepositEvent(t, ctx, msg)

	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), sequence)
	events, hasMore, err := k.GetPrivacyEvents(ctx, -1, 1, 10, nil)
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Len(t, events, 1)
	creatorFound := false
	for _, attribute := range events[0].Attributes {
		if attribute.Key == privacytypes.AttributeKeyCreator {
			creatorFound = true
			require.Equal(t, actorString, attribute.Value)
		}
		require.NotEqual(t, "funder", attribute.Key)
		require.NotEqual(t, "amount", attribute.Key)
	}
	require.True(t, creatorFound)

	scan, err := k.GetPrivacyScanPageV2(ctx, nil, 1, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	require.Len(t, scan.Summaries, 1)
	require.Len(t, scan.Outputs, 1)
	require.Equal(t, msg.NoteCommitment, scan.Outputs[0].Commitment)
}

func TestCacheAwareDepositBankKeeperSelfTransferIsNoOp(t *testing.T) {
	_, ctx, bankKeeper := setupCacheAwareDepositKeeper(t)
	moduleAddress := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bankKeeper.setTestBalance(t, ctx, moduleAddress, "uclair", 20)

	err := bankKeeper.SendCoinsFromAccountToModule(
		ctx,
		moduleAddress,
		privacytypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin("uclair", 7)),
	)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(20), bankKeeper.testBalance(t, ctx, moduleAddress, "uclair"))
}

func TestDepositWithFunderRejectsPrivacyModuleFunder(t *testing.T) {
	k, ctx, bankKeeper := setupCacheAwareDepositKeeper(t)
	actorString := testAddress(0x79)
	moduleAddress := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bankKeeper.setTestBalance(t, ctx, moduleAddress, "uclair", 20)
	msg := testDepositMsg(t, actorString, "7uclair", big.NewInt(7), "uclair", []byte{0x79})
	privacyBefore := collectStoreEntries(t, k.storeService, ctx)
	bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)

	_, err := k.DepositWithFunder(ctx, msg, moduleAddress)
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidAddress)
	require.ErrorContains(t, err, "deposit funder must not be the privacy module account")
	require.Zero(t, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, sdkmath.NewInt(20), bankKeeper.testBalance(t, ctx, moduleAddress, "uclair"))
	require.Zero(t, k.GetLeafCount(ctx))
	exists, hasErr := k.HasCommitment(ctx, msg.NoteCommitment)
	require.NoError(t, hasErr)
	require.False(t, exists)
	require.Empty(t, ctx.EventManager().Events())
	require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
	require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
}

func TestDepositWithFunderRejectsRedirectedModuleTransfer(t *testing.T) {
	k, ctx, bankKeeper := setupCacheAwareDepositKeeper(t)
	actorString := testAddress(0x7a)
	actor, err := sdk.AccAddressFromBech32(actorString)
	require.NoError(t, err)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x7b}, 20))
	redirectedRecipient := sdk.AccAddress(bytes.Repeat([]byte{0x7c}, 20))
	bankKeeper.redirectAccountToModule = redirectedRecipient
	bankKeeper.setTestBalance(t, ctx, funder, "uclair", 20)
	msg := testDepositMsg(t, actorString, "7uclair", big.NewInt(7), "uclair", []byte{0x7a})
	privacyBefore := collectStoreEntries(t, k.storeService, ctx)
	bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)

	_, err = k.DepositWithFunder(ctx, msg, funder)
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrLogic)
	require.ErrorContains(t, err, "privacy module balance mismatch after deposit bank transfer")
	require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
	requireNoCommittedDeposit(t, k, ctx, bankKeeper, actor, funder, 0, 20, msg.NoteCommitment)
	require.True(t, bankKeeper.testBalance(t, ctx, redirectedRecipient, "uclair").IsZero())
	require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
	require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
}

func TestDepositWithFunderZeroValueKeepsTransparentBalances(t *testing.T) {
	k, ctx, bankKeeper := setupCacheAwareDepositKeeper(t)
	actorString := testAddress(0x77)
	actor, err := sdk.AccAddressFromBech32(actorString)
	require.NoError(t, err)
	funder := sdk.AccAddress(make([]byte, 20))
	for i := range funder {
		funder[i] = 0x78
	}
	moduleAddress := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bankKeeper.setTestBalance(t, ctx, actor, "uclair", 50)
	bankKeeper.setTestBalance(t, ctx, funder, "uclair", 20)
	msg := testDepositMsg(t, actorString, "0uclair", big.NewInt(0), "uclair", []byte{0x77})

	_, err = k.DepositWithFunder(ctx, msg, funder)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(50), bankKeeper.testBalance(t, ctx, actor, "uclair"))
	require.Equal(t, sdkmath.NewInt(20), bankKeeper.testBalance(t, ctx, funder, "uclair"))
	require.True(t, bankKeeper.testBalance(t, ctx, moduleAddress, "uclair").IsZero())
	require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
	require.Empty(t, bankKeeper.lastAccountAmount)
	require.Equal(t, uint64(1), k.GetLeafCount(ctx))

	snapshot, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.True(t, snapshot.TotalDeposited.IsZero())
	require.True(t, snapshot.ModuleBalance.IsZero())
	require.True(t, snapshot.InvariantHolds)
	requireExactDepositEvent(t, ctx, msg)
}

func TestDepositWithFunderMutationFailuresRollback(t *testing.T) {
	actorString := testAddress(0x73)
	actor, err := sdk.AccAddressFromBech32(actorString)
	require.NoError(t, err)
	funder := sdk.AccAddress(make([]byte, 20))
	for i := range funder {
		funder[i] = 0x74
	}
	baseMsg := testDepositMsg(t, actorString, "7uclair", big.NewInt(7), "uclair", []byte{0x73})

	newState := func(t testing.TB, funderBalance int64) (*Keeper, sdk.Context, *cacheAwareDepositBankKeeper) {
		t.Helper()
		k, ctx, bankKeeper := setupCacheAwareDepositKeeper(t)
		bankKeeper.setTestBalance(t, ctx, actor, "uclair", 50)
		bankKeeper.setTestBalance(t, ctx, funder, "uclair", funderBalance)
		return k, ctx, bankKeeper
	}

	t.Run("insufficient funder balance", func(t *testing.T) {
		k, ctx, bankKeeper := newState(t, 3)
		privacyBefore := collectStoreEntries(t, k.storeService, ctx)
		bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)
		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.Error(t, err)
		require.True(t, errors.Is(err, sdkerrors.ErrInsufficientFunds), err)
		require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
		requireNoCommittedDeposit(t, k, ctx, bankKeeper, actor, funder, 50, 3, baseMsg.NoteCommitment)
		snapshot, snapshotErr := k.GetReserveSnapshot(ctx, "uclair")
		require.NoError(t, snapshotErr)
		require.True(t, snapshot.TotalDeposited.IsZero())
		require.True(t, snapshot.InvariantHolds)
		require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
		require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
	})

	t.Run("bank failure", func(t *testing.T) {
		k, ctx, bankKeeper := newState(t, 20)
		bankKeeper.errFromAccountToModule = errors.New("injected bank failure")
		privacyBefore := collectStoreEntries(t, k.storeService, ctx)
		bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)
		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "failed to lock tokens")
		require.ErrorContains(t, err, "injected bank failure")
		require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
		requireNoCommittedDeposit(t, k, ctx, bankKeeper, actor, funder, 50, 20, baseMsg.NoteCommitment)
		require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
		require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
	})

	t.Run("reserve failure after bank debit", func(t *testing.T) {
		k, ctx, bankKeeper := newState(t, 20)
		store := k.storeService.OpenKVStore(ctx)
		reserveKey := privacytypes.GetReserveDepositKey("uclair")
		require.NoError(t, store.Set(reserveKey, []byte("not-an-int")))
		privacyBefore := collectStoreEntries(t, k.storeService, ctx)
		bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)

		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "failed to record privacy reserve deposit")
		require.ErrorContains(t, err, "stored reserve amount is invalid")
		require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
		requireNoCommittedDeposit(t, k, ctx, bankKeeper, actor, funder, 50, 20, baseMsg.NoteCommitment)
		stored, getErr := store.Get(reserveKey)
		require.NoError(t, getErr)
		require.Equal(t, []byte("not-an-int"), stored)
		require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
		require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
	})

	t.Run("tree snapshot failure after reserve", func(t *testing.T) {
		k, ctx, bankKeeper := newState(t, 20)
		wouldBeRoot := naiveRootFromLeaves([][]byte{baseMsg.NoteCommitment})
		store := k.storeService.OpenKVStore(ctx)
		rootSnapshotKey := privacytypes.GetMerkleRootSnapshotKey(wouldBeRoot)
		require.NoError(t, store.Set(rootSnapshotKey, []byte{0xff}))
		privacyBefore := collectStoreEntries(t, k.storeService, ctx)
		bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)

		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "failed to append the note commitment")
		require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
		requireNoCommittedDeposit(t, k, ctx, bankKeeper, actor, funder, 50, 20, baseMsg.NoteCommitment)
		snapshot, snapshotErr := k.GetReserveSnapshot(ctx, "uclair")
		require.NoError(t, snapshotErr)
		require.True(t, snapshot.TotalDeposited.IsZero())
		stored, getErr := store.Get(rootSnapshotKey)
		require.NoError(t, getErr)
		require.Equal(t, []byte{0xff}, stored)
		historical, getErr := store.Has(privacytypes.GetHistoricalRootKey(wouldBeRoot))
		require.NoError(t, getErr)
		require.False(t, historical)
		require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
		require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
	})

	t.Run("event index failure after tree append", func(t *testing.T) {
		k, ctx, bankKeeper := newState(t, 20)
		store := k.storeService.OpenKVStore(ctx)
		sequenceIndexKey := privacytypes.GetPrivacyScanSequenceKey(1)
		require.NoError(t, store.Set(sequenceIndexKey, sdk.Uint64ToBigEndian(uint64(ctx.BlockHeight()))))
		wouldBeRoot := naiveRootFromLeaves([][]byte{baseMsg.NoteCommitment})
		privacyBefore := collectStoreEntries(t, k.storeService, ctx)
		bankBefore := collectStoreEntries(t, bankKeeper.storeService, ctx)

		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "failed to index deposit privacy event")
		require.ErrorContains(t, err, "privacy scan global sequence is already indexed")
		require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
		requireNoCommittedDeposit(t, k, ctx, bankKeeper, actor, funder, 50, 20, baseMsg.NoteCommitment)
		snapshot, snapshotErr := k.GetReserveSnapshot(ctx, "uclair")
		require.NoError(t, snapshotErr)
		require.True(t, snapshot.TotalDeposited.IsZero())

		for _, key := range [][]byte{
			privacytypes.GetPrivacyScanSummaryKey(ctx.BlockHeight(), 1),
			privacytypes.GetPrivacyScanOutputKey(ctx.BlockHeight(), 1, 0),
			privacytypes.GetPrivacyEventKey(ctx.BlockHeight(), 1),
			privacytypes.GetMerkleRootSnapshotKey(wouldBeRoot),
			privacytypes.GetHistoricalRootKey(wouldBeRoot),
		} {
			exists, hasErr := store.Has(key)
			require.NoError(t, hasErr)
			require.False(t, exists, "%x", key)
		}
		stored, getErr := store.Get(sequenceIndexKey)
		require.NoError(t, getErr)
		require.Equal(t, sdk.Uint64ToBigEndian(uint64(ctx.BlockHeight())), stored)
		require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, ctx))
		require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, ctx))
	})
}

func TestDepositWithFunderOuterCacheRollback(t *testing.T) {
	k, parentCtx, bankKeeper := setupCacheAwareDepositKeeper(t)
	actorString := testAddress(0x75)
	actor, err := sdk.AccAddressFromBech32(actorString)
	require.NoError(t, err)
	funder := sdk.AccAddress(make([]byte, 20))
	for i := range funder {
		funder[i] = 0x76
	}
	moduleAddress := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bankKeeper.setTestBalance(t, parentCtx, actor, "uclair", 50)
	bankKeeper.setTestBalance(t, parentCtx, funder, "uclair", 20)
	msg := testDepositMsg(t, actorString, "7uclair", big.NewInt(7), "uclair", []byte{0x75})
	privacyBefore := collectStoreEntries(t, k.storeService, parentCtx)
	bankBefore := collectStoreEntries(t, bankKeeper.storeService, parentCtx)

	outerCtx, _ := parentCtx.CacheContext()
	_, err = k.DepositWithFunder(outerCtx, msg, funder)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(50), bankKeeper.testBalance(t, outerCtx, actor, "uclair"))
	require.Equal(t, sdkmath.NewInt(13), bankKeeper.testBalance(t, outerCtx, funder, "uclair"))
	require.Equal(t, sdkmath.NewInt(7), bankKeeper.testBalance(t, outerCtx, moduleAddress, "uclair"))
	require.Equal(t, uint64(1), k.GetLeafCount(outerCtx))
	outerSequence, err := k.GetPrivacyGlobalSequence(outerCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), outerSequence)
	requireExactDepositEvent(t, outerCtx, msg)

	policyErr := errors.New("injected downstream policy failure")
	require.Error(t, policyErr)
	// Do not call the outer write function: the downstream failure discards it.

	requireNoCommittedDeposit(t, k, parentCtx, bankKeeper, actor, funder, 50, 20, msg.NoteCommitment)
	snapshot, err := k.GetReserveSnapshot(parentCtx, "uclair")
	require.NoError(t, err)
	require.True(t, snapshot.TotalDeposited.IsZero())
	require.True(t, snapshot.ModuleBalance.IsZero())
	require.True(t, snapshot.InvariantHolds)
	require.Equal(t, privacyBefore, collectStoreEntries(t, k.storeService, parentCtx))
	require.Equal(t, bankBefore, collectStoreEntries(t, bankKeeper.storeService, parentCtx))
}
