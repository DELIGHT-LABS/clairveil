package transfer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestParsePrivacyPolicy(t *testing.T) {
	policy, err := ParsePrivacyPolicy("amount-from-to")
	require.NoError(t, err)
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom, policy)
}

func TestPrivacyPolicyLabel(t *testing.T) {
	require.Equal(t, PrivacyPolicyAmountFromTo, PrivacyPolicyLabel(privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom))
}

func TestParseDisclosureMode(t *testing.T) {
	mode, err := ParseDisclosureMode("recipient-encrypted")
	require.NoError(t, err)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, mode)
}

func TestUserDisclosureModeLabel(t *testing.T) {
	require.Equal(t, DisclosureModeNone, UserDisclosureModeLabel(privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE))
	require.Equal(t, DisclosureModePublic, UserDisclosureModeLabel(privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC))
	require.Equal(t, DisclosureModeRecipientEncrypted, UserDisclosureModeLabel(privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED))
}

func TestResolveRuntimeConfigAllPrivateRejectsDisclosureMode(t *testing.T) {
	_, pubKey := testScalarAndPubKey(331)
	pubKeyBytes := pubKey.Bytes()

	_, err := ResolveRuntimeConfig(
		context.Background(),
		&stubAuditDisclosureTargetProvider{pubKeyHex: fmt.Sprintf("%x", pubKeyBytes[:])},
		ResolveRuntimeConfigInput{
			RawPolicy:         PrivacyPolicyAllPrivate,
			RawDisclosureMode: DisclosureModePublic,
		},
	)
	require.ErrorContains(t, err, "all-private transfers must use disclosure mode")
}

func TestResolveRuntimeConfigPublicRejectsDisclosureKey(t *testing.T) {
	_, pubKey := testScalarAndPubKey(337)
	pubKeyBytes := pubKey.Bytes()

	_, err := ResolveRuntimeConfig(
		context.Background(),
		&stubAuditDisclosureTargetProvider{pubKeyHex: fmt.Sprintf("%x", pubKeyBytes[:])},
		ResolveRuntimeConfigInput{
			RawPolicy:           PrivacyPolicyAmount,
			RawDisclosureMode:   DisclosureModePublic,
			DisclosurePubKeyHex: fmt.Sprintf("%x", pubKeyBytes[:]),
		},
	)
	require.ErrorContains(t, err, "public disclosure must not set a disclosure pubkey")
}

func TestResolveRuntimeConfigEncryptedResolvesTargets(t *testing.T) {
	_, disclosurePubKey := testScalarAndPubKey(347)
	disclosurePubKeyBytes := disclosurePubKey.Bytes()
	_, auditPubKey := testScalarAndPubKey(349)
	auditPubKeyBytes := auditPubKey.Bytes()

	config, err := ResolveRuntimeConfig(
		context.Background(),
		&stubAuditDisclosureTargetProvider{pubKeyHex: fmt.Sprintf("%x", auditPubKeyBytes[:])},
		ResolveRuntimeConfigInput{
			RawPolicy:           PrivacyPolicyAmountFromTo,
			RawDisclosureMode:   DisclosureModeRecipientEncrypted,
			DisclosurePubKeyHex: fmt.Sprintf("%x", disclosurePubKeyBytes[:]),
		},
	)
	require.NoError(t, err)
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom, config.UserPrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, config.UserDisclosureMode)
	require.Equal(t, disclosurePubKey.Bytes(), config.UserDisclosureTargetPubKey.Bytes())
	require.Equal(t, disclosurePubKeyBytes[:], config.UserDisclosureTargetPubKeyBz)
	require.Equal(t, auditPubKey.Bytes(), config.AuditDisclosureTargetPubKey.Bytes())
	require.Equal(t, auditPubKeyBytes[:], config.AuditDisclosureTargetPubKeyBz)
}
