package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark/backend/groth16"
)

type msgServer struct {
	Keeper
}

type shieldedTransferRequest struct {
	relayer                     string
	proof                       []byte
	root                        []byte
	nullifiers                  [][]byte
	newCommitments              [][]byte
	cipherTexts                 [][]byte
	userPrivacyPolicy           uint32
	userDisclosureDigest        []byte
	userDisclosureMode          types.UserDisclosureMode
	userDisclosureTargetPubKey  []byte
	userDisclosurePayload       []byte
	auditDisclosureDigest       []byte
	auditDisclosureTargetPubKey []byte
	auditDisclosurePayload      []byte
}

// NewMsgServerImpl returns an implementation of the MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func wrapMerkleAppendPreconditionErr(err error, capacityMessage string) error {
	if errors.Is(err, errMerkleTreeOverflow) || errors.Is(err, errMerkleTreeRebuildTooLarge) {
		return errorsmod.Wrap(sdkerrors.ErrLogic, err.Error())
	}
	if errors.Is(err, errMerkleTreeCapacity) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, capacityMessage)
	}
	return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
}

// Deposit locks transparent funds and appends the encrypted note commitment.
func (k msgServer) Deposit(goCtx context.Context, msg *types.MsgDeposit) (*types.MsgDepositResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	depositor, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, err
	}

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

	if err := k.EnsureCanAppendCommitments(ctx, 1); err != nil {
		return nil, wrapMerkleAppendPreconditionErr(err, "not enough merkle tree capacity for deposit output")
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, depositor, types.ModuleName, sdk.NewCoins(coin)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock tokens")
	}

	if err := k.AppendCommitment(ctx, canonicalCommitment); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "failed to append the note commitment")
	}

	eventAttrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator),
		sdk.NewAttribute(types.AttributeKeyCommitment, fmt.Sprintf("%x", canonicalCommitment)),
		sdk.NewAttribute(types.AttributeKeyEncryptedNote, fmt.Sprintf("%x", msg.EncryptedNote)),
	}
	if err := k.emitIndexedPrivacyEvent(ctx, types.EventTypeDeposit, eventAttrs); err != nil {
		return nil, errorsmod.Wrap(err, "failed to index deposit privacy event")
	}

	return &types.MsgDepositResponse{}, nil
}

// Withdraw verifies a spend proof and releases transparent funds.
func (k msgServer) Withdraw(goCtx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	canonicalRoot, err := validateFieldElementBytesStrict(msg.Root)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw root must be canonical 32-byte field bytes")
	}

	canonicalNullifier, err := validateFieldElementBytesStrict(msg.Nullifier)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw nullifier must be canonical 32-byte field bytes")
	}

	if msg.ChainId != ctx.ChainID() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "withdraw chain id mismatch: expected %s, got %s", ctx.ChainID(), msg.ChainId)
	}

	if ctx.BlockTime().Unix() > msg.ExpiresAtUnix {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw payload has expired")
	}

	if !k.CheckHistoricalRoot(ctx, canonicalRoot) {
		k.Logger(ctx).Error("on-chain root mismatch", "root", fmt.Sprintf("%x", canonicalRoot))
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw root was not found in the historical merkle roots")
	}

	if k.HasNullifier(ctx, canonicalNullifier) {
		k.Logger(ctx).Error("double spend detected", "nullifier", fmt.Sprintf("%x", canonicalNullifier))
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw nullifier was already used (double spend)")
	}

	recipientAddr, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "recipient address %q is invalid", msg.Recipient)
	}
	recipientInt := new(big.Int).SetBytes(recipientAddr.Bytes())

	var assignment circuit.SpendCircuit

	assignment.MerkleRoot = new(big.Int).SetBytes(canonicalRoot)
	assignment.Nullifier = new(big.Int).SetBytes(canonicalNullifier)
	assignment.Recipient = recipientInt

	coin, err := sdk.ParseCoinNormalized(msg.Amount)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw amount string is invalid")
	}
	if !coin.Amount.IsPositive() {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "withdraw amount must be positive")
	}
	if err := types.ValidateShieldedAmount("withdraw amount", coin.Amount.BigInt()); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	amountVal := new(big.Int).Set(coin.Amount.BigInt())
	assignment.Amount = amountVal
	assignment.AssetID = privacycrypto.HashString(coin.Denom)

	publicWitness, err := newPublicWitnessBN254(&assignment)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to create the spend public witness: %v", err)
	}

	proof, err := readProofBN254(msg.Proof)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "failed to decode the spend proof")
	}

	spendVK, err := zk.GetSpendVerifyingKey()
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to load the spend verifying key: %v", err)
	}

	if err := groth16.Verify(proof, spendVK, publicWitness); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "spend proof verification failed; the proof, recipient, amount, or asset may not match: %v", err)
	}

	k.SetNullifier(ctx, canonicalNullifier)
	k.Logger(ctx).Info("nullifier marked as used", "nullifier", fmt.Sprintf("%x", canonicalNullifier))

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, sdk.NewCoins(coin)); err != nil {
		return nil, err
	}

	eventAttrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyNullifier, fmt.Sprintf("%x", canonicalNullifier)),
		sdk.NewAttribute(types.AttributeKeyRelayer, msg.Creator),
		sdk.NewAttribute("recipient", msg.Recipient),
	}
	if err := k.emitIndexedPrivacyEvent(ctx, types.EventTypeWithdraw, eventAttrs); err != nil {
		return nil, errorsmod.Wrap(err, "failed to index withdraw privacy event")
	}

	return &types.MsgWithdrawResponse{}, nil
}

