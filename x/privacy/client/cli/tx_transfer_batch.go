package cli

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type transferBatchOutput struct {
	TxHash       string   `json:"txhash"`
	Height       int64    `json:"height"`
	Code         uint32   `json:"code"`
	RawLog       string   `json:"raw_log,omitempty"`
	MessageCount int      `json:"message_count"`
	Amounts      []string `json:"amounts"`
}

func CmdTransferBatch() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer-batch [shielded_address] [amount]...",
		Short: "Broadcast multiple prepared shielded transfers in one Cosmos transaction",
		Long: strings.TrimSpace(`
Broadcast several independent MsgTransfer messages in one Cosmos transaction envelope.

	This command does not run the recursive split/merge planner. Each requested amount
	must be satisfiable from the currently spendable notes without reusing an input
	note inside the same batch. Prepare enough exact or pairable notes, including
	zero-value dummy notes when a single note is used as an input.

	User disclosure uses the same shared flags as transfer:
	--privacy-policy all-private|amount|to|amount-to|from|amount-from|from-to|amount-from-to
	--disclosure-mode none|public|recipient-encrypted
			`),
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			recipientSpendPubKey, recipientViewPubKey, err := resolveTransferRecipient(args[0])
			if err != nil {
				return err
			}
			coins, err := parseTransferBatchCoins(args[1:])
			if err != nil {
				return err
			}
			config, err := resolveTransferRuntimeConfig(cmd, clientCtx)
			if err != nil {
				return err
			}
			identity, err := resolveTransferExecutionIdentity(clientCtx)
			if err != nil {
				return err
			}

			forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
			if err != nil {
				return err
			}
			notes, err := scanNotesWithOptions(clientCtx, identity.seed, scanNotesOptions{
				logWriter:   privacyCommandLogWriter(cmd),
				forceRescan: forceRescan,
			})
			if err != nil {
				return err
			}

			latencyFlow := newPrivacyLatencyFlow("transfer_batch")
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()

			msgs, err := buildTransferBatchMessages(
				cmd.Context(),
				clientCtx,
				notes,
				identity,
				recipientSpendPubKey,
				recipientViewPubKey,
				coins,
				privacytransfer.StepDisclosureConfig{
					UserPrivacyPolicy:             config.userPrivacyPolicy,
					UserDisclosureMode:            config.userDisclosureMode,
					UserDisclosureTargetPubKey:    config.userDisclosureTargetPubKey,
					UserDisclosureTargetPubKeyBz:  config.userDisclosureTargetPubKeyBz,
					AuditDisclosureTargetPubKey:   config.auditDisclosureTargetPubKey,
					AuditDisclosureTargetPubKeyBz: config.auditDisclosureTargetPubKeyBz,
					DisableSelfViewDisclosure:     config.disableSelfViewDisclosure,
				},
				privacyCommandLogWriter(cmd),
				latencyFlow,
			)
			if err != nil {
				runErr = err
				return err
			}

			broadcaster := privacyprovider.CosmosTxBroadcaster{
				ClientContext: clientCtx,
				Flags:         cmd.Flags(),
				FromName:      clientCtx.GetFromName(),
			}
			startedAt := time.Now()
			if transferBatchUsesStandardTxCLI(clientCtx) {
				runErr = broadcaster.GenerateOrBroadcast(msgs...)
				latencyFlow.recordSubmit(startedAt, "", runErr)
				return runErr
			}

			txRes, err := broadcaster.BroadcastSDKMessages(cmd.Context(), msgs...)
			txHash := ""
			if txRes != nil {
				txHash = txRes.TxHash
			}
			latencyFlow.recordSubmit(startedAt, txHash, err)
			if err != nil {
				runErr = err
				return err
			}
			if txRes.Code != 0 {
				runErr = fmt.Errorf("tx failed with code %d: %s", txRes.Code, txRes.RawLog)
				return runErr
			}

			output := transferBatchOutput{
				TxHash:       txRes.TxHash,
				Height:       txRes.Height,
				Code:         txRes.Code,
				RawLog:       txRes.RawLog,
				MessageCount: len(msgs),
				Amounts:      coinStrings(coins),
			}
			if privacyCommandOutputJSONEnabled(cmd) {
				runErr = printCommandJSON(cmd, output)
				return runErr
			}
			privacyCommandPrintf(cmd, "submitted transfer batch tx %s with %d messages\n", txRes.TxHash, len(msgs))
			return nil
		},
	}
	cmd.Flags().String(flagTransferPrivacyPolicy, transferPrivacyPolicyAllPrivate, "User disclosure policy shared by every batched transfer: all-private|amount|to|amount-to|from|amount-from|from-to|amount-from-to")
	cmd.Flags().String(flagTransferDisclosureMode, transferDisclosureModeNone, "User disclosure mode shared by every batched transfer: none|public|recipient-encrypted")
	cmd.Flags().String(flagTransferDisclosurePubKey, "", "Recipient disclosure public key hex for recipient-encrypted mode")
	cmd.Flags().Bool(flagTransferNoSelfView, false, "Disable sender self-view disclosure for every batched transfer")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before note selection")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func transferBatchUsesStandardTxCLI(clientCtx client.Context) bool {
	return clientCtx.IsAux ||
		clientCtx.GenerateOnly ||
		clientCtx.Simulate ||
		clientCtx.Offline ||
		!clientCtx.SkipConfirm
}

