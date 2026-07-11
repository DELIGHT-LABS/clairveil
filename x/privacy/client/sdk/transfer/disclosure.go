package transfer

import (
	"encoding/hex"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type DisclosureBuildInput struct {
	OutputCommitment       []byte
	TransferDenom          string
	FromNote               privacytypes.Note
	RecipientNote          privacytypes.Note
	UserDisclosureBlinding *big.Int
	FullDisclosureBlinding *big.Int
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
		input.UserDisclosureBlinding,
	)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)
	blindingHex, err := privacyfield.CanonicalHexFromBigInt(input.UserDisclosureBlinding)
	if err != nil {
		return nil, fmt.Errorf("invalid user disclosure blinding: %w", err)
	}

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
		BlindingHex:         blindingHex,
		AssetIDHex:          assetIDHex,
	}

	if userPrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0 {
		payload.Amount = input.RecipientNote.Amount.String()
	}
	if userPrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0 {
		payload.FromShieldedAddress = fromAddress
	}
	if userPrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0 {
		payload.ToShieldedAddress = toAddress
	}

	payloadPlaintext, err := marshalTransferDisclosurePlaintextV1(input, privacytypes.DisclosurePlaneUserV1, userPrivacyPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal user DisclosurePlaintextV1: %w", err)
	}

	payloadBytes := payloadPlaintext
	switch userDisclosureMode {
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
		if userDisclosureTargetPubKey == nil {
			return nil, fmt.Errorf("recipient-encrypted disclosure requires a disclosure target public key")
		}
		rawCipherText, err := privacycrypto.AsymEncrypt(payloadPlaintext, *userDisclosureTargetPubKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt user disclosure payload: %w", err)
		}
		payloadBytes, err = privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeUserDisclosureV1, rawCipherText)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported user disclosure mode %d", userDisclosureMode)
	}

	return &DisclosureData{
		PayloadJSON: payloadPlaintext,
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
		input.FullDisclosureBlinding,
	)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)
	blindingHex, err := privacyfield.CanonicalHexFromBigInt(input.FullDisclosureBlinding)
	if err != nil {
		return nil, fmt.Errorf("invalid full disclosure blinding: %w", err)
	}

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
		BlindingHex:         blindingHex,
		Amount:              input.RecipientNote.Amount.String(),
		AssetIDHex:          assetIDHex,
		FromShieldedAddress: fromAddress,
		ToShieldedAddress:   toAddress,
	}

	payloadPlaintext, err := marshalTransferDisclosurePlaintextV1(input, privacytypes.DisclosurePlaneFullV1, privacytypes.DisclosureFullMarkerV1)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal audit DisclosurePlaintextV1: %w", err)
	}

	rawCipherText, err := privacycrypto.AsymEncrypt(payloadPlaintext, *auditDisclosureTargetPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt audit disclosure payload: %w", err)
	}
	cipherText, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeAuditDisclosureV1, rawCipherText)
	if err != nil {
		return nil, err
	}

	return &DisclosureData{
		PayloadJSON: payloadPlaintext,
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
		input.FullDisclosureBlinding,
	)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)
	blindingHex, err := privacyfield.CanonicalHexFromBigInt(input.FullDisclosureBlinding)
	if err != nil {
		return nil, fmt.Errorf("invalid full disclosure blinding: %w", err)
	}

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
		BlindingHex:         blindingHex,
		Amount:              input.RecipientNote.Amount.String(),
		AssetIDHex:          assetIDHex,
		FromShieldedAddress: fromAddress,
		ToShieldedAddress:   toAddress,
	}

	payloadPlaintext, err := marshalTransferDisclosurePlaintextV1(input, privacytypes.DisclosurePlaneFullV1, privacytypes.DisclosureFullMarkerV1)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal self-view DisclosurePlaintextV1: %w", err)
	}

	rawCipherText, err := privacycrypto.AsymEncrypt(payloadPlaintext, *selfViewDisclosureTargetPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt self-view disclosure payload: %w", err)
	}
	cipherText, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeSelfViewDisclosureV1, rawCipherText)
	if err != nil {
		return nil, err
	}

	return &DisclosureData{
		PayloadJSON: payloadPlaintext,
		CipherText:  cipherText,
		Digest:      digest,
		Payload:     payload,
	}, nil
}

