package keeper

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"testing"

	corestore "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func cloneDepositMessage(msg *privacytypes.MsgDeposit) *privacytypes.MsgDeposit {
	if msg == nil {
		return nil
	}
	clone := *msg
	clone.NoteCommitment = append([]byte(nil), msg.NoteCommitment...)
	clone.EncryptedNote = append([]byte(nil), msg.EncryptedNote...)
	clone.Proof = append([]byte(nil), msg.Proof...)
	return &clone
}

func collectStoreEntries(t testing.TB, service corestore.KVStoreService, ctx sdk.Context) map[string][]byte {
	t.Helper()
	store := service.OpenKVStore(ctx)
	iterator, err := store.Iterator(nil, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, iterator.Close())
	}()

	entries := make(map[string][]byte)
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	require.NoError(t, iterator.Error())
	return entries
}

func requireExactDepositEvent(t testing.TB, ctx sdk.Context, msg *privacytypes.MsgDeposit) {
	t.Helper()
	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, privacytypes.EventTypeDeposit, events[0].Type)
	expected := []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyCreator, msg.Creator),
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment, fmt.Sprintf("%x", msg.NoteCommitment)),
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, fmt.Sprintf("%x", msg.EncryptedNote)),
	}
	require.Len(t, events[0].Attributes, len(expected))
	for i, attribute := range events[0].Attributes {
		require.Equal(t, expected[i].Key, attribute.Key)
		require.Equal(t, expected[i].Value, attribute.Value)
		require.False(t, attribute.Index)
	}
}

func TestDepositWithFunderMatchesMsgServerDeposit(t *testing.T) {
	actor := testAddress(0x61)
	baseMsg := testDepositMsg(t, actor, "7uclair", big.NewInt(7), "uclair", []byte{0x61})

	publicKeeper, publicCtx, publicBank := setupCacheAwareDepositKeeper(t)
	trustedKeeper, trustedCtx, trustedBank := setupCacheAwareDepositKeeper(t)
	publicCtx = publicCtx.WithBlockHeight(61).WithTxBytes([]byte("deposit-funder-equivalence"))
	trustedCtx = trustedCtx.WithBlockHeight(61).WithTxBytes([]byte("deposit-funder-equivalence"))
	publicCtx = publicCtx.WithGasMeter(storetypes.NewGasMeter(10 * DepositProofVerificationGas))
	trustedCtx = trustedCtx.WithGasMeter(storetypes.NewGasMeter(10 * DepositProofVerificationGas))
	actorAddress, err := sdk.AccAddressFromBech32(actor)
	require.NoError(t, err)
	publicBank.setTestBalance(t, publicCtx, actorAddress, "uclair", 20)
	trustedBank.setTestBalance(t, trustedCtx, actorAddress, "uclair", 20)

	publicResponse, publicErr := NewMsgServerImpl(*publicKeeper).Deposit(
		sdk.WrapSDKContext(publicCtx),
		cloneDepositMessage(baseMsg),
	)
	trustedResponse, trustedErr := trustedKeeper.DepositWithFunder(
		trustedCtx,
		cloneDepositMessage(baseMsg),
		actorAddress,
	)

	require.NoError(t, publicErr)
	require.NoError(t, trustedErr)
	require.Equal(t, publicResponse, trustedResponse)
	require.Equal(t, publicCtx.GasMeter().GasConsumed(), trustedCtx.GasMeter().GasConsumed())
	require.Equal(t, actorAddress, publicBank.lastAccountSender)
	require.Equal(t, actorAddress, trustedBank.lastAccountSender)
	require.Equal(t, publicBank.lastAccountAmount, trustedBank.lastAccountAmount)
	require.Equal(t, collectStoreEntries(t, publicKeeper.storeService, publicCtx), collectStoreEntries(t, trustedKeeper.storeService, trustedCtx))
	require.Equal(t, collectStoreEntries(t, publicBank.storeService, publicCtx), collectStoreEntries(t, trustedBank.storeService, trustedCtx))
	require.Equal(t, publicCtx.EventManager().Events(), trustedCtx.EventManager().Events())
	require.Equal(t, "13", publicBank.testBalance(t, publicCtx, actorAddress, "uclair").String())
	require.Equal(t, "7", publicBank.testBalance(t, publicCtx, authtypes.NewModuleAddress(privacytypes.ModuleName), "uclair").String())

	publicEvents, publicHasMore, err := publicKeeper.GetPrivacyEvents(publicCtx, -1, 1, 10, nil)
	require.NoError(t, err)
	trustedEvents, trustedHasMore, err := trustedKeeper.GetPrivacyEvents(trustedCtx, -1, 1, 10, nil)
	require.NoError(t, err)
	require.Equal(t, publicHasMore, trustedHasMore)
	require.Equal(t, publicEvents, trustedEvents)

	publicScan, err := publicKeeper.GetPrivacyScanPageV2(publicCtx, nil, 1, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	trustedScan, err := trustedKeeper.GetPrivacyScanPageV2(trustedCtx, nil, 1, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	require.Equal(t, publicScan, trustedScan)
	requireExactDepositEvent(t, publicCtx, baseMsg)
	requireExactDepositEvent(t, trustedCtx, baseMsg)
}

func TestDepositWithFunderUsesExplicitBankSender(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	actor := testAddress(0x62)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x63}, 20))
	msg := testDepositMsg(t, actor, "5uclair", big.NewInt(5), "uclair", []byte{0x62})

	_, err := k.DepositWithFunder(ctx, msg, funder)
	require.NoError(t, err)
	require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, funder, bankKeeper.lastAccountSender)
	require.NotEqual(t, msg.Creator, bankKeeper.lastAccountSender.String())
	require.Equal(t, privacytypes.ModuleName, bankKeeper.lastAccountModule)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uclair", 5)), bankKeeper.lastAccountAmount)
	requireExactDepositEvent(t, ctx, msg)
}

