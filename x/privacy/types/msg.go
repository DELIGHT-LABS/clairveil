package types

import (
	"strings"

	errorsmod "cosmossdk.io/errors"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ sdk.Msg = &MsgDeposit{}
var _ sdk.Msg = &MsgWithdraw{}
var _ sdk.Msg = &MsgTransfer{}

const expectedJoinSplitElements = 2
const expectedFieldElementBytes = 32

const (
	TransferPrivacyPolicyAllPrivate           uint32 = 0
	TransferPrivacyPolicyDiscloseAmount       uint32 = 1
	TransferPrivacyPolicyDiscloseTo           uint32 = 2
	TransferPrivacyPolicyDiscloseFrom         uint32 = 4
	TransferPrivacyPolicyDiscloseAmountTo     uint32 = TransferPrivacyPolicyDiscloseAmount | TransferPrivacyPolicyDiscloseTo
	TransferPrivacyPolicyDiscloseAmountFrom   uint32 = TransferPrivacyPolicyDiscloseAmount | TransferPrivacyPolicyDiscloseFrom
	TransferPrivacyPolicyDiscloseToFrom       uint32 = TransferPrivacyPolicyDiscloseTo | TransferPrivacyPolicyDiscloseFrom
	TransferPrivacyPolicyDiscloseAmountToFrom uint32 = TransferPrivacyPolicyDiscloseAmount | TransferPrivacyPolicyDiscloseTo | TransferPrivacyPolicyDiscloseFrom
)

func validateTransferDisclosurePolicy(policy uint32) error {
	switch policy {
	case TransferPrivacyPolicyAllPrivate,
		TransferPrivacyPolicyDiscloseAmount,
		TransferPrivacyPolicyDiscloseTo,
		TransferPrivacyPolicyDiscloseAmountTo,
		TransferPrivacyPolicyDiscloseFrom,
		TransferPrivacyPolicyDiscloseAmountFrom,
		TransferPrivacyPolicyDiscloseToFrom,
		TransferPrivacyPolicyDiscloseAmountToFrom:
		return nil
	default:
		return errorsmod.Wrapf(
			sdkerrors.ErrInvalidRequest,
			"unsupported transfer privacy policy %d (supported: 0=all-private, 1=amount, 2=to, 3=amount-to, 4=from, 5=amount-from, 6=to-from, 7=amount-to-from)",
			policy,
		)
	}
}

func validateCreatorAddress(creator string) error {
	_, err := sdk.AccAddressFromBech32(creator)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	return nil
}

func validateFieldElementBytesStrict(name string, bz []byte) error {
	if len(bz) != expectedFieldElementBytes {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s must be exactly %d bytes", name, expectedFieldElementBytes)
	}

	var elem fr.Element
	if err := elem.SetBytesCanonical(bz); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s must be canonical 32-byte field bytes", name)
	}

	return nil
}

func validateTransferPayload(root []byte, nullifiers, newCommitments, cipherTexts [][]byte) error {
	if len(nullifiers) != expectedJoinSplitElements {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 nullifiers; got %d", len(nullifiers))
	}
	if len(newCommitments) != expectedJoinSplitElements {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 commitments; got %d", len(newCommitments))
	}
	if len(cipherTexts) != expectedJoinSplitElements {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "transfer requires exactly 2 ciphertexts; got %d", len(cipherTexts))
	}

	if err := validateFieldElementBytesStrict("root", root); err != nil {
		return err
	}

	for i, nullifier := range nullifiers {
		if err := validateFieldElementBytesStrict("nullifier", nullifier); err != nil {
			return errorsmod.Wrapf(err, "nullifier index %d", i)
		}
	}

	for i, commitment := range newCommitments {
		if err := validateFieldElementBytesStrict("commitment", commitment); err != nil {
			return errorsmod.Wrapf(err, "commitment index %d", i)
		}
	}

	return nil
}

func validateDisclosureTargetPubKey(name string, bz []byte) error {
	if len(bz) == 0 {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s is required when disclosure is enabled", name)
	}
	if _, err := decodePublicKey(bz); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s is invalid: %v", name, err)
	}
	return nil
}

func validateUserDisclosure(
	policy uint32,
	digest []byte,
	mode UserDisclosureMode,
	targetPubKey []byte,
	payload []byte,
) error {
	if err := validateTransferDisclosurePolicy(policy); err != nil {
		return err
	}

	if policy == TransferPrivacyPolicyAllPrivate {
		if len(digest) != 0 || len(targetPubKey) != 0 || len(payload) != 0 {
			return errorsmod.Wrap(
				sdkerrors.ErrInvalidRequest,
				"all-private transfers must not include user disclosure digest, target pubkey, or payload",
			)
		}
		if mode != UserDisclosureMode_USER_DISCLOSURE_MODE_NONE {
			return errorsmod.Wrap(
				sdkerrors.ErrInvalidRequest,
				"all-private transfers must use user disclosure mode none",
			)
		}
		return nil
	}

	if err := validateFieldElementBytesStrict("user disclosure digest", digest); err != nil {
		return err
	}
	if len(payload) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "user disclosure payload is required when the transfer is not all-private")
	}

	switch mode {
	case UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
		if len(targetPubKey) != 0 {
			return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "public user disclosure must not include a target pubkey")
		}
	case UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
		if err := validateDisclosureTargetPubKey("user disclosure target pubkey", targetPubKey); err != nil {
			return err
		}
	case UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "user disclosure mode none is only valid for all-private transfers")
	default:
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "unsupported user disclosure mode %d", mode)
	}

	return nil
}

