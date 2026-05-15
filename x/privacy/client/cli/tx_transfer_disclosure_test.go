package cli

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildTransferDisclosureReportForPublicUserPayload(t *testing.T) {
	payload := &transferDisclosurePayload{
		Plane:      transferDisclosurePayloadPlaneUser,
		Policy:     types.TransferPrivacyPolicyDiscloseAmount,
		Amount:     "7",
		AssetDenom: "uclair",
	}
	input := &transferDisclosureDecodeInput{IsPlaintextPayload: true}
	verification := &transferDisclosureVerificationReport{Verified: true}

	report := buildTransferDisclosureReport(payload, input, verification)

	require.Equal(t, "public", report.Source)
	require.Equal(t, "user", report.Summary.Plane)
	require.Equal(t, "public", report.Summary.Delivery)
	require.Equal(t, "amount", report.Summary.Policy)
	require.Equal(t, []string{"amount"}, report.Summary.DisclosedFields)
}

func TestBuildTransferDisclosureReportForAuditPayload(t *testing.T) {
	payload := &transferDisclosurePayload{
		Plane:               transferDisclosurePayloadPlaneAudit,
		Policy:              types.TransferPrivacyPolicyDiscloseAmountToFrom,
		Amount:              "10",
		AssetDenom:          "uclair",
		FromShieldedAddress: "clairs1from",
		ToShieldedAddress:   "clairs1to",
	}
	input := &transferDisclosureDecodeInput{}
	verification := &transferDisclosureVerificationReport{Verified: true}

	report := buildTransferDisclosureReport(payload, input, verification)

	require.Equal(t, "audit_encrypted", report.Source)
	require.Equal(t, "audit", report.Summary.Plane)
	require.Equal(t, "audit-encrypted", report.Summary.Delivery)
	require.Equal(t, "audit-full", report.Summary.Policy)
	require.Equal(t, []string{"amount", "from_shielded_address", "to_shielded_address"}, report.Summary.DisclosedFields)
}

func TestUserDisclosureModeLabel(t *testing.T) {
	require.Equal(t, "none", userDisclosureModeLabel(types.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE))
	require.Equal(t, "public", userDisclosureModeLabel(types.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC))
	require.Equal(t, "recipient-encrypted", userDisclosureModeLabel(types.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED))
}

func TestResolveTransferDisclosureCipherTextInputRejectsMixedSources(t *testing.T) {
	cmd := newTransferDisclosureTestCommand(t)
	require.NoError(t, cmd.Flags().Set(flagTransferDisclosureTxHash, "ABCD"))

	_, err := resolveTransferDisclosureCipherTextInput(cmd, []string{"cafe"})

	require.ErrorContains(t, err, "provide either ciphertext_hex or --tx-hash, not both")
}

func TestResolveTransferDisclosureCipherTextInputRequiresSource(t *testing.T) {
	cmd := newTransferDisclosureTestCommand(t)

	_, err := resolveTransferDisclosureCipherTextInput(cmd, nil)

	require.ErrorContains(t, err, "provide ciphertext_hex or set --tx-hash")
}

func TestNormalizeTransferDisclosurePlaneRejectsUnsupportedValue(t *testing.T) {
	_, err := normalizeTransferDisclosurePlane("sideways")

	require.ErrorContains(t, err, "unsupported --disclosure-plane value \"sideways\"")
	require.ErrorContains(t, err, "supported values: auto, public, recipient, audit")
}

func TestLookupTransferDisclosureCipherTextByTxHashRequiresRPCClient(t *testing.T) {
	_, err := lookupTransferDisclosureCipherTextByTxHash(context.Background(), client.Context{}, "ABCD", transferDisclosurePlaneAuto)

	require.ErrorContains(t, err, "an RPC client is required to look up --tx-hash")
}

func newTransferDisclosureTestCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().String(flagTransferDisclosureTxHash, "", "")
	cmd.Flags().String(flagTransferDisclosurePlane, transferDisclosurePlaneAuto, "")
	return cmd
}
