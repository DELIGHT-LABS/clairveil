package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	sdkkeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestPrintTransferCommandSummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printTransferCommandSummary(cmd, "clairs1recipient", "10uclair", "amount-to", "public", true)

	require.Equal(
		t,
		"Shielded transfer\n- recipient: clairs1recipient\n- amount: 10uclair\n- user disclosure: amount-to / public\n- audit disclosure: enabled (chain-configured key)\n- auto dummy: true\n",
		out.String(),
	)
}

func TestPrintWithdrawCommandSummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printWithdrawCommandSummary(cmd, "Shielded withdraw", "clair1recipient", "7uclair", true, false)

	require.Equal(
		t,
		"Shielded withdraw\n- recipient: clair1recipient\n- amount: 7uclair\n- auto plan: true\n- auto dummy: false\n",
		out.String(),
	)
}

func TestPrintAutoDummyPreparationSummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printAutoDummyPreparationSummary(cmd, "uclair", "0uclair")

	require.Equal(
		t,
		"Auto dummy note\n- denom: uclair\n- deposit: 0uclair\n- why: the current two-input transfer path needs a same-denom zero note to split one larger note\n",
		out.String(),
	)
}

func TestPrintPlannerSelfTransferSummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printPlannerSelfTransferSummary(cmd, "7uclair", "clairs1self")

	require.Equal(
		t,
		"Planner self-transfer\n- amount: 7uclair\n- recipient: clairs1self\n- purpose: create one exact-match note for withdraw\n- mode: all-private self-transfer\n",
		out.String(),
	)
}

func TestTransferProgressOutputHelpers(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printTransferScanStep(cmd, 2)
	printTransferBroadcastFinal(cmd, 2)
	printTransferBroadcastSelfMerge(cmd, 2, big.NewInt(15))
	printTransferComplete(cmd, 3, "tx-hash")
	printTransferWaitForBlock(cmd, 1, "tx-wait")

	require.Equal(
		t,
		"Step 2: scanning shielded wallet notes.\nStep 2: broadcasting the final transfer.\nStep 2: broadcasting a planner self-transfer for 15.\nStep 3: final transfer accepted (tx: tx-hash)\nStep 1: waiting for the next block after tx tx-wait\n",
		out.String(),
	)
}

func TestPrintCommandJSONWritesIndentedJSON(t *testing.T) {
	cmd, out := newOutputTestCommand()

	err := printCommandJSON(cmd, map[string]string{"hello": "world"})

	require.NoError(t, err)
	require.Equal(t, "{\n  \"hello\": \"world\"\n}\n", out.String())
}

func TestScanNotesWithOptionsRequiresFromAccount(t *testing.T) {
	_, err := scanNotesWithOptions(client.Context{}, nil, scanNotesOptions{})

	require.ErrorContains(t, err, "a transparent --from account is required to scan shielded notes")
}

func TestShowShieldedAddressOutputModes(t *testing.T) {
	clientCtx, fromAddress := newRootSeedQualificationClientContext(t, sdkkeyring.BackendTest)
	expectedShieldedAddress := deriveQualifiedShieldedAddress(t, clientCtx)

	t.Run("json", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowShieldedAddress(), clientCtx, nil)

		var summary shieldedAddressSummary
		require.NoError(t, json.Unmarshal([]byte(stdout), &summary))
		require.Equal(t, fromAddress, summary.FromAddress)
		require.Equal(t, expectedShieldedAddress, summary.Address)
		require.Equal(t, "transparent-keyring-root", summary.DerivedFrom)
		require.Contains(t, summary.Usage, "share this full shielded address")
		require.Empty(t, stderr)
	})

	t.Run("text", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowShieldedAddress(), clientCtx, []string{"--output", "text"})

		require.Contains(t, stdout, "Shielded address")
		require.Contains(t, stdout, "- address: "+expectedShieldedAddress)
		require.Empty(t, stderr)
	})
}

func TestShowViewingKeyOutputModes(t *testing.T) {
	clientCtx, fromAddress := newRootSeedQualificationClientContext(t, sdkkeyring.BackendTest)
	rootSeed, _, err := derivePrivacyRootSeed(clientCtx)
	require.NoError(t, err)

	viewScalar, viewPubKey, _ := deriveViewKeys(rootSeed)
	expectedIncomingViewKeyHex := scalarToFixedHex(viewScalar)
	expectedViewPublicKeyHex := encodePointHex(viewPubKey)

	t.Run("json", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowViewingKey(), clientCtx, nil)

		var summary viewingKeySummary
		require.NoError(t, json.Unmarshal([]byte(stdout), &summary))
		require.Equal(t, fromAddress, summary.FromAddress)
		require.Equal(t, expectedIncomingViewKeyHex, summary.IncomingViewKeyHex)
		require.Equal(t, expectedViewPublicKeyHex, summary.ViewPublicKeyHex)
		require.Equal(t, "transparent-keyring-root", summary.DerivedFrom)
		require.Empty(t, stderr)
	})

	t.Run("text", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowViewingKey(), clientCtx, []string{"--output", "text"})

		require.Contains(t, stdout, "Viewing keys")
		require.Contains(t, stdout, "- incoming viewing key (hex): "+expectedIncomingViewKeyHex)
		require.Contains(t, stdout, "- view public key (hex): "+expectedViewPublicKeyHex)
		require.Empty(t, stderr)
	})
}

func TestShowDisclosurePubKeyOutputModes(t *testing.T) {
	clientCtx, fromAddress := newRootSeedQualificationClientContext(t, sdkkeyring.BackendTest)
	expectedPubKeyHex := deriveQualifiedDisclosurePubKey(t, clientCtx)

	t.Run("json-default", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowDisclosurePubKey(), clientCtx, nil)

		var summary disclosurePublicKeySummary
		require.NoError(t, json.Unmarshal([]byte(stdout), &summary))
		require.Equal(t, fromAddress, summary.FromAddress)
		require.Equal(t, expectedPubKeyHex, summary.PublicKeyHex)
		require.Equal(t, "transparent-keyring-root", summary.DerivedFrom)
		require.Empty(t, stderr)
	})

	t.Run("json-flag", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowDisclosurePubKey(), clientCtx, []string{"--output", "text", "--json"})

		var summary disclosurePublicKeySummary
		require.NoError(t, json.Unmarshal([]byte(stdout), &summary))
		require.Equal(t, expectedPubKeyHex, summary.PublicKeyHex)
		require.Empty(t, stderr)
	})

	t.Run("text", func(t *testing.T) {
		stdout, stderr := executeOutputCommand(t, CmdShowDisclosurePubKey(), clientCtx, []string{"--output", "text"})

		require.Contains(t, stdout, "Disclosure public key")
		require.Contains(t, stdout, "- public key (hex): "+expectedPubKeyHex)
		require.Contains(t, stdout, "- transparent account: "+fromAddress)
		require.Empty(t, stderr)
	})
}

func newOutputTestCommand() (*cobra.Command, *bytes.Buffer) {
	out := new(bytes.Buffer)
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	return cmd, out
}

func executeOutputCommand(t *testing.T, cmd *cobra.Command, clientCtx client.Context, args []string) (string, string) {
	t.Helper()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetContext(context.Background())

	require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
	if args == nil {
		args = []string{"--output", "json"}
	}
	cmd.SetArgs(args)

	err := cmd.Execute()
	require.NoError(t, err)

	return stdout.String(), stderr.String()
}
