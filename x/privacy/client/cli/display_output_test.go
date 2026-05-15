package cli

import (
	"bytes"
	"errors"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestRenderListNotesText(t *testing.T) {
	uclairAssetIDHex, err := canonicalFieldHexFromBigInt(privacycrypto.HashString("uclair"))
	require.NoError(t, err)
	uatomAssetIDHex, err := canonicalFieldHexFromBigInt(privacycrypto.HashString("uatom"))
	require.NoError(t, err)

	foundNotes := []FoundNote{
		{
			Note: privacytypes.Note{
				Amount:  math.NewInt(7).BigInt(),
				AssetID: privacycrypto.HashString("uclair"),
			},
			Nullifier: "abcdef1234567890",
			IsSpent:   false,
		},
		{
			Note: privacytypes.Note{
				Amount:  math.NewInt(2).BigInt(),
				AssetID: privacycrypto.HashString("uatom"),
			},
			Nullifier: "fedcba0987654321",
			IsSpent:   false,
		},
		{
			Note: privacytypes.Note{
				Amount:  math.NewInt(3).BigInt(),
				AssetID: privacycrypto.HashString("uclair"),
			},
			Nullifier: "1122334455667788",
			IsSpent:   true,
		},
	}

	rendered := renderListNotesText(foundNotes)
	uclairShort := shortAssetID(uclairAssetIDHex)
	uatomShort := shortAssetID(uatomAssetIDHex)

	require.Contains(t, rendered, "No.  | Amount")
	require.Contains(t, rendered, "Asset ID")
	require.Contains(t, rendered, "[01] | 7")
	require.Contains(t, rendered, "spendable  | "+uclairShort+" | abcdef...567890")
	require.Contains(t, rendered, "[02] | 2")
	require.Contains(t, rendered, "spendable  | "+uatomShort+" | fedcba...654321")
	require.Contains(t, rendered, "[03] | 3")
	require.Contains(t, rendered, "spent      | "+uclairShort+" | 112233...667788")
	require.Contains(t, rendered, "\nSpendable totals by asset:\n")
	require.Contains(t, rendered, "- "+uclairAssetIDHex+": 7\n")
	require.Contains(t, rendered, "- "+uatomAssetIDHex+": 2\n")
	require.Contains(t, rendered, "Shielded notes:\n")
	require.Contains(t, rendered, "\nSpendable note payloads (JSON):\n")
	require.Contains(t, rendered, "[Note #01 - amount=7 asset_id="+uclairAssetIDHex+"]\n")
	require.Contains(t, rendered, "[Note #02 - amount=2 asset_id="+uatomAssetIDHex+"]\n")
	require.Contains(t, rendered, "\"am\":7")
	require.NotContains(t, rendered, " uclair")
}

func TestPrintShieldedAddressSummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printShieldedAddressSummary(cmd, "clairs1abc")

	require.Equal(
		t,
		"Shielded address\n- address: clairs1abc\n- usage: share this full shielded address when someone needs to send you private funds\n",
		out.String(),
	)
}

func TestPrintViewingKeySummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printViewingKeySummary(cmd, "abcd", "efgh")

	require.Equal(
		t,
		"Viewing keys\n- incoming viewing key (hex): abcd\n- view public key (hex): efgh\n",
		out.String(),
	)
}

func TestPrintDisclosurePublicKeySummary(t *testing.T) {
	cmd, out := newOutputTestCommand()

	printDisclosurePublicKeySummary(cmd, "pubhex", "clair1addr")

	require.Equal(
		t,
		"Disclosure public key\n- public key (hex): pubhex\n- transparent account: clair1addr\n- derived from: transparent-keyring-root\n",
		out.String(),
	)
}

func TestPrintLocalWalletLoadRecoveryWarningReportsBackupPath(t *testing.T) {
	var out bytes.Buffer

	printLocalWalletLoadRecoveryWarning(&out, &privacyscan.LoadLocalWalletFileResult{
		Path:              "/tmp/privacy_wallet_clair1test.json",
		CorruptBackupPath: "/tmp/privacy_wallet_clair1test.json.corrupt-1700000000.bak",
	})

	require.Equal(
		t,
		"Warning: failed to parse local wallet privacy_wallet_clair1test.json; moved the broken cache to privacy_wallet_clair1test.json.corrupt-1700000000.bak and starting with an empty view.\n",
		out.String(),
	)
}

func TestPrintLocalWalletLoadRecoveryWarningReportsRenameFailure(t *testing.T) {
	var out bytes.Buffer

	printLocalWalletLoadRecoveryWarning(&out, &privacyscan.LoadLocalWalletFileResult{
		Path:                   "/tmp/privacy_wallet_clair1test.json",
		CorruptBackupRenameErr: errors.New("permission denied"),
	})

	require.Equal(
		t,
		"Warning: failed to parse local wallet privacy_wallet_clair1test.json and could not back it up: permission denied; starting with an empty view.\n",
		out.String(),
	)
}

func TestPrintLocalWalletSaveWarning(t *testing.T) {
	var out bytes.Buffer

	printLocalWalletSaveWarning(&out, errors.New("disk full"))

	require.Equal(t, "Warning: failed to save local wallet: disk full\n", out.String())
}

func TestBuildSpendableAssetTotals(t *testing.T) {
	uclairAssetIDHex, err := canonicalFieldHexFromBigInt(privacycrypto.HashString("uclair"))
	require.NoError(t, err)
	uatomAssetIDHex, err := canonicalFieldHexFromBigInt(privacycrypto.HashString("uatom"))
	require.NoError(t, err)

	totals := buildSpendableAssetTotals([]FoundNote{
		{
			Note:    privacytypes.Note{Amount: math.NewInt(7).BigInt(), AssetID: privacycrypto.HashString("uclair")},
			IsSpent: false,
		},
		{
			Note:    privacytypes.Note{Amount: math.NewInt(2).BigInt(), AssetID: privacycrypto.HashString("uatom")},
			IsSpent: false,
		},
		{
			Note:    privacytypes.Note{Amount: math.NewInt(5).BigInt(), AssetID: privacycrypto.HashString("uclair")},
			IsSpent: false,
		},
		{
			Note:    privacytypes.Note{Amount: math.NewInt(9).BigInt(), AssetID: privacycrypto.HashString("uclair")},
			IsSpent: true,
		},
	})

	require.Len(t, totals, 2)
	require.Equal(t, uclairAssetIDHex, totals[0].AssetIDHex)
	require.Equal(t, "12", totals[0].Total.String())
	require.Equal(t, uatomAssetIDHex, totals[1].AssetIDHex)
	require.Equal(t, "2", totals[1].Total.String())
}

func TestShortNullifier(t *testing.T) {
	require.Equal(t, "short", shortNullifier("short"))
	require.Equal(t, "abcdef...567890", shortNullifier("abcdef1234567890"))
}

func TestShortAssetID(t *testing.T) {
	require.Equal(t, "short-asset", shortAssetID("short-asset"))
	require.Equal(t, "12345678...90abcdef", shortAssetID("1234567890abcdef1234567890abcdef"))
}
