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
	viewTags                    [][]byte
	userPrivacyPolicy           uint32
	userDisclosureDigest        []byte
	userDisclosureMode          types.UserDisclosureMode
	userDisclosureTargetPubKey  []byte
	userDisclosurePayload       []byte
	auditDisclosureDigest       []byte
	auditDisclosureTargetPubKey []byte
	auditDisclosurePayload      []byte
	selfViewDisclosureDigest    []byte
	selfViewDisclosurePayload   []byte
	expiresAtUnix               int64
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
	if k.HasCommitment(ctx, canonicalCommitment) {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "note commitment already exists")
	}

	if err := k.EnsureCanAppendCommitments(ctx, 1); err != nil {
		return nil, wrapMerkleAppendPreconditionErr(err, "not enough merkle tree capacity for deposit output")
	}

	var assignment circuit.DepositCircuit
	assignment.Commitment = new(big.Int).SetBytes(canonicalCommitment)
	assignment.Amount = new(big.Int).Set(coin.Amount.BigInt())
	assignment.AssetID = privacycrypto.HashString(coin.Denom)

	if err := verifyProofBN254(ctx, msg.Proof, &assignment, DepositProofVerificationGas, "privacy deposit proof verification", zk.GetDepositVerifyingKey); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "deposit proof verification failed; the proof, amount, asset, or commitment may not match: %v", err)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, depositor, types.ModuleName, sdk.NewCoins(coin)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock tokens")
	}
	if err := k.RecordReserveDeposit(ctx, coin); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record privacy reserve deposit")
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

	if ctx.BlockTime().Unix() >= msg.ExpiresAtUnix {
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
	var assignment circuit.SpendCircuit

	assignment.MerkleRoot = new(big.Int).SetBytes(canonicalRoot)
	assignment.Nullifier = new(big.Int).SetBytes(canonicalNullifier)
	chainDomain, err := types.ComputeChainDomainV1(ctx.ChainID(), types.ActiveCircuitSetID)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to compute withdraw chain domain: %v", err)
	}
	recipientDigest, err := types.ComputeWithdrawRecipientDigestV1(recipientAddr.Bytes())
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to compute withdraw recipient digest: %v", err)
	}
	assignment.ChainDomainHi = chainDomain.Hi
	assignment.ChainDomainLo = chainDomain.Lo
	assignment.ExpiresAtUnix = big.NewInt(msg.ExpiresAtUnix)
	assignment.RecipientDigestHi = recipientDigest.Hi
	assignment.RecipientDigestLo = recipientDigest.Lo

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

	if err := verifyProofBN254(ctx, msg.Proof, &assignment, SpendProofVerificationGas, "privacy spend proof verification", zk.GetSpendVerifyingKey); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "spend proof verification failed; the proof, recipient, amount, or asset may not match: %v", err)
	}

	k.SetNullifier(ctx, canonicalNullifier)
	k.Logger(ctx).Info("nullifier marked as used", "nullifier", fmt.Sprintf("%x", canonicalNullifier))

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, sdk.NewCoins(coin)); err != nil {
		return nil, err
	}
	if err := k.RecordReserveWithdraw(ctx, coin); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record privacy reserve withdraw")
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
		viewTags:                    msg.ViewTags,
		userPrivacyPolicy:           msg.UserPrivacyPolicy,
		userDisclosureDigest:        msg.UserDisclosureDigest,
		userDisclosureMode:          msg.UserDisclosureMode,
		userDisclosureTargetPubKey:  msg.UserDisclosureTargetPubkey,
		userDisclosurePayload:       msg.UserDisclosurePayload,
		auditDisclosureDigest:       msg.AuditDisclosureDigest,
		auditDisclosureTargetPubKey: msg.AuditDisclosureTargetPubkey,
		auditDisclosurePayload:      msg.AuditDisclosurePayload,
		selfViewDisclosureDigest:    msg.SelfViewDisclosureDigest,
		selfViewDisclosurePayload:   msg.SelfViewDisclosurePayload,
		expiresAtUnix:               msg.ExpiresAtUnix,
	}); err != nil {
		return nil, err
	}

	return &types.MsgTransferResponse{}, nil
}

