package scan

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestVerifyPrivacyScanDisclosuresSeparatesDigestMismatch(t *testing.T) {
	zero := func() *big.Int { return new(big.Int) }
	plaintext := &privacytypes.DisclosurePlaintextV1{
		Plane: privacytypes.DisclosurePlaneUserV1, OutputIndex: 3,
		Policy: privacytypes.TransferPrivacyPolicyDiscloseAmount, DisclosedFieldBitmap: privacytypes.TransferPrivacyPolicyDiscloseAmount,
		Commitment: big.NewInt(11), Amount: big.NewInt(17), AssetID: big.NewInt(19),
		SenderSpendKeyX: zero(), SenderSpendKeyY: zero(), SenderViewKeyX: zero(), SenderViewKeyY: zero(),
		RecipientSpendKeyX: zero(), RecipientSpendKeyY: zero(), RecipientViewKeyX: zero(), RecipientViewKeyY: zero(),
		DisclosureBlinding: big.NewInt(23),
	}
	encoded, err := privacytypes.MarshalDisclosurePlaintextV1(plaintext)
	require.NoError(t, err)
	digest, err := privacytypes.ComputeBatchUserDisclosureDigestV1(privacytypes.BatchUserDisclosureV1Input{
		OutputIndex: 3, Commitment: plaintext.Commitment, Policy: plaintext.Policy, DisclosedFieldBitmap: plaintext.DisclosedFieldBitmap,
		SelectedAmount: plaintext.Amount, AssetID: plaintext.AssetID,
		SelectedFromSpendKeyX: plaintext.SenderSpendKeyX, SelectedFromSpendKeyY: plaintext.SenderSpendKeyY, SelectedFromViewKeyX: plaintext.SenderViewKeyX, SelectedFromViewKeyY: plaintext.SenderViewKeyY,
		SelectedToSpendKeyX: plaintext.RecipientSpendKeyX, SelectedToSpendKeyY: plaintext.RecipientSpendKeyY, SelectedToViewKeyX: plaintext.RecipientViewKeyX, SelectedToViewKeyY: plaintext.RecipientViewKeyY,
		UserDisclosureBlinding: plaintext.DisclosureBlinding,
	})
	require.NoError(t, err)
	output := &privacytypes.PrivacyScanOutputV2{
		EventType: privacytypes.EventTypeBatchTransferV1, OutputIndex: 3, Commitment: plaintext.Commitment.FillBytes(make([]byte, 32)),
		UserPrivacyPolicy: plaintext.Policy, UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String(),
		UserDisclosurePayload: encoded, UserDisclosureDigest: digest.FillBytes(make([]byte, 32)),
	}
	evidence := VerifyPrivacyScanDisclosures(output, DisclosureKeySet{})
	require.Equal(t, DisclosureVerified, evidence.User.Status)

	output.UserDisclosureDigest[31] ^= 1
	evidence = VerifyPrivacyScanDisclosures(output, DisclosureKeySet{})
	require.Equal(t, DisclosureDigestMismatch, evidence.User.Status)
	require.False(t, evidence.AuditDeliveryFailed)
}

func TestVerifyPrivacyScanDisclosuresMarksAuditDeliveryFailure(t *testing.T) {
	raw := make([]byte, 452) // DisclosurePlaintextV1 plus ECIES overhead.
	envelope, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeAuditDisclosureV1, raw)
	require.NoError(t, err)
	output := &privacytypes.PrivacyScanOutputV2{AuditDisclosurePayload: envelope}
	evidence := VerifyPrivacyScanDisclosures(output, DisclosureKeySet{Audit: big.NewInt(1)})
	require.Equal(t, DisclosureDecryptFailed, evidence.Audit.Status)
	require.True(t, evidence.AuditDeliveryFailed)
	require.True(t, evidence.ManualReview)
	require.Equal(t, "AuditDeliveryFailed", evidence.ManualReviewReason)
}
