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

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	flagAutoDummy              = "auto-dummy"
	flagTransferDisclosureMode = "disclosure-mode"
	maxTransferPlanSteps       = 12

	transferDisclosureModeNone               = "none"
	transferDisclosureModePublic             = "public"
	transferDisclosureModeRecipientEncrypted = "recipient-encrypted"
)

type transferRuntimeConfig struct {
	userPrivacyPolicy             uint32
	userDisclosureMode            types.UserDisclosureMode
	userDisclosureTargetPubKey    *crypto_tedwards.PointAffine
	userDisclosureTargetPubKeyBz  []byte
	auditDisclosureTargetPubKey   *crypto_tedwards.PointAffine
	auditDisclosureTargetPubKeyBz []byte
}

func CmdTransfer() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer [shielded_address] [amount]",
		Short: "Latest shielded transfer with optional user disclosure and mandatory audit disclosure",
		Long: strings.TrimSpace(`
Use the single latest shielded transfer flow.

- The transfer itself stays private on-chain.
- User disclosure is optional and controlled with --privacy-policy and --disclosure-mode.
- Audit disclosure is always attached and is encrypted to the chain-configured audit key.
- Recipient addresses must be full shielded addresses.
- A zero-value dummy note is only needed when the current two-input transfer circuit must split one larger note.
- With the default --auto-dummy=true, the CLI prepares that dummy note automatically.
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			targetAddrStr := args[0]
			recipientSpendPubKey, recipientViewPubKey, err := resolveTransferRecipient(targetAddrStr)
			if err != nil {
				return err
			}

			targetCoin, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return err
			}
			targetAmount := targetCoin.Amount.BigInt()

			config, err := resolveTransferRuntimeConfig(cmd, clientCtx)
			if err != nil {
				return err
			}
			autoDummy, err := cmd.Flags().GetBool(flagAutoDummy)
			if err != nil {
				return err
			}

			printTransferCommandSummary(
				cmd,
				targetAddrStr,
				targetCoin.String(),
				policyLabel(config.userPrivacyPolicy),
				userDisclosureModeLabel(config.userDisclosureMode),
				autoDummy,
			)

			txRes, err := executeTransferFlow(
				cmd,
				clientCtx,
				recipientSpendPubKey,
				recipientViewPubKey,
				targetAmount,
				targetCoin.Denom,
				autoDummy,
				privacytransfer.StepDisclosureConfig{
					UserPrivacyPolicy:             config.userPrivacyPolicy,
					UserDisclosureMode:            config.userDisclosureMode,
					UserDisclosureTargetPubKey:    config.userDisclosureTargetPubKey,
					UserDisclosureTargetPubKeyBz:  config.userDisclosureTargetPubKeyBz,
					AuditDisclosureTargetPubKey:   config.auditDisclosureTargetPubKey,
					AuditDisclosureTargetPubKeyBz: config.auditDisclosureTargetPubKeyBz,
				},
			)
			if err != nil {
				return err
			}
			if privacyCommandOutputJSONEnabled(cmd) {
				return printCommandJSON(cmd, txRes)
			}
			return nil
		},
	}
	cmd.Flags().String(flagTransferPrivacyPolicy, transferPrivacyPolicyAllPrivate, "User disclosure policy: all-private|amount|to|amount-to|from|amount-from|from-to|amount-from-to")
	cmd.Flags().String(flagTransferDisclosureMode, transferDisclosureModeNone, "User disclosure mode: none|public|recipient-encrypted")
	cmd.Flags().String(flagTransferDisclosurePubKey, "", "Recipient disclosure public key hex for recipient-encrypted mode")
	cmd.Flags().Bool(flagAutoDummy, true, "Automatically create a zero-value dummy note with a preparatory deposit when a single-note split requires it")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before planner note selection")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func resolveTransferRuntimeConfig(cmd *cobra.Command, clientCtx client.Context) (*transferRuntimeConfig, error) {
	rawPolicy, err := cmd.Flags().GetString(flagTransferPrivacyPolicy)
	if err != nil {
		return nil, err
	}

	rawMode, err := cmd.Flags().GetString(flagTransferDisclosureMode)
	if err != nil {
		return nil, err
	}

	disclosurePubKeyHex, err := cmd.Flags().GetString(flagTransferDisclosurePubKey)
	if err != nil {
		return nil, err
	}

	config, err := privacytransfer.ResolveRuntimeConfig(
		context.Background(),
		privacyprovider.NewTransferQueryProvider(types.NewQueryClient(clientCtx)),
		privacytransfer.ResolveRuntimeConfigInput{
			RawPolicy:           rawPolicy,
			RawDisclosureMode:   rawMode,
			DisclosurePubKeyHex: disclosurePubKeyHex,
		},
	)
	if err != nil {
		return nil, err
	}

	return &transferRuntimeConfig{
		userPrivacyPolicy:             config.UserPrivacyPolicy,
		userDisclosureMode:            config.UserDisclosureMode,
		userDisclosureTargetPubKey:    config.UserDisclosureTargetPubKey,
		userDisclosureTargetPubKeyBz:  config.UserDisclosureTargetPubKeyBz,
		auditDisclosureTargetPubKey:   config.AuditDisclosureTargetPubKey,
		auditDisclosureTargetPubKeyBz: config.AuditDisclosureTargetPubKeyBz,
	}, nil
}

func queryAuditDisclosureTarget(clientCtx client.Context) (*crypto_tedwards.PointAffine, []byte, error) {
	return privacytransfer.ResolveAuditDisclosureTarget(
		context.Background(),
		privacyprovider.NewTransferQueryProvider(types.NewQueryClient(clientCtx)),
	)
}

type transferExecutionIdentity struct {
	scalar      *big.Int
	spendPubKey *crypto_tedwards.PointAffine
	viewPubKey  *crypto_tedwards.PointAffine
	seed        []byte
}

func resolveTransferExecutionIdentity(clientCtx client.Context) (*transferExecutionIdentity, error) {
	scalar, spendPubKey, seed, err := getExplicitKeys(clientCtx)
	if err != nil {
		return nil, err
	}
	_, viewPubKey, _ := deriveViewKeys(seed)

	return &transferExecutionIdentity{
		scalar:      scalar,
		spendPubKey: spendPubKey,
		viewPubKey:  viewPubKey,
		seed:        seed,
	}, nil
}

func executeTransferFlow(
	cmd *cobra.Command,
	clientCtx client.Context,
	finalRecipientSpend *crypto_tedwards.PointAffine,
	finalRecipientView *crypto_tedwards.PointAffine,
	targetAmount *big.Int,
	targetDenom string,
	autoDummy bool,
	disclosure privacytransfer.StepDisclosureConfig,
) (*sdk.TxResponse, error) {
	identity, err := resolveTransferExecutionIdentity(clientCtx)
	if err != nil {
		return nil, err
	}

	return executeTransferFlowWithIdentity(
		cmd,
		clientCtx,
		identity,
		finalRecipientSpend,
		finalRecipientView,
		targetAmount,
		targetDenom,
		autoDummy,
		disclosure,
	)
}

func executeTransferFlowWithIdentity(
	cmd *cobra.Command,
	clientCtx client.Context,
	identity *transferExecutionIdentity,
	finalRecipientSpend *crypto_tedwards.PointAffine,
	finalRecipientView *crypto_tedwards.PointAffine,
	targetAmount *big.Int,
	targetDenom string,
	autoDummy bool,
	disclosure privacytransfer.StepDisclosureConfig,
) (*sdk.TxResponse, error) {
	if identity == nil {
		return nil, fmt.Errorf("transfer execution identity is required")
	}
	forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
	if err != nil {
		return nil, err
	}

	return privacytransfer.ExecuteTransfer(
		cmd.Context(),
		&transferRecursiveNoteSource{
			clientCtx:   clientCtx,
			seed:        identity.seed,
			logWriter:   privacyCommandLogWriter(cmd),
			forceRescan: forceRescan,
		},
		transferRecursiveDummyPreparer{
			cmd:       cmd,
			clientCtx: clientCtx,
		},
		transferRecursiveBlockWaiter{clientCtx: clientCtx},
		transferRecursiveObserver{cmd: cmd},
		privacytransfer.ExecuteTransferDependencies{
			MerklePaths: privacyprovider.NewTransferQueryProvider(types.NewQueryClient(clientCtx)),
			Signer:      manualJoinSplitNoteHashSigner{scalar: identity.scalar, pubKey: identity.spendPubKey},
			Artifacts:   transferJoinSplitArtifactProvider{},
			Runner:      transferJoinSplitProofRunner{logWriter: privacyCommandLogWriter(cmd)},
			Broadcaster: transferMessageBroadcaster{
				broadcaster: privacyprovider.CosmosTxBroadcaster{
					ClientContext: clientCtx,
					Flags:         cmd.Flags(),
					FromName:      clientCtx.GetFromName(),
				},
			},
		},
		privacytransfer.ExecuteTransferInput{
			Creator:              clientCtx.GetFromAddress().String(),
			RecipientSpendPubKey: finalRecipientSpend,
			RecipientViewPubKey:  finalRecipientView,
			SenderSpendPubKey:    identity.spendPubKey,
			SenderViewPubKey:     identity.viewPubKey,
			TransferAmount:       targetAmount,
			TransferDenom:        targetDenom,
			Disclosure:           disclosure,
			StartStep:            1,
			MaxSteps:             maxTransferPlanSteps,
			AutoDummy:            autoDummy,
		},
	)
}

type transferRecursiveNoteSource struct {
	clientCtx   client.Context
	seed        []byte
	logWriter   io.Writer
	forceRescan bool
}

func (s *transferRecursiveNoteSource) LoadFoundNotes(_ context.Context) ([]FoundNote, error) {
	opts := scanNotesOptions{
		logWriter:   s.logWriter,
		forceRescan: consumeOneShotBool(&s.forceRescan),
	}
	return scanNotesWithOptions(s.clientCtx, s.seed, opts)
}

type transferRecursiveDummyPreparer struct {
	cmd       *cobra.Command
	clientCtx client.Context
}

func (p transferRecursiveDummyPreparer) PrepareDummyNote(_ context.Context, denom string) error {
	return autoPrepareDummyNote(p.cmd, p.clientCtx, denom)
}

type transferRecursiveBlockWaiter struct {
	clientCtx client.Context
}

func (w transferRecursiveBlockWaiter) WaitForNextBlock(_ context.Context, currentHeight int64) error {
	return waitForBlock(w.clientCtx, currentHeight)
}

type transferRecursiveObserver struct {
	cmd *cobra.Command
}

func (o transferRecursiveObserver) OnScan(step int) {
	printTransferScanStep(o.cmd, step)
}

func (o transferRecursiveObserver) OnBroadcastFinal(step int) {
	printTransferBroadcastFinal(o.cmd, step)
}

func (o transferRecursiveObserver) OnBroadcastSelfMerge(step int, total *big.Int) {
	printTransferBroadcastSelfMerge(o.cmd, step, total)
}

func (o transferRecursiveObserver) OnTransferComplete(step int, txHash string) {
	printTransferComplete(o.cmd, step, txHash)
}

func (o transferRecursiveObserver) OnWaitForBlock(step int, txHash string, _ int64) {
	printTransferWaitForBlock(o.cmd, step, txHash)
}

func selectInputs(notes []FoundNote, targetDenom string, target *big.Int) ([2]FoundNote, *big.Int, bool, bool) {
	selection := privacytransfer.SelectInputs(notes, targetDenom, target)
	return selection.Inputs, selection.Total, selection.IsFinal, selection.NeedsZeroDummy
}

func resolveTransferRecipient(targetAddrStr string) (*crypto_tedwards.PointAffine, *crypto_tedwards.PointAffine, error) {
	return privacytransfer.ResolveRecipient(targetAddrStr)
}

type manualJoinSplitNoteHashSigner struct {
	scalar *big.Int
	pubKey *crypto_tedwards.PointAffine
}

func (s manualJoinSplitNoteHashSigner) SignNoteHash(msgHash *big.Int) ([]byte, error) {
	return manualSign(msgHash, s.scalar, s.pubKey)
}

func waitForBlock(clientCtx client.Context, currentHeight int64) error {
	_ = clientCtx
	_ = currentHeight
	time.Sleep(8 * time.Second)
	return nil
}

type transferJoinSplitArtifactProvider struct{}

func (transferJoinSplitArtifactProvider) JoinSplitR1CS() (constraint.ConstraintSystem, error) {
	return zk.GetJoinSplitR1CS()
}

func (transferJoinSplitArtifactProvider) JoinSplitProvingKey() (groth16.ProvingKey, error) {
	return zk.GetJoinSplitProvingKey()
}

type transferJoinSplitProofRunner struct {
	logWriter io.Writer
}

func (r transferJoinSplitProofRunner) ProveJoinSplit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, joinSplitWitness witness.Witness) (groth16.Proof, error) {
	return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
		return groth16.Prove(r1cs, provingKey, joinSplitWitness)
	})
}

type transferMessageBroadcaster struct {
	broadcaster privacyprovider.CosmosTxBroadcaster
}

func (b transferMessageBroadcaster) BroadcastTransferMessage(ctx context.Context, msg *types.MsgTransfer) (*sdk.TxResponse, error) {
	return b.broadcaster.BroadcastSDKMessage(ctx, msg)
}

func findZeroNote(notes []FoundNote, excludeIndex int) int {
	return privacytransfer.FindZeroNote(notes, excludeIndex)
}