func (k msgServer) Transfer(goCtx context.Context, msg *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := k.executeShieldedTransfer(ctx, shieldedTransferRequest{
		relayer:                     msg.Creator,
		proof:                       msg.Proof,
		root:                        msg.Root,
		nullifiers:                  msg.Nullifiers,
		newCommitments:              msg.NewCommitments,
		cipherTexts:                 msg.CipherTexts,
		userPrivacyPolicy:           msg.UserPrivacyPolicy,
		userDisclosureDigest:        msg.UserDisclosureDigest,
		userDisclosureMode:          msg.UserDisclosureMode,
		userDisclosureTargetPubKey:  msg.UserDisclosureTargetPubkey,
		userDisclosurePayload:       msg.UserDisclosurePayload,
		auditDisclosureDigest:       msg.AuditDisclosureDigest,
		auditDisclosureTargetPubKey: msg.AuditDisclosureTargetPubkey,
		auditDisclosurePayload:      msg.AuditDisclosurePayload,
	}); err != nil {
		return nil, err
	}

	return &types.MsgTransferResponse{}, nil
}

func (k msgServer) executeShieldedTransfer(ctx sdk.Context, req shieldedTransferRequest) error {
	canonicalRoot, err := validateFieldElementBytesStrict(req.root)
	if err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "transfer root must be canonical 32-byte field bytes")
	}

	if !k.CheckHistoricalRoot(ctx, canonicalRoot) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "transfer root was not found in the historical merkle roots")
	}

	if len(req.nullifiers) != 2 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 nullifiers; got %d", len(req.nullifiers))
	}
	if len(req.newCommitments) != 2 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 commitments; got %d", len(req.newCommitments))
	}
	if len(req.cipherTexts) != 2 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 ciphertexts; got %d", len(req.cipherTexts))
	}

	expectedAuditTargetPubKey := k.GetAuditMasterPubkey(ctx)
	if len(expectedAuditTargetPubKey) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "chain audit master pubkey is not configured")
	}
	if len(req.auditDisclosureTargetPubKey) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "audit disclosure target pubkey is required for transfer validation")
	}
	if !bytes.Equal(expectedAuditTargetPubKey, req.auditDisclosureTargetPubKey) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "audit disclosure target pubkey does not match the chain audit configuration")
	}

	canonicalNullifiers := make([][]byte, len(req.nullifiers))
	for i, nullifier := range req.nullifiers {
		canonicalNullifier, err := validateFieldElementBytesStrict(nullifier)
		if err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "nullifier %d must be canonical 32-byte field bytes", i)
		}
		canonicalNullifiers[i] = canonicalNullifier

		if k.HasNullifier(ctx, canonicalNullifier) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "nullifier %d was already used", i)
		}
	}

	canonicalCommitments := make([][]byte, len(req.newCommitments))
	for i, commitment := range req.newCommitments {
		canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
		if err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "commitment %d must be canonical 32-byte field bytes", i)
		}
		canonicalCommitments[i] = canonicalCommitment
	}

	if err := k.EnsureCanAppendCommitments(ctx, uint64(len(canonicalCommitments))); err != nil {
		return wrapMerkleAppendPreconditionErr(err, "not enough merkle tree capacity for transfer outputs")
	}

	proof, err := readProofBN254(req.proof)
	if err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "failed to decode the joinsplit proof")
	}

	var assignment circuit.JoinSplitCircuit
	assignment.MerkleRoot = new(big.Int).SetBytes(canonicalRoot)
	assignment.Nullifiers[0] = new(big.Int).SetBytes(canonicalNullifiers[0])
	assignment.Nullifiers[1] = new(big.Int).SetBytes(canonicalNullifiers[1])
	assignment.Commitments[0] = new(big.Int).SetBytes(canonicalCommitments[0])
	assignment.Commitments[1] = new(big.Int).SetBytes(canonicalCommitments[1])
	assignment.UserPrivacyPolicy = big.NewInt(int64(req.userPrivacyPolicy))
	if len(req.userDisclosureDigest) > 0 {
		assignment.UserDisclosureDigest = new(big.Int).SetBytes(req.userDisclosureDigest)
	} else {
		assignment.UserDisclosureDigest = big.NewInt(0)
	}
	assignment.AuditDisclosureDigest = new(big.Int).SetBytes(req.auditDisclosureDigest)

	publicWitness, err := newPublicWitnessBN254(&assignment)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to create the joinsplit public witness: %v", err)
	}

	verifyingKey, err := zk.GetJoinSplitVerifyingKey()
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to load the joinsplit verifying key: %v", err)
	}

	if err := groth16.Verify(proof, verifyingKey, publicWitness); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "joinsplit proof verification failed: %v", err)
	}

	for _, nullifier := range canonicalNullifiers {
		k.SetNullifier(ctx, nullifier)
	}

	for _, commitment := range canonicalCommitments {
		if err := k.AppendCommitment(ctx, commitment); err != nil {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "failed to append a new commitment")
		}
	}

	eventAttrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyRelayer, req.relayer),
		sdk.NewAttribute(types.AttributeKeyNullifier1, fmt.Sprintf("%x", canonicalNullifiers[0])),
		sdk.NewAttribute(types.AttributeKeyNullifier2, fmt.Sprintf("%x", canonicalNullifiers[1])),
		sdk.NewAttribute(types.AttributeKeyCommitment1, fmt.Sprintf("%x", canonicalCommitments[0])),
		sdk.NewAttribute(types.AttributeKeyCommitment2, fmt.Sprintf("%x", canonicalCommitments[1])),
		sdk.NewAttribute(types.AttributeKeyCipherText1, fmt.Sprintf("%x", req.cipherTexts[0])),
		sdk.NewAttribute(types.AttributeKeyCipherText2, fmt.Sprintf("%x", req.cipherTexts[1])),
		sdk.NewAttribute(types.AttributeKeyUserPrivacyPolicy, strconv.FormatUint(uint64(req.userPrivacyPolicy), 10)),
		sdk.NewAttribute(types.AttributeKeyUserDisclosureMode, req.userDisclosureMode.String()),
	}
	if len(req.userDisclosureDigest) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyUserDisclosureDigest, fmt.Sprintf("%x", req.userDisclosureDigest)))
	}
	if len(req.userDisclosureTargetPubKey) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyUserDisclosureTargetPubKey, fmt.Sprintf("%x", req.userDisclosureTargetPubKey)))
	}
	if len(req.userDisclosurePayload) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyUserDisclosurePayload, fmt.Sprintf("%x", req.userDisclosurePayload)))
	}
	if len(req.auditDisclosureDigest) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyAuditDisclosureDigest, fmt.Sprintf("%x", req.auditDisclosureDigest)))
	}
	if len(req.auditDisclosureTargetPubKey) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyAuditDisclosureTargetPubKey, fmt.Sprintf("%x", req.auditDisclosureTargetPubKey)))
	}
	if len(req.auditDisclosurePayload) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyAuditDisclosurePayload, fmt.Sprintf("%x", req.auditDisclosurePayload)))
	}

	if err := k.emitIndexedPrivacyEvent(ctx, types.EventTypeShieldedTransfer, eventAttrs); err != nil {
		return errorsmod.Wrap(err, "failed to index transfer privacy event")
	}

	return nil
}
