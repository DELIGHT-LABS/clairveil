package keeper

import (
	"fmt"
	"math/big"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

// DepositWithFunder executes the canonical Clairveil deposit transition while
// debiting funder. msg.Creator remains the actor and indexed-event creator.
//
// SECURITY: this method does not authenticate msg.Creator or authorize funder.
// The in-process caller must derive both from trusted application state and
// execute the call inside the caller's rollback boundary.
func (k Keeper) DepositWithFunder(
	ctx sdk.Context,
	msg *types.MsgDeposit,
	funder sdk.AccAddress,
) (*types.MsgDepositResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if _, err := sdk.AccAddressFromBech32(msg.Creator); err != nil {
		return nil, err
	}
	if err := sdk.VerifyAddressFormat(funder); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid deposit funder: %v", err)
	}
	if funder.Equals(authtypes.NewModuleAddress(types.ModuleName)) {
		return nil, errorsmod.Wrap(
			sdkerrors.ErrInvalidAddress,
			"deposit funder must not be the privacy module account",
		)
	}

	return k.depositWithValidatedFunder(ctx, msg, funder, true)
}

func (k Keeper) depositWithValidatedFunder(
	ctx sdk.Context,
	msg *types.MsgDeposit,
	funder sdk.AccAddress,
	verifyModuleBalanceDelta bool,
) (*types.MsgDepositResponse, error) {
	coin, err := sdk.ParseCoinNormalized(msg.Amount)
	if err != nil {
		return nil, err
	}
	if err := types.ValidateShieldedAmount("deposit amount", coin.Amount.BigInt()); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	canonicalCommitment, err := validateFieldElementBytesStrict(msg.NoteCommitment)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "note commitment must be canonical 32-byte field bytes")
	}
	commitmentExists, err := k.HasCommitment(ctx, canonicalCommitment)
	if err != nil {
		return nil, fmt.Errorf("check deposit commitment index: %w", err)
	}
	if commitmentExists {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "note commitment already exists")
	}

	if err := k.EnsureCanAppendCommitments(ctx, 1); err != nil {
		return nil, wrapMerkleAppendPreconditionErr(err, "not enough merkle tree capacity for deposit output")
	}

	var assignment circuit.DepositCircuit
	assignment.Commitment = new(big.Int).SetBytes(canonicalCommitment)
	assignment.Amount = new(big.Int).Set(coin.Amount.BigInt())
	assetID, err := k.RequireRegisteredAssetV1(ctx, coin.Denom)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "deposit asset registry validation failed: %v", err)
	}
	assignment.AssetID = new(big.Int).SetBytes(assetID)

	if err := verifyProofBN254(ctx, msg.Proof, &assignment, DepositProofVerificationGas, "privacy deposit proof verification", zk.GetDepositVerifyingKey); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "deposit proof verification failed; the proof, amount, asset, or commitment may not match: %v", err)
	}

	cacheCtx, writeCache := ctx.CacheContext()
	var moduleBalanceBefore sdk.Coin
	if verifyModuleBalanceDelta {
		moduleAddress := authtypes.NewModuleAddress(types.ModuleName)
		moduleBalanceBefore = k.bankKeeper.GetBalance(cacheCtx, moduleAddress, coin.Denom)
	}
	if err := k.bankKeeper.SendCoinsFromAccountToModule(cacheCtx, funder, types.ModuleName, sdk.NewCoins(coin)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock tokens")
	}
	if verifyModuleBalanceDelta {
		moduleAddress := authtypes.NewModuleAddress(types.ModuleName)
		moduleBalanceAfter := k.bankKeeper.GetBalance(cacheCtx, moduleAddress, coin.Denom)
		expectedModuleBalance := moduleBalanceBefore.Amount.Add(coin.Amount)
		if !moduleBalanceAfter.Amount.Equal(expectedModuleBalance) {
			return nil, errorsmod.Wrapf(
				sdkerrors.ErrLogic,
				"privacy module balance mismatch after deposit bank transfer: got %s, expected %s",
				moduleBalanceAfter.Amount,
				expectedModuleBalance,
			)
		}
	}
	if err := k.RecordReserveDeposit(cacheCtx, coin); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record privacy reserve deposit")
	}

	if err := k.AppendCommitment(cacheCtx, canonicalCommitment); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "failed to append the note commitment")
	}

	eventAttrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator),
		sdk.NewAttribute(types.AttributeKeyCommitment, fmt.Sprintf("%x", canonicalCommitment)),
		sdk.NewAttribute(types.AttributeKeyEncryptedNote, fmt.Sprintf("%x", msg.EncryptedNote)),
	}
	if err := k.emitIndexedPrivacyEvent(cacheCtx, types.EventTypeDeposit, eventAttrs); err != nil {
		return nil, errorsmod.Wrap(err, "failed to index deposit privacy event")
	}

	writeCache()
	return &types.MsgDepositResponse{}, nil
}
