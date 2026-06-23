package transfer

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type DisclosureBuildInput struct {
	OutputCommitment []byte
	TransferDenom    string
	FromNote         privacytypes.Note
	RecipientNote    privacytypes.Note
}

type DisclosureData struct {
	PayloadJSON []byte
	CipherText  []byte
	Digest      []byte
	Payload     privacydisclosure.Payload
}

func BuildUserDisclosureData(
	input DisclosureBuildInput,
	userPrivacyPolicy uint32,
	userDisclosureMode privacytypes.UserDisclosureMode,
	userDisclosureTargetPubKey *crypto_tedwards.PointAffine,
) (*DisclosureData, error) {
	if userPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
		return nil, nil
	}

	commitmentHex := hex.EncodeToString(input.OutputCommitment)
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(input.RecipientNote.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient asset id: %w", err)
	}

	digest, err := privacytypes.ComputeTransferDisclosureDigestBytes(
		userPrivacyPolicy,
		privacytypes.TransferDisclosureRecipientOutputIndex,
		input.OutputCommitment,
		input.RecipientNote.Amount,
		input.RecipientNote.AssetID,
		input.FromNote.ReceiverSpendPubKeyX,
		input.FromNote.ReceiverSpendPubKeyY,
		input.FromNote.ReceiverViewPubKeyX,
		input.FromNote.ReceiverViewPubKeyY,
		input.RecipientNote.ReceiverSpendPubKeyX,
		input.RecipientNote.ReceiverSpendPubKeyY,
		input.RecipientNote.ReceiverViewPubKeyX,
		input.RecipientNote.ReceiverViewPubKeyY,
	)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)

	fromAddress, toAddress, err := disclosureAddresses(input)
	if err != nil {
		return nil, err
	}

	payload := privacydisclosure.Payload{
		Version:             privacydisclosure.PayloadVersion,
		Plane:               privacydisclosure.PlaneUser,
		Policy:              userPrivacyPolicy,
		OutputIndex:         privacytypes.TransferDisclosureRecipientOutputIndex,
		CommitmentHex:       commitmentHex,
		DisclosureDigestHex: digestHex,
	}

	if userPrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0 {
		payload.Amount = input.RecipientNote.Amount.String()
		payload.AssetIDHex = assetIDHex
		payload.AssetDenom = input.TransferDenom
	}
	if userPrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0 {
		payload.FromShieldedAddress = fromAddress
	}
	if userPrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0 {
		payload.ToShieldedAddress = toAddress
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal user disclosure payload: %w", err)
	}

	payloadBytes := payloadJSON
	switch userDisclosureMode {
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
		if userDisclosureTargetPubKey == nil {
			return nil, fmt.Errorf("recipient-encrypted disclosure requires a disclosure target public key")
		}
		payloadBytes, err = privacycrypto.AsymEncrypt(payloadJSON, *userDisclosureTargetPubKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt user disclosure payload: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported user disclosure mode %d", userDisclosureMode)
	}

	return &DisclosureData{
		PayloadJSON: payloadJSON,
		CipherText:  payloadBytes,
		Digest:      digest,
		Payload:     payload,
	}, nil
}

