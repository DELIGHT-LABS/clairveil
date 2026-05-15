package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func printListNotesScanStart(cmd *cobra.Command) {
	privacyCommandOutputPrintln(cmd, "Scanning shielded wallet notes...")
}

func renderListNotesText(foundNotes []FoundNote) string {
	var builder strings.Builder

	spendableTotals := buildSpendableAssetTotals(foundNotes)

	builder.WriteString("Shielded notes:\n")
	builder.WriteString(fmt.Sprintf("%-4s | %-15s | %-10s | %-19s | %s\n", "No.", "Amount", "Status", "Asset ID", "Nullifier"))

	for i, info := range foundNotes {
		status := "spendable"
		if info.IsSpent {
			status = "spent"
		}

		builder.WriteString(fmt.Sprintf(
			"[%02d] | %-15s | %-10s | %-19s | %s\n",
			i+1,
			noteAmountString(info.Note),
			status,
			shortAssetID(noteAssetIDHex(info.Note)),
			shortNullifier(info.Nullifier),
		))
	}

	if len(spendableTotals) > 0 {
		builder.WriteString("\nSpendable totals by asset:\n")
		for _, summary := range spendableTotals {
			builder.WriteString(fmt.Sprintf("- %s: %s\n", summary.AssetIDHex, summary.Total.String()))
		}
	}

	if len(spendableTotals) > 0 {
		builder.WriteString("\nSpendable note payloads (JSON):\n")

		for i, info := range foundNotes {
			if info.IsSpent {
				continue
			}

			jsonBytes, _ := json.Marshal(info.Note)
			builder.WriteString(fmt.Sprintf(
				"\n[Note #%02d - amount=%s asset_id=%s]\n%s\n",
				i+1,
				noteAmountString(info.Note),
				noteAssetIDHex(info.Note),
				string(jsonBytes),
			))
		}
	} else {
		builder.WriteString("\nNo spendable shielded notes found.\n")
	}

	return builder.String()
}

func printShieldedAddressSummary(cmd *cobra.Command, addr string) {
	printCommandSection(
		cmd,
		"Shielded address",
		fmt.Sprintf("address: %s", addr),
		"usage: share this full shielded address when someone needs to send you private funds",
	)
}

func printViewingKeySummary(cmd *cobra.Command, incomingViewKeyHex string, viewPubKeyHex string) {
	printCommandSection(
		cmd,
		"Viewing keys",
		fmt.Sprintf("incoming viewing key (hex): %s", incomingViewKeyHex),
		fmt.Sprintf("view public key (hex): %s", viewPubKeyHex),
	)
}

func printDisclosurePublicKeySummary(cmd *cobra.Command, pubKeyHex string, fromAddress string) {
	printCommandSection(
		cmd,
		"Disclosure public key",
		fmt.Sprintf("public key (hex): %s", pubKeyHex),
		fmt.Sprintf("transparent account: %s", fromAddress),
		"derived from: transparent-keyring-root",
	)
}

func printLocalWalletLoadRecoveryWarning(w io.Writer, result *privacyscan.LoadLocalWalletFileResult) {
	if result == nil {
		return
	}

	filename := result.Path
	if filename != "" {
		filename = filepath.Base(filename)
	}

	switch {
	case result.CorruptBackupRenameErr != nil:
		printWarningf(w, "Warning: failed to parse local wallet %s and could not back it up: %v; starting with an empty view.\n", filename, result.CorruptBackupRenameErr)
	case result.CorruptBackupPath != "":
		printWarningf(w, "Warning: failed to parse local wallet %s; moved the broken cache to %s and starting with an empty view.\n", filename, filepath.Base(result.CorruptBackupPath))
	}
}

func printLocalWalletSaveWarning(w io.Writer, err error) {
	if err == nil {
		return
	}

	printWarningf(w, "Warning: failed to save local wallet: %v\n", err)
}

func printWarningf(w io.Writer, format string, args ...any) {
	if w == nil {
		w = os.Stderr
	}

	fmt.Fprintf(w, format, args...)
}

type spendableAssetTotal struct {
	AssetIDHex string
	Total      *big.Int
}

func buildSpendableAssetTotals(foundNotes []FoundNote) []spendableAssetTotal {
	spendable, _ := privacyscan.SummarizeSpendableNotes(foundNotes)
	if len(spendable) == 0 {
		return nil
	}

	byAsset := make(map[string]*big.Int, len(spendable))
	for _, note := range spendable {
		assetIDHex := noteAssetIDHex(note.Note)
		if _, exists := byAsset[assetIDHex]; !exists {
			byAsset[assetIDHex] = new(big.Int)
		}
		byAsset[assetIDHex].Add(byAsset[assetIDHex], note.Note.Amount)
	}

	order := make([]string, 0, len(byAsset))
	for assetIDHex := range byAsset {
		order = append(order, assetIDHex)
	}
	sort.Strings(order)

	summaries := make([]spendableAssetTotal, 0, len(order))
	for _, assetIDHex := range order {
		summaries = append(summaries, spendableAssetTotal{
			AssetIDHex: assetIDHex,
			Total:      new(big.Int).Set(byAsset[assetIDHex]),
		})
	}

	return summaries
}

func noteAssetIDHex(note privacytypes.Note) string {
	if note.AssetID == nil {
		return "unknown"
	}

	assetIDHex, err := canonicalFieldHexFromBigInt(note.AssetID)
	if err != nil {
		return note.AssetID.String()
	}

	return assetIDHex
}

func shortAssetID(value string) string {
	if len(value) <= 18 {
		return value
	}

	return fmt.Sprintf("%s...%s", value[:8], value[len(value)-8:])
}

func shortNullifier(value string) string {
	if len(value) <= 12 {
		return value
	}
	return fmt.Sprintf("%s...%s", value[:6], value[len(value)-6:])
}
