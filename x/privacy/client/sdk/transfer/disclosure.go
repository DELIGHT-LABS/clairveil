package transfer

import (
	"encoding/hex"
	"fmt"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type DisclosureBuildInput struct {
	OutputCommitment       []byte
	TransferDenom          string
	FromNote               privacytypes.SecretNoteV1
	RecipientNote          privacytypes.SecretNoteV1
	UserDisclosureBlinding privacycrypto.FieldValue
	FullDisclosureBlinding privacycrypto.FieldValue
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
	assetIDHex := input.RecipientNote.AssetIDHex()

	digest, err := fixedTransferDigest(input, userPrivacyPolicy)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)
	blindingHex := fixedFieldHex(input.UserDisclosureBlinding)

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
		payload.Amount = fmt.Sprintf("%d", input.RecipientNote.Amount)
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
	assetIDHex := input.RecipientNote.AssetIDHex()

	digest, err := fixedTransferDigest(input, privacytypes.DisclosureFullMarkerV1)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)
	blindingHex := fixedFieldHex(input.FullDisclosureBlinding)

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
		Amount:              fmt.Sprintf("%d", input.RecipientNote.Amount),
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
	assetIDHex := input.RecipientNote.AssetIDHex()

	digest, err := fixedTransferDigest(input, privacytypes.DisclosureFullMarkerV1)
	if err != nil {
		return nil, err
	}
	digestHex := hex.EncodeToString(digest)
	blindingHex := fixedFieldHex(input.FullDisclosureBlinding)

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
		Amount:              fmt.Sprintf("%d", input.RecipientNote.Amount),
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
	commitment, err := privacycrypto.ParseFieldValueBE32(input.OutputCommitment)
	if err != nil {
		return nil, err
	}
	payload := &privacytypes.SecretDisclosurePlaintextV1{
		Plane:                plane,
		OutputIndex:          privacytypes.TransferDisclosureRecipientOutputIndex,
		Policy:               policy,
		DisclosedFieldBitmap: policy,
		Commitment:           commitment,
		Amount:               0,
		AssetID:              input.RecipientNote.AssetID,
		SenderSpendKeyX:      privacycrypto.FieldValue{},
		SenderSpendKeyY:      privacycrypto.FieldValue{},
		SenderViewKeyX:       privacycrypto.FieldValue{},
		SenderViewKeyY:       privacycrypto.FieldValue{},
		RecipientSpendKeyX:   privacycrypto.FieldValue{},
		RecipientSpendKeyY:   privacycrypto.FieldValue{},
		RecipientViewKeyX:    privacycrypto.FieldValue{},
		RecipientViewKeyY:    privacycrypto.FieldValue{},
	}

	if plane == privacytypes.DisclosurePlaneFullV1 {
		payload.DisclosedFieldBitmap = privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom
		payload.Amount = input.RecipientNote.Amount
		payload.AssetID = input.RecipientNote.AssetID
		payload.SenderSpendKeyX = input.FromNote.ReceiverSpendPubKeyX
		payload.SenderSpendKeyY = input.FromNote.ReceiverSpendPubKeyY
		payload.SenderViewKeyX = input.FromNote.ReceiverViewPubKeyX
		payload.SenderViewKeyY = input.FromNote.ReceiverViewPubKeyY
		payload.RecipientSpendKeyX = input.RecipientNote.ReceiverSpendPubKeyX
		payload.RecipientSpendKeyY = input.RecipientNote.ReceiverSpendPubKeyY
		payload.RecipientViewKeyX = input.RecipientNote.ReceiverViewPubKeyX
		payload.RecipientViewKeyY = input.RecipientNote.ReceiverViewPubKeyY
		payload.DisclosureBlinding = input.FullDisclosureBlinding
		return privacytypes.MarshalSecretDisclosurePlaintextV1(payload)
	}

	if policy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0 {
		payload.Amount = input.RecipientNote.Amount
	}
	if policy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0 {
		payload.SenderSpendKeyX = input.FromNote.ReceiverSpendPubKeyX
		payload.SenderSpendKeyY = input.FromNote.ReceiverSpendPubKeyY
		payload.SenderViewKeyX = input.FromNote.ReceiverViewPubKeyX
		payload.SenderViewKeyY = input.FromNote.ReceiverViewPubKeyY
	}
	if policy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0 {
		payload.RecipientSpendKeyX = input.RecipientNote.ReceiverSpendPubKeyX
		payload.RecipientSpendKeyY = input.RecipientNote.ReceiverSpendPubKeyY
		payload.RecipientViewKeyX = input.RecipientNote.ReceiverViewPubKeyX
		payload.RecipientViewKeyY = input.RecipientNote.ReceiverViewPubKeyY
	}
	payload.DisclosureBlinding = input.UserDisclosureBlinding
	return privacytypes.MarshalSecretDisclosurePlaintextV1(payload)
}