func validateAuditDisclosure(digest, targetPubKey, payload []byte) error {
	if err := validateFieldElementBytesStrict("audit disclosure digest", digest); err != nil {
		return err
	}
	if err := validateDisclosureTargetPubKey("audit disclosure target pubkey", targetPubKey); err != nil {
		return err
	}
	if len(payload) == 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "audit disclosure payload is required for transfer validation")
	}
	return nil
}

func NewMsgDeposit(creator string, amount string, commitment []byte, encryptedNote []byte) *MsgDeposit {
	return &MsgDeposit{
		Creator:        creator,
		Amount:         amount,
		NoteCommitment: commitment,
		EncryptedNote:  encryptedNote,
	}
}

func (msg *MsgDeposit) Route() string {
	return RouterKey
}

func (msg *MsgDeposit) Type() string {
	return "Deposit"
}

func (msg *MsgDeposit) ValidateBasic() error {
	if err := validateCreatorAddress(msg.Creator); err != nil {
		return err
	}

	if err := validateFieldElementBytesStrict("note commitment", msg.NoteCommitment); err != nil {
		return err
	}

	return nil
}

func NewMsgWithdraw(creator string, proof, root, nullifier, newCommitment, encNote []byte, amount, recipient, chainID string, expiresAtUnix int64) *MsgWithdraw {
	return &MsgWithdraw{
		Creator:           creator,
		Proof:             proof,
		Root:              root,
		Nullifier:         nullifier,
		NewNoteCommitment: newCommitment,
		EncryptedNote:     encNote,
		Amount:            amount,
		Recipient:         recipient,
		ChainId:           chainID,
		ExpiresAtUnix:     expiresAtUnix,
	}
}

func (msg *MsgWithdraw) Route() string {
	return RouterKey
}

func (msg *MsgWithdraw) Type() string {
	return "Withdraw"
}

func (msg *MsgWithdraw) ValidateBasic() error {
	if err := validateCreatorAddress(msg.Creator); err != nil {
		return err
	}

	if err := validateFieldElementBytesStrict("root", msg.Root); err != nil {
		return err
	}

	if err := validateFieldElementBytesStrict("nullifier", msg.Nullifier); err != nil {
		return err
	}

	if err := validateFieldElementBytesStrict("new note commitment", msg.NewNoteCommitment); err != nil {
		return err
	}

	if msg.ChainId == "" {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "chain id is required for withdraw")
	}

	if msg.ExpiresAtUnix <= 0 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "expires_at_unix must be positive for withdraw")
	}

	return nil
}

// NewMsgTransfer constructs a transfer without disclosure metadata.
func NewMsgTransfer(
	creator string,
	proof, root []byte,
	nullifiers [][]byte,
	newCommitments [][]byte,
	cipherTexts [][]byte,
) *MsgTransfer {
	return &MsgTransfer{
		Creator:        creator,
		Proof:          proof,
		Root:           root,
		Nullifiers:     nullifiers,
		NewCommitments: newCommitments,
		CipherTexts:    cipherTexts,
	}
}

// NewMsgTransferWithDisclosure constructs a transfer with user and audit disclosure metadata.
func NewMsgTransferWithDisclosure(
	creator string,
	proof, root []byte,
	nullifiers [][]byte,
	newCommitments [][]byte,
	cipherTexts [][]byte,
	userPrivacyPolicy uint32,
	userDisclosureDigest []byte,
	userDisclosureMode UserDisclosureMode,
	userDisclosureTargetPubKey []byte,
	userDisclosurePayload []byte,
	auditDisclosureDigest []byte,
	auditDisclosureTargetPubKey []byte,
	auditDisclosurePayload []byte,
) *MsgTransfer {
	return &MsgTransfer{
		Creator:                     creator,
		Proof:                       proof,
		Root:                        root,
		Nullifiers:                  nullifiers,
		NewCommitments:              newCommitments,
		CipherTexts:                 cipherTexts,
		UserPrivacyPolicy:           userPrivacyPolicy,
		UserDisclosureDigest:        userDisclosureDigest,
		UserDisclosureMode:          userDisclosureMode,
		UserDisclosureTargetPubkey:  userDisclosureTargetPubKey,
		UserDisclosurePayload:       userDisclosurePayload,
		AuditDisclosureDigest:       auditDisclosureDigest,
		AuditDisclosureTargetPubkey: auditDisclosureTargetPubKey,
		AuditDisclosurePayload:      auditDisclosurePayload,
	}
}

func (msg *MsgTransfer) ValidateBasic() error {
	if err := validateCreatorAddress(msg.Creator); err != nil {
		return err
	}

	if err := validateTransferPayload(msg.Root, msg.Nullifiers, msg.NewCommitments, msg.CipherTexts); err != nil {
		return err
	}

	if err := validateUserDisclosure(
		msg.UserPrivacyPolicy,
		msg.UserDisclosureDigest,
		msg.UserDisclosureMode,
		msg.UserDisclosureTargetPubkey,
		msg.UserDisclosurePayload,
	); err != nil {
		return err
	}

	if err := validateAuditDisclosure(
		msg.AuditDisclosureDigest,
		msg.AuditDisclosureTargetPubkey,
		msg.AuditDisclosurePayload,
	); err != nil {
		return err
	}

	return nil
}

func disclosureModeLabel(mode UserDisclosureMode) string {
	switch mode {
	case UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
		return "none"
	case UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
		return "public"
	case UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
		return "recipient-encrypted"
	default:
		return strings.ToLower(mode.String())
	}
}
