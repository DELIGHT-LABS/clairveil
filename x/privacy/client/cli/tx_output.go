package cli

import (
	"fmt"
	"math/big"

	"github.com/spf13/cobra"
)

func printCommandSection(cmd *cobra.Command, title string, lines ...string) {
	privacyCommandPrintln(cmd, title)
	for _, line := range lines {
		privacyCommandPrintf(cmd, "- %s\n", line)
	}
}

func printTransferCommandSummary(cmd *cobra.Command, recipient string, amount string, userPolicy string, userMode string, autoDummy bool) {
	printCommandSection(
		cmd,
		"Shielded transfer",
		fmt.Sprintf("recipient: %s", recipient),
		fmt.Sprintf("amount: %s", amount),
		fmt.Sprintf("user disclosure: %s / %s", userPolicy, userMode),
		"audit disclosure: enabled (chain-configured key)",
		fmt.Sprintf("auto dummy: %t", autoDummy),
	)
}

func printWithdrawCommandSummary(cmd *cobra.Command, title string, recipient string, amount string, autoPlan bool, autoDummy bool) {
	printCommandSection(
		cmd,
		title,
		fmt.Sprintf("recipient: %s", recipient),
		fmt.Sprintf("amount: %s", amount),
		fmt.Sprintf("auto plan: %t", autoPlan),
		fmt.Sprintf("auto dummy: %t", autoDummy),
	)
}

func printAutoDummyPreparationSummary(cmd *cobra.Command, denom string, deposit string) {
	printCommandSection(
		cmd,
		"Auto dummy note",
		fmt.Sprintf("denom: %s", denom),
		fmt.Sprintf("deposit: %s", deposit),
		"why: the current two-input transfer path needs a same-denom zero note to split one larger note",
	)
}

func printAutoDummySubmitted(cmd *cobra.Command, txHash string) {
	privacyCommandPrintf(cmd, "Auto dummy note accepted (tx: %s); waiting for the next block.\n", txHash)
}

func printSelectedWithdrawNote(cmd *cobra.Command, amount string) {
	privacyCommandPrintf(cmd, "Selected exact-match note: %s\n", amount)
}

func printPlannerSelfTransferSummary(cmd *cobra.Command, target string, recipient string) {
	printCommandSection(
		cmd,
		"Planner self-transfer",
		fmt.Sprintf("amount: %s", target),
		fmt.Sprintf("recipient: %s", recipient),
		"purpose: create one exact-match note for withdraw",
		"mode: all-private self-transfer",
	)
}

func printPlannerTransferSubmitted(cmd *cobra.Command, txHash string) {
	privacyCommandPrintf(cmd, "Planner self-transfer accepted (tx: %s); waiting for the next block.\n", txHash)
}

func printPreparedWithdrawPayloadSaved(cmd *cobra.Command, outPath string) {
	privacyCommandPrintf(cmd, "Prepared withdraw payload saved: %s\n", outPath)
}

func printTransferScanStep(cmd *cobra.Command, step int) {
	privacyCommandPrintf(cmd, "Step %d: scanning shielded wallet notes.\n", step)
}

func printTransferBroadcastFinal(cmd *cobra.Command, step int) {
	privacyCommandPrintf(cmd, "Step %d: broadcasting the final transfer.\n", step)
}

func printTransferBroadcastSelfMerge(cmd *cobra.Command, step int, total *big.Int) {
	privacyCommandPrintf(cmd, "Step %d: broadcasting a planner self-transfer for %s.\n", step, total.String())
}

func printTransferComplete(cmd *cobra.Command, step int, txHash string) {
	privacyCommandPrintf(cmd, "Step %d: final transfer accepted (tx: %s)\n", step, txHash)
}

func printTransferWaitForBlock(cmd *cobra.Command, step int, txHash string) {
	privacyCommandPrintf(cmd, "Step %d: waiting for the next block after tx %s\n", step, txHash)
}