func parseTransferBatchCoins(args []string) ([]sdk.Coin, error) {
	coins := make([]sdk.Coin, 0, len(args))
	for _, arg := range args {
		coin, err := sdk.ParseCoinNormalized(arg)
		if err != nil {
			return nil, err
		}
		if !coin.Amount.IsPositive() {
			return nil, fmt.Errorf("transfer-batch amount must be positive: %s", arg)
		}
		if len(coins) > 0 && coin.Denom != coins[0].Denom {
			return nil, fmt.Errorf("transfer-batch currently requires one denom per tx: got %s and %s", coins[0].Denom, coin.Denom)
		}
		coins = append(coins, coin)
	}
	return coins, nil
}

func buildTransferBatchMessages(
	ctx context.Context,
	clientCtx client.Context,
	notes []FoundNote,
	identity *transferExecutionIdentity,
	recipientSpendPubKey *crypto_tedwards.PointAffine,
	recipientViewPubKey *crypto_tedwards.PointAffine,
	coins []sdk.Coin,
	disclosure privacytransfer.StepDisclosureConfig,
	logWriter io.Writer,
	latencyFlow *privacyLatencyFlow,
) ([]sdk.Msg, error) {
	if identity == nil {
		return nil, fmt.Errorf("transfer execution identity is required")
	}
	if recipientSpendPubKey == nil || recipientViewPubKey == nil {
		return nil, fmt.Errorf("recipient spend/view public keys are required")
	}
	if len(coins) == 0 {
		return nil, fmt.Errorf("at least one transfer amount is required")
	}
	if !disclosure.DisableSelfViewDisclosure && disclosure.SelfViewDisclosureTargetPubKey == nil {
		_, selfViewDisclosurePubKey, _ := deriveDisclosureKeys(identity.seed)
		disclosure.SelfViewDisclosureTargetPubKey = selfViewDisclosurePubKey
	}

	targets := make([]*big.Int, len(coins))
	for i, coin := range coins {
		targets[i] = coin.Amount.BigInt()
	}
	selections, err := privacytransfer.SelectInputBatch(notes, coins[0].Denom, targets)
	if err != nil {
		return nil, fmt.Errorf("transfer-batch input selection: %w", err)
	}

	msgs := make([]sdk.Msg, 0, len(coins))
	for i, coin := range coins {
		selection := selections[i]
		msg, err := privacytransfer.BuildTransferStepMessage(
			ctx,
			privacyprovider.NewTransferQueryProvider(privacytypes.NewQueryClient(clientCtx)),
			manualJoinSplitNoteHashSigner{scalar: identity.scalar, pubKey: identity.spendPubKey},
			transferJoinSplitArtifactProvider{},
			transferJoinSplitProofRunner{logWriter: logWriter, latencyFlow: latencyFlow},
			privacytransfer.BuildTransferStepMessageInput{
				Creator:              clientCtx.GetFromAddress().String(),
				Inputs:               selection.Inputs,
				RecipientSpendPubKey: recipientSpendPubKey,
				RecipientViewPubKey:  recipientViewPubKey,
				TransferAmount:       coin.Amount.BigInt(),
				TransferDenom:        coin.Denom,
				SenderSpendPubKey:    identity.spendPubKey,
				SenderViewPubKey:     identity.viewPubKey,
				IsFinal:              true,
				Disclosure:           disclosure,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("build batch item %d (%s): %w", i, coin.String(), err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

func removeTransferBatchInputs(notes []FoundNote, inputs [2]FoundNote) []FoundNote {
	used := map[string]struct{}{
		transferBatchFoundNoteKey(inputs[0]): {},
		transferBatchFoundNoteKey(inputs[1]): {},
	}
	out := make([]FoundNote, 0, len(notes))
	for _, note := range notes {
		if _, ok := used[transferBatchFoundNoteKey(note)]; ok {
			continue
		}
		out = append(out, note)
	}
	return out
}

func transferBatchFoundNoteKey(note FoundNote) string {
	if trimmed := strings.ToLower(strings.TrimSpace(note.Nullifier)); trimmed != "" {
		return "nullifier:" + trimmed
	}
	if transferBatchNoteCanComputeCommitment(note.Note) {
		commitment := note.Note.ComputeCommitment()
		if commitmentHex, err := privacyfield.CanonicalHexFromBigInt(commitment); err == nil {
			return "commitment:" + commitmentHex
		}
	}
	return fmt.Sprintf("fallback:%d:%s:%s", note.Height, strings.ToLower(strings.TrimSpace(note.TxHash)), note.Note.Amount.String())
}

func transferBatchNoteCanComputeCommitment(note privacytypes.Note) bool {
	return note.ReceiverSpendPubKeyX != nil &&
		note.ReceiverSpendPubKeyY != nil &&
		note.ReceiverViewPubKeyX != nil &&
		note.ReceiverViewPubKeyY != nil &&
		note.Amount != nil &&
		note.AssetID != nil &&
		note.Randomness != nil
}

func coinStrings(coins []sdk.Coin) []string {
	out := make([]string, len(coins))
	for i, coin := range coins {
		out[i] = coin.String()
	}
	return out
}