func (k msgServer) executeShieldedTransfer(ctx sdk.Context, req shieldedTransferRequest) error {
	if req.expiresAtUnix <= 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "expires_at_unix must be positive for transfer")
	}
	if ctx.BlockTime().Unix() >= req.expiresAtUnix {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "transfer payload has expired")
	}
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
	if len(req.viewTags) != 2 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 view tags; got %d", len(req.viewTags))
	}
	if err := types.ValidateDistinctCanonicalFieldElements("nullifier", req.nullifiers); err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	if err := types.ValidateDistinctCanonicalFieldElements("commitment", req.newCommitments); err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}
	for i, viewTag := range req.viewTags {
		if len(viewTag) != types.ViewTagLength {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "view tag %d must be exactly %d bytes", i, types.ViewTagLength)
		}
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
		if k.HasCommitment(ctx, canonicalCommitment) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "commitment %d already exists", i)
		}
	}

	if err := k.EnsureCanAppendCommitments(ctx, uint64(len(canonicalCommitments))); err != nil {
		return wrapMerkleAppendPreconditionErr(err, "not enough merkle tree capacity for transfer outputs")
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
	assignment.FullDisclosureDigest = new(big.Int).SetBytes(req.auditDisclosureDigest)
	chainDomain, err := types.ComputeChainDomainV1(ctx.ChainID(), types.ActiveCircuitSetID)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to compute transfer chain domain: %v", err)
	}
	assignment.ChainDomainHi = chainDomain.Hi
	assignment.ChainDomainLo = chainDomain.Lo
	assignment.ExpiresAtUnix = big.NewInt(req.expiresAtUnix)
	effectMessage := &types.MsgTransfer{
		Root:                        canonicalRoot,
		Nullifiers:                  canonicalNullifiers,
		NewCommitments:              canonicalCommitments,
		CipherTexts:                 req.cipherTexts,
		ViewTags:                    req.viewTags,
		UserPrivacyPolicy:           req.userPrivacyPolicy,
		UserDisclosureDigest:        req.userDisclosureDigest,
		UserDisclosureMode:          req.userDisclosureMode,
		UserDisclosureTargetPubkey:  req.userDisclosureTargetPubKey,
		UserDisclosurePayload:       req.userDisclosurePayload,
		AuditDisclosureDigest:       req.auditDisclosureDigest,
		AuditDisclosureTargetPubkey: req.auditDisclosureTargetPubKey,
		AuditDisclosurePayload:      req.auditDisclosurePayload,
		SelfViewDisclosureDigest:    req.selfViewDisclosureDigest,
		SelfViewDisclosurePayload:   req.selfViewDisclosurePayload,
		ExpiresAtUnix:               req.expiresAtUnix,
	}
	payloadDigest, err := types.ComputeTransferPayloadDigestV1(effectMessage)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to compute canonical transfer payload digest: %v", err)
	}
	assignment.PayloadDigestHi = payloadDigest.Hi
	assignment.PayloadDigestLo = payloadDigest.Lo

	if err := verifyProofBN254(ctx, req.proof, &assignment, JoinSplitProofVerificationGas, "privacy joinsplit proof verification", zk.GetJoinSplitVerifyingKey); err != nil {
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
		sdk.NewAttribute(types.AttributeKeyViewTag1, fmt.Sprintf("%x", req.viewTags[0])),
		sdk.NewAttribute(types.AttributeKeyViewTag2, fmt.Sprintf("%x", req.viewTags[1])),
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
	if len(req.selfViewDisclosureDigest) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeySelfViewDisclosureDigest, fmt.Sprintf("%x", req.selfViewDisclosureDigest)))
	}
	if len(req.selfViewDisclosurePayload) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeySelfViewDisclosurePayload, fmt.Sprintf("%x", req.selfViewDisclosurePayload)))
	}

	if err := k.emitIndexedPrivacyEvent(ctx, types.EventTypeShieldedTransfer, eventAttrs); err != nil {
		return errorsmod.Wrap(err, "failed to index transfer privacy event")
	}

	return nil
}