func TestDepositWithFunderRejectsInvalidFunder(t *testing.T) {
	actor := testAddress(0x64)
	msg := privacytypes.NewMsgDeposit(
		actor,
		"1uclair",
		fixedFieldBytes(64),
		testKeeperEnvelope(t, privacytypes.EnvelopeDepositNoteV1),
		[]byte{0x01},
	)

	for _, tc := range []struct {
		name   string
		funder sdk.AccAddress
	}{
		{name: "empty", funder: nil},
		{name: "too long", funder: sdk.AccAddress(bytes.Repeat([]byte{0x01}, sdkaddress.MaxAddrLen+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
			before := collectStoreEntries(t, k.storeService, ctx)

			_, err := k.DepositWithFunder(ctx, cloneDepositMessage(msg), tc.funder)
			require.Error(t, err)
			require.True(t, errors.Is(err, sdkerrors.ErrInvalidAddress), err)
			require.ErrorContains(t, err, "invalid deposit funder")
			require.Zero(t, bankKeeper.fromAccountToModuleCalls)
			require.Equal(t, before, collectStoreEntries(t, k.storeService, ctx))
			require.Empty(t, ctx.EventManager().Events())
		})
	}
}

func TestDepositWithFunderPreservesZeroValueAndMessageOwnership(t *testing.T) {
	k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
	actor := testAddress(0x65)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
	msg := testDepositMsg(t, actor, "0uclair", big.NewInt(0), "uclair", []byte{0x65})
	before := cloneDepositMessage(msg)

	_, err := k.DepositWithFunder(ctx, msg, funder)
	require.NoError(t, err)
	require.Equal(t, before, msg)
	require.Equal(t, 1, bankKeeper.fromAccountToModuleCalls)
	require.Equal(t, funder, bankKeeper.lastAccountSender)
	require.Empty(t, bankKeeper.lastAccountAmount)
	require.True(t, bankKeeper.moduleBalances.IsZero())
	require.Equal(t, uint64(1), k.GetLeafCount(ctx))

	snapshot, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.True(t, snapshot.ModuleBalance.IsZero())
	require.True(t, snapshot.TotalDeposited.IsZero())
	require.True(t, snapshot.InvariantHolds)
	requireExactDepositEvent(t, ctx, msg)
}

func TestDepositWithFunderPreMutationFailuresDoNotCallBank(t *testing.T) {
	actor := testAddress(0x67)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x68}, 20))
	baseMsg := testDepositMsg(t, actor, "3uclair", big.NewInt(3), "uclair", []byte{0x67})

	t.Run("invalid proof", func(t *testing.T) {
		k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
		msg := cloneDepositMessage(baseMsg)
		msg.Proof[len(msg.Proof)-1] ^= 1
		before := cloneDepositMessage(msg)
		_, err := k.DepositWithFunder(ctx, msg, funder)
		require.ErrorContains(t, err, "deposit proof verification failed")
		require.Equal(t, before, msg)
		require.Zero(t, bankKeeper.fromAccountToModuleCalls)
		require.Zero(t, k.GetLeafCount(ctx))
		require.Empty(t, ctx.EventManager().Events())
	})

	t.Run("duplicate commitment", func(t *testing.T) {
		k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
		require.NoError(t, k.AppendCommitment(ctx, baseMsg.NoteCommitment))
		callsBefore := bankKeeper.fromAccountToModuleCalls
		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "note commitment already exists")
		require.Equal(t, callsBefore, bankKeeper.fromAccountToModuleCalls)
		require.Equal(t, uint64(1), k.GetLeafCount(ctx))
	})

	t.Run("unregistered asset", func(t *testing.T) {
		k, ctx, bankKeeper := setupMsgServerKeeper()
		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "deposit asset registry validation failed")
		require.Zero(t, bankKeeper.fromAccountToModuleCalls)
		require.Zero(t, k.GetLeafCount(ctx))
	})

	t.Run("merkle capacity", func(t *testing.T) {
		k, ctx, bankKeeper := setupRegisteredMsgServerKeeper(t)
		k.SetLeafCount(ctx, MaxMerkleLeaves)
		_, err := k.DepositWithFunder(ctx, cloneDepositMessage(baseMsg), funder)
		require.ErrorContains(t, err, "not enough merkle tree capacity")
		require.Zero(t, bankKeeper.fromAccountToModuleCalls)
		require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))
	})
}