func disclosureAddresses(input DisclosureBuildInput) (string, string, error) {
	fromAddress, err := secretNoteAddress(input.FromNote)
	if err != nil {
		return "", "", fmt.Errorf("failed to encode sender shielded address: %w", err)
	}

	toAddress, err := secretNoteAddress(input.RecipientNote)
	if err != nil {
		return "", "", fmt.Errorf("failed to encode recipient shielded address: %w", err)
	}

	return fromAddress, toAddress, nil
}

func fixedFieldHex(value privacycrypto.FieldValue) string {
	raw := value.Bytes()
	return hex.EncodeToString(raw[:])
}
func fixedTransferDigest(input DisclosureBuildInput, policy uint32) ([]byte, error) {
	c, err := privacycrypto.ParseFieldValueBE32(input.OutputCommitment)
	if err != nil {
		return nil, err
	}
	var digest privacycrypto.FieldValue
	if policy == privacytypes.DisclosureFullMarkerV1 {
		digest, err = privacytypes.SecretFullTransferDisclosureDigestV1(privacytypes.SecretFullTransferDisclosureV1Input{OutputIndex: privacytypes.TransferDisclosureRecipientOutputIndex, Commitment: c, Amount: input.RecipientNote.Amount, AssetID: input.RecipientNote.AssetID, FromSpendPubKeyX: input.FromNote.ReceiverSpendPubKeyX, FromSpendPubKeyY: input.FromNote.ReceiverSpendPubKeyY, FromViewPubKeyX: input.FromNote.ReceiverViewPubKeyX, FromViewPubKeyY: input.FromNote.ReceiverViewPubKeyY, ToSpendPubKeyX: input.RecipientNote.ReceiverSpendPubKeyX, ToSpendPubKeyY: input.RecipientNote.ReceiverSpendPubKeyY, ToViewPubKeyX: input.RecipientNote.ReceiverViewPubKeyX, ToViewPubKeyY: input.RecipientNote.ReceiverViewPubKeyY, Blinding: input.FullDisclosureBlinding})
	} else {
		digest, err = privacytypes.SecretTransferDisclosureDigestV1(privacytypes.SecretTransferDisclosureV1Input{Policy: policy, OutputIndex: privacytypes.TransferDisclosureRecipientOutputIndex, Commitment: c, Amount: input.RecipientNote.Amount, AssetID: input.RecipientNote.AssetID, FromSpendPubKeyX: input.FromNote.ReceiverSpendPubKeyX, FromSpendPubKeyY: input.FromNote.ReceiverSpendPubKeyY, FromViewPubKeyX: input.FromNote.ReceiverViewPubKeyX, FromViewPubKeyY: input.FromNote.ReceiverViewPubKeyY, ToSpendPubKeyX: input.RecipientNote.ReceiverSpendPubKeyX, ToSpendPubKeyY: input.RecipientNote.ReceiverSpendPubKeyY, ToViewPubKeyX: input.RecipientNote.ReceiverViewPubKeyX, ToViewPubKeyY: input.RecipientNote.ReceiverViewPubKeyY, Blinding: input.UserDisclosureBlinding})
	}
	if err != nil {
		return nil, err
	}
	raw := digest.Bytes()
	return append([]byte(nil), raw[:]...), nil
}