func marshalTransferDisclosurePlaintextV1(
	input DisclosureBuildInput,
	plane privacytypes.DisclosurePlaneV1,
	policy uint32,
) ([]byte, error) {
	payload := &privacytypes.DisclosurePlaintextV1{
		Plane:                plane,
		OutputIndex:          privacytypes.TransferDisclosureRecipientOutputIndex,
		Policy:               policy,
		DisclosedFieldBitmap: policy,
		Commitment:           new(big.Int).SetBytes(input.OutputCommitment),
		Amount:               new(big.Int),
		AssetID:              new(big.Int).Set(input.RecipientNote.AssetID),
		SenderSpendKeyX:      new(big.Int),
		SenderSpendKeyY:      new(big.Int),
		SenderViewKeyX:       new(big.Int),
		SenderViewKeyY:       new(big.Int),
		RecipientSpendKeyX:   new(big.Int),
		RecipientSpendKeyY:   new(big.Int),
		RecipientViewKeyX:    new(big.Int),
		RecipientViewKeyY:    new(big.Int),
	}

	if plane == privacytypes.DisclosurePlaneFullV1 {
		payload.DisclosedFieldBitmap = privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom
		payload.Amount = new(big.Int).Set(input.RecipientNote.Amount)
		payload.AssetID = new(big.Int).Set(input.RecipientNote.AssetID)
		payload.SenderSpendKeyX = new(big.Int).Set(input.FromNote.ReceiverSpendPubKeyX)
		payload.SenderSpendKeyY = new(big.Int).Set(input.FromNote.ReceiverSpendPubKeyY)
		payload.SenderViewKeyX = new(big.Int).Set(input.FromNote.ReceiverViewPubKeyX)
		payload.SenderViewKeyY = new(big.Int).Set(input.FromNote.ReceiverViewPubKeyY)
		payload.RecipientSpendKeyX = new(big.Int).Set(input.RecipientNote.ReceiverSpendPubKeyX)
		payload.RecipientSpendKeyY = new(big.Int).Set(input.RecipientNote.ReceiverSpendPubKeyY)
		payload.RecipientViewKeyX = new(big.Int).Set(input.RecipientNote.ReceiverViewPubKeyX)
		payload.RecipientViewKeyY = new(big.Int).Set(input.RecipientNote.ReceiverViewPubKeyY)
		payload.DisclosureBlinding = new(big.Int).Set(input.FullDisclosureBlinding)
		return privacytypes.MarshalDisclosurePlaintextV1(payload)
	}

	if policy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0 {
		payload.Amount = new(big.Int).Set(input.RecipientNote.Amount)
	}
	if policy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0 {
		payload.SenderSpendKeyX = new(big.Int).Set(input.FromNote.ReceiverSpendPubKeyX)
		payload.SenderSpendKeyY = new(big.Int).Set(input.FromNote.ReceiverSpendPubKeyY)
		payload.SenderViewKeyX = new(big.Int).Set(input.FromNote.ReceiverViewPubKeyX)
		payload.SenderViewKeyY = new(big.Int).Set(input.FromNote.ReceiverViewPubKeyY)
	}
	if policy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0 {
		payload.RecipientSpendKeyX = new(big.Int).Set(input.RecipientNote.ReceiverSpendPubKeyX)
		payload.RecipientSpendKeyY = new(big.Int).Set(input.RecipientNote.ReceiverSpendPubKeyY)
		payload.RecipientViewKeyX = new(big.Int).Set(input.RecipientNote.ReceiverViewPubKeyX)
		payload.RecipientViewKeyY = new(big.Int).Set(input.RecipientNote.ReceiverViewPubKeyY)
	}
	payload.DisclosureBlinding = new(big.Int).Set(input.UserDisclosureBlinding)
	return privacytypes.MarshalDisclosurePlaintextV1(payload)
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