func BuildAuditDisclosureData(
	input DisclosureBuildInput,
	auditDisclosureTargetPubKey *crypto_tedwards.PointAffine,
) (*DisclosureData, error) {
	if auditDisclosureTargetPubKey == nil {
		return nil, fmt.Errorf("audit disclosure target public key is required")
	}

	commitmentHex := hex.EncodeToString(input.OutputCommitment)
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(input.RecipientNote.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient asset id: %w", err)
	}

	digest, err := privacytypes.ComputeAuditTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		input.OutputCommitment,
		input.RecipientNote.Amount,
		input.RecipientNote.AssetID,
		input.FromNote.ReceiverSpendPubKeyX,
		input.FromNote.ReceiverSpendPubKeyY,
		input.FromNote.ReceiverViewPubKeyX,
		input.FromNote.ReceiverViewPubKeyY,
		input.RecipientNote.ReceiverSpendPubKeyX,
		input.RecipientNote.ReceiverSpendPubKeyY,
		input.RecipientNote.ReceiverViewPubKeyX,
		input.RecipientNote.ReceiverViewPubKeyY,
	)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)

	fromAddress, toAddress, err := disclosureAddresses(input)
	if err != nil {
		return nil, err
	}

	payload := privacydisclosure.Payload{
		Version:             privacydisclosure.PayloadVersion,
		Plane:               privacydisclosure.PlaneAudit,
		Policy:              privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
		OutputIndex:         privacytypes.TransferDisclosureRecipientOutputIndex,
		CommitmentHex:       commitmentHex,
		DisclosureDigestHex: digestHex,
		Amount:              input.RecipientNote.Amount.String(),
		AssetIDHex:          assetIDHex,
		AssetDenom:          input.TransferDenom,
		FromShieldedAddress: fromAddress,
		ToShieldedAddress:   toAddress,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal audit disclosure payload: %w", err)
	}

	cipherText, err := privacycrypto.AsymEncrypt(payloadJSON, *auditDisclosureTargetPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt audit disclosure payload: %w", err)
	}

	return &DisclosureData{
		PayloadJSON: payloadJSON,
		CipherText:  cipherText,
		Digest:      digest,
		Payload:     payload,
	}, nil
}

func BuildSelfViewDisclosureData(
	input DisclosureBuildInput,
	selfViewDisclosureTargetPubKey *crypto_tedwards.PointAffine,
) (*DisclosureData, error) {
	if selfViewDisclosureTargetPubKey == nil {
		return nil, fmt.Errorf("self-view disclosure target public key is required")
	}

	commitmentHex := hex.EncodeToString(input.OutputCommitment)
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(input.RecipientNote.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient asset id: %w", err)
	}

	digest, err := privacytypes.ComputeSelfViewTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		input.OutputCommitment,
		input.RecipientNote.Amount,
		input.RecipientNote.AssetID,
		input.FromNote.ReceiverSpendPubKeyX,
		input.FromNote.ReceiverSpendPubKeyY,
		input.FromNote.ReceiverViewPubKeyX,
		input.FromNote.ReceiverViewPubKeyY,
		input.RecipientNote.ReceiverSpendPubKeyX,
		input.RecipientNote.ReceiverSpendPubKeyY,
		input.RecipientNote.ReceiverViewPubKeyX,
		input.RecipientNote.ReceiverViewPubKeyY,
	)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)

	fromAddress, toAddress, err := disclosureAddresses(input)
	if err != nil {
		return nil, err
	}

	payload := privacydisclosure.Payload{
		Version:             privacydisclosure.PayloadVersion,
		Plane:               privacydisclosure.PlaneSelfView,
		Policy:              privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
		OutputIndex:         privacytypes.TransferDisclosureRecipientOutputIndex,
		CommitmentHex:       commitmentHex,
		DisclosureDigestHex: digestHex,
		Amount:              input.RecipientNote.Amount.String(),
		AssetIDHex:          assetIDHex,
		AssetDenom:          input.TransferDenom,
		FromShieldedAddress: fromAddress,
		ToShieldedAddress:   toAddress,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal self-view disclosure payload: %w", err)
	}

	cipherText, err := privacycrypto.AsymEncrypt(payloadJSON, *selfViewDisclosureTargetPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt self-view disclosure payload: %w", err)
	}

	return &DisclosureData{
		PayloadJSON: payloadJSON,
		CipherText:  cipherText,
		Digest:      digest,
		Payload:     payload,
	}, nil
}

func disclosureAddresses(input DisclosureBuildInput) (string, string, error) {
	fromAddress, err := input.FromNote.ReceiverShieldedAddress()
	if err != nil {
		return "", "", fmt.Errorf("failed to encode sender shielded address: %w", err)
	}

	toAddress, err := input.RecipientNote.ReceiverShieldedAddress()
	if err != nil {
		return "", "", fmt.Errorf("failed to encode recipient shielded address: %w", err)
	}

	return fromAddress, toAddress, nil
}
