package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestSeedLocalnetNotesWritesGenesisWalletAndNotesOut(t *testing.T) {
	dir := t.TempDir()
	genesisPath := filepath.Join(dir, "genesis.json")
	walletHome := filepath.Join(dir, "home")
	notesPath := filepath.Join(dir, "alice-notes.json")
	reportPath := filepath.Join(dir, "seed-report.json")
	ownerAddress := "clair1owneraddress"
	require.NoError(t, os.WriteFile(genesisPath, []byte(`{"app_state":{"privacy":{"commitments":[]}}}`), 0o600))

	require.NoError(t, runSeedLocalnetNotes([]string{
		"-genesis", genesisPath,
		"-wallet-home", walletHome,
		"-owner-address", ownerAddress,
		"-shielded-address", testSeedLocalnetShieldedAddress(t),
		"-count", "2",
		"-amount", "7",
		"-denom", "uclair",
		"-notes-out", notesPath,
		"-out", reportPath,
	}))

	var genesis map[string]any
	require.NoError(t, readJSONFile(genesisPath, &genesis))
	privacyState := genesis["app_state"].(map[string]any)["privacy"].(map[string]any)
	commitments := privacyState["commitments"].([]any)
	require.Len(t, commitments, 4)
	for _, value := range commitments {
		decoded, err := base64.StdEncoding.DecodeString(value.(string))
		require.NoError(t, err)
		require.Len(t, decoded, 32)
	}

	walletResult, err := privacyscan.LoadLocalWalletFile(walletHome, ownerAddress)
	require.NoError(t, err)
	require.Len(t, walletResult.Wallet.Notes, 4)

	var notes listNotesFile
	require.NoError(t, readJSONFile(notesPath, &notes))
	require.Len(t, notes.Notes, 4)
	require.Equal(t, "7", notes.Notes[0].Amount)
	require.Equal(t, "0", notes.Notes[1].Amount)

	var report seedLocalnetNotesReport
	require.NoError(t, readJSONFile(reportPath, &report))
	require.Equal(t, 2, report.SeededItemCount)
	require.Equal(t, 4, report.SeededNoteCount)
	require.Equal(t, 4, report.CommitmentsAdded)
}

func testSeedLocalnetShieldedAddress(t *testing.T) string {
	t.Helper()
	rootSeed := make([]byte, privacyidentity.RootSeedLength)
	for i := range rootSeed {
		rootSeed[i] = byte(i + 1)
	}
	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)
	address, err := privacytypes.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
	require.NoError(t, err)
	return address
}

func readJSONFile(path string, out any) error {
	bz, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(bz, out)
}