func TestDepositWithFunderFailuresMatchMsgServerDeposit(t *testing.T) {
	actor := testAddress(0x69)
	actorAddress, err := sdk.AccAddressFromBech32(actor)
	require.NoError(t, err)
	baseMsg := testDepositMsg(t, actor, "3uclair", big.NewInt(3), "uclair", []byte{0x69})

	registeredSetup := func(t testing.TB) (*Keeper, sdk.Context, *mockPrivacyBankKeeper) {
		return setupRegisteredMsgServerKeeper(t)
	}
	unregisteredSetup := func(testing.TB) (*Keeper, sdk.Context, *mockPrivacyBankKeeper) {
		return setupMsgServerKeeper()
	}

	for _, tc := range []struct {
		name    string
		setup   func(testing.TB) (*Keeper, sdk.Context, *mockPrivacyBankKeeper)
		prepare func(testing.TB, *Keeper, sdk.Context, *mockPrivacyBankKeeper)
		mutate  func(*privacytypes.MsgDeposit)
	}{
		{
			name:  "invalid proof",
			setup: registeredSetup,
			mutate: func(msg *privacytypes.MsgDeposit) {
				msg.Proof[len(msg.Proof)-1] ^= 1
			},
		},
		{
			name:  "duplicate commitment",
			setup: registeredSetup,
			prepare: func(t testing.TB, k *Keeper, ctx sdk.Context, _ *mockPrivacyBankKeeper) {
				require.NoError(t, k.AppendCommitment(ctx, baseMsg.NoteCommitment))
			},
		},
		{
			name:  "unregistered asset",
			setup: unregisteredSetup,
		},
		{
			name:  "merkle capacity",
			setup: registeredSetup,
			prepare: func(_ testing.TB, k *Keeper, ctx sdk.Context, _ *mockPrivacyBankKeeper) {
				k.SetLeafCount(ctx, MaxMerkleLeaves)
			},
		},
		{
			name:  "bank failure",
			setup: registeredSetup,
			prepare: func(_ testing.TB, _ *Keeper, _ sdk.Context, bankKeeper *mockPrivacyBankKeeper) {
				bankKeeper.errFromAccountToModule = errors.New("equivalent bank failure")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			publicKeeper, publicCtx, publicBank := tc.setup(t)
			trustedKeeper, trustedCtx, trustedBank := tc.setup(t)
			publicCtx = publicCtx.WithGasMeter(storetypes.NewGasMeter(10 * DepositProofVerificationGas))
			trustedCtx = trustedCtx.WithGasMeter(storetypes.NewGasMeter(10 * DepositProofVerificationGas))
			if tc.prepare != nil {
				tc.prepare(t, publicKeeper, publicCtx, publicBank)
				tc.prepare(t, trustedKeeper, trustedCtx, trustedBank)
			}

			publicMsg := cloneDepositMessage(baseMsg)
			trustedMsg := cloneDepositMessage(baseMsg)
			if tc.mutate != nil {
				tc.mutate(publicMsg)
				tc.mutate(trustedMsg)
			}

			_, publicErr := NewMsgServerImpl(*publicKeeper).Deposit(sdk.WrapSDKContext(publicCtx), publicMsg)
			_, trustedErr := trustedKeeper.DepositWithFunder(trustedCtx, trustedMsg, actorAddress)
			require.Error(t, publicErr)
			require.Error(t, trustedErr)
			require.Equal(t, publicErr.Error(), trustedErr.Error())
			publicCodespace, publicCode, _ := errorsmod.ABCIInfo(publicErr, false)
			trustedCodespace, trustedCode, _ := errorsmod.ABCIInfo(trustedErr, false)
			require.Equal(t, publicCodespace, trustedCodespace)
			require.Equal(t, publicCode, trustedCode)
			require.Equal(t, publicCtx.GasMeter().GasConsumed(), trustedCtx.GasMeter().GasConsumed())
			require.Equal(t, publicBank.fromAccountToModuleCalls, trustedBank.fromAccountToModuleCalls)
			require.Equal(t, publicBank.moduleBalances, trustedBank.moduleBalances)
			require.Equal(t, collectStoreEntries(t, publicKeeper.storeService, publicCtx), collectStoreEntries(t, trustedKeeper.storeService, trustedCtx))
			require.Equal(t, publicCtx.EventManager().Events(), trustedCtx.EventManager().Events())
		})
	}
}
