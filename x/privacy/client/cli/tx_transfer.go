package cli

import (
	"context"
	"fmt"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
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
	flagTransferNoSelfView     = "no-self-view"
	flagTransferExpiresIn      = "expires-in"
	maxTransferPlanSteps       = 12

	transferDisclosureModeNone               = "none"
	transferDisclosureModePublic             = "public"
	transferDisclosureModeRecipientEncrypted = "recipient-encrypted"
)

type transferRuntimeConfig struct {
	userPrivacyPolicy            uint32
	userDisclosureMode           types.UserDisclosureMode
	userDisclosureTargetPubKey   *crypto_tedwards.PointAffine
	userDisclosureTargetPubKeyBz []byte
	disableSelfViewDisclosure    bool
}

func transferLatencyFlowProfile(userPrivacyPolicy uint32) string {
	if userPrivacyPolicy == types.TransferPrivacyPolicyAllPrivate {
		return "transfer_all_private"
	}
	return "transfer_with_disclosure"
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
			expiresAtUnix, err := resolveTransferExpiresAtUnix(cmd)
			if err != nil {
				return err
			}

			printTransferCommandSummary(
				cmd,
				targetAddrStr,
				targetCoin.String(),
				policyLabel(config.userPrivacyPolicy),
				userDisclosureModeLabel(config.userDisclosureMode),
				!config.disableSelfViewDisclosure,
				autoDummy,
				clientCtx.ChainID,
				expiresAtUnix,
			)

			latencyFlow := newPrivacyLatencyFlow(transferLatencyFlowProfile(config.userPrivacyPolicy))
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()

			txRes, err := executeTransferFlow(
				cmd,
				clientCtx,
				recipientSpendPubKey,
				recipientViewPubKey,
				targetAmount,
				targetCoin.Denom,
				autoDummy,
				expiresAtUnix,
				privacytransfer.StepDisclosureConfig{
					UserPrivacyPolicy:            config.userPrivacyPolicy,
					UserDisclosureMode:           config.userDisclosureMode,
					UserDisclosureTargetPubKey:   config.userDisclosureTargetPubKey,
					UserDisclosureTargetPubKeyBz: config.userDisclosureTargetPubKeyBz,
					DisableSelfViewDisclosure:    config.disableSelfViewDisclosure,
				},
				latencyFlow,
			)
			if err != nil {
				runErr = err
				return err
			}
			if privacyCommandOutputJSONEnabled(cmd) {
				runErr = printCommandJSON(cmd, txRes)
				return runErr
			}
			return nil
		},
	}
	cmd.Flags().String(flagTransferPrivacyPolicy, transferPrivacyPolicyAllPrivate, "User disclosure policy: all-private|amount|to|amount-to|from|amount-from|from-to|amount-from-to")
	cmd.Flags().String(flagTransferDisclosureMode, transferDisclosureModeNone, "User disclosure mode: none|public|recipient-encrypted")
	cmd.Flags().String(flagTransferDisclosurePubKey, "", "Recipient disclosure public key hex for recipient-encrypted mode")
	cmd.Flags().Bool(flagTransferNoSelfView, false, "Disable sender self-view disclosure for this transfer")
	cmd.Flags().Bool(flagAutoDummy, true, "Automatically create a zero-value dummy note with a self batch transfer using a spendable positive note of the same denom when a single-note split requires it")
	cmd.Flags().Int64(flagTransferExpiresIn, int64(defaultPreparedWithdrawExpiry/time.Second), "owner intent validity window in seconds")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before planner note selection")
	addAuditV2Flags(cmd)
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
	disableSelfViewDisclosure, err := cmd.Flags().GetBool(flagTransferNoSelfView)
	if err != nil {
		return nil, err
	}

	config, err := privacytransfer.ResolveAuditV2RuntimeConfig(privacytransfer.ResolveRuntimeConfigInput{
		RawPolicy:           rawPolicy,
		RawDisclosureMode:   rawMode,
		DisclosurePubKeyHex: disclosurePubKeyHex,
	})
	if err != nil {
		return nil, err
	}

	return &transferRuntimeConfig{
		userPrivacyPolicy:            config.UserPrivacyPolicy,
		userDisclosureMode:           config.UserDisclosureMode,
		userDisclosureTargetPubKey:   config.UserDisclosureTargetPubKey,
		userDisclosureTargetPubKeyBz: config.UserDisclosureTargetPubKeyBz,
		disableSelfViewDisclosure:    disableSelfViewDisclosure,
	}, nil
}

type transferExecutionIdentity struct {
	scalar      privacycrypto.SecretScalar
	spendPubKey *crypto_tedwards.PointAffine
	viewPubKey  *crypto_tedwards.PointAffine
	seed        []byte
}

func resolveTransferExecutionIdentity(clientCtx client.Context) (*transferExecutionIdentity, error) {
	scalar, spendPubKey, seed, err := getExplicitKeys(clientCtx)
	if err != nil {
		return nil, err
	}
	_, viewPubKey, _, err := deriveViewKeys(seed)
	if err != nil {
		return nil, err
	}

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
	expiresAtUnix int64,
	disclosure privacytransfer.StepDisclosureConfig,
	latencyFlow *privacyLatencyFlow,
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
		expiresAtUnix,
		disclosure,
		latencyFlow,
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
	expiresAtUnix int64,
	disclosure privacytransfer.StepDisclosureConfig,
	latencyFlow *privacyLatencyFlow,
) (*sdk.TxResponse, error) {
	if identity == nil {
		return nil, fmt.Errorf("transfer execution identity is required")
	}
	if !disclosure.DisableSelfViewDisclosure && disclosure.SelfViewDisclosureTargetPubKey == nil {
		_, selfViewDisclosurePubKey, _, err := deriveDisclosureKeys(identity.seed)
		if err != nil {
			return nil, err
		}
		disclosure.SelfViewDisclosureTargetPubKey = selfViewDisclosurePubKey
	}
	forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
	if err != nil {
		return nil, err
	}
	runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
	if err != nil {
		return nil, err
	}
	runtime.expiresAt = expiresAtUnix
	executor := &auditTransferStepExecutor{cmd: cmd, clientCtx: clientCtx, identity: identity, runtime: runtime, disclosure: disclosure, denom: targetDenom, latencyFlow: latencyFlow}
	_, err = privacytransfer.ExecuteRecursiveTransfer(
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
		executor,
		transferRecursiveBlockWaiter{clientCtx: clientCtx},
		transferRecursiveObserver{cmd: cmd},
		privacytransfer.ExecuteRecursiveTransferInput{
			FinalRecipientSpendPubKey: finalRecipientSpend,
			FinalRecipientViewPubKey:  finalRecipientView,
			SelfSpendPubKey:           identity.spendPubKey,
			SelfViewPubKey:            identity.viewPubKey,
			TargetAmount:              targetAmount,
			TargetDenom:               targetDenom,
			StartStep:                 1,
			MaxSteps:                  maxTransferPlanSteps,
			AutoDummy:                 autoDummy,
		},
	)
	if err != nil {
		return nil, err
	}
	if executor.lastResponse == nil {
		return nil, fmt.Errorf("recursive transfer did not return a final tx response")
	}
	return executor.lastResponse, nil
}

type auditTransferStepExecutor struct {
	cmd          *cobra.Command
	clientCtx    client.Context
	identity     *transferExecutionIdentity
	runtime      *auditV2Runtime
	disclosure   privacytransfer.StepDisclosureConfig
	denom        string
	latencyFlow  *privacyLatencyFlow
	lastResponse *sdk.TxResponse
}

func (e *auditTransferStepExecutor) ExecuteTransferStep(ctx context.Context, decision *privacytransfer.RecursivePlannerDecision) (*privacytransfer.RecursiveTransferTxResult, error) {
	if e == nil || decision == nil {
		return nil, fmt.Errorf("audit transfer step is required")
	}
	disclosure := privacytransfer.EffectiveStepDisclosureConfig(e.disclosure, decision.IsFinal)
	snapshot, err := e.runtime.snapshotSource(ctx)
	if err != nil {
		return nil, err
	}
	prepared, full, err := privacytransfer.PrepareAuditV2Transfer(ctx, privacyprovider.NewTransferQueryProvider(types.NewQueryClient(e.clientCtx)), snapshot,
		e.clientCtx.GetFromAddress().String(), e.runtime.expiresAt, privacytransfer.PrepareJoinSplitInput{
			Inputs: decision.Inputs, RecipientSpendPubKey: decision.RecipientSpendPubKey, RecipientViewPubKey: decision.RecipientViewPubKey,
			TransferAmount: decision.SendAmount, SenderSpendPubKey: e.identity.spendPubKey, SenderViewPubKey: e.identity.viewPubKey,
		}, e.denom, privacytransfer.AuditV2DisclosureConfig{
			UserPrivacyPolicy: disclosure.UserPrivacyPolicy, UserDisclosureMode: disclosure.UserDisclosureMode,
			UserDisclosureTargetPubKey: disclosure.UserDisclosureTargetPubKey, UserDisclosureTargetPubKeyBz: disclosure.UserDisclosureTargetPubKeyBz,
			DisableSelfViewDisclosure: disclosure.DisableSelfViewDisclosure, SelfViewDisclosureTargetPubKey: disclosure.SelfViewDisclosureTargetPubKey,
		}, func(intent *big.Int) ([]byte, error) {
			return manualSign(intent, e.identity.scalar, e.identity.spendPubKey)
		})
	if err != nil {
		return nil, err
	}
	msg, err := e.runtime.prove(e.cmd, prepared, full)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now()
	response, err := (privacyprovider.CosmosTxBroadcaster{ClientContext: e.clientCtx, Flags: e.cmd.Flags(), FromName: e.clientCtx.GetFromName()}).BroadcastSDKMessage(ctx, msg)
	txHash := ""
	if response != nil {
		txHash = response.TxHash
	}
	e.latencyFlow.recordSubmit(startedAt, txHash, err)
	if err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("tx failed with code %d: %s", response.Code, response.RawLog)
	}
	e.lastResponse = response
	return &privacytransfer.RecursiveTransferTxResult{TxHash: response.TxHash, Height: response.Height}, nil
}

func resolveTransferExpiresAtUnix(cmd *cobra.Command) (int64, error) {
	expiresInSeconds, err := cmd.Flags().GetInt64(flagTransferExpiresIn)
	if err != nil {
		return 0, err
	}
	if expiresInSeconds <= 0 {
		return 0, fmt.Errorf("expires-in must be positive")
	}
	return time.Now().Add(time.Duration(expiresInSeconds) * time.Second).Unix(), nil
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

type manualTransferOwnerIntentSigner struct {
	scalar privacycrypto.SecretScalar
	pubKey *crypto_tedwards.PointAffine
}

func (s manualTransferOwnerIntentSigner) SignOwnerIntent(request privacytransfer.JoinSplitOwnerIntentSigningRequestV1) ([]byte, error) {
	return privacytransfer.SignValidatedJoinSplitOwnerIntentV1(request, func(msgHash *big.Int) ([]byte, error) {
		return manualSign(msgHash, s.scalar, s.pubKey)
	})
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
	logWriter   io.Writer
	latencyFlow *privacyLatencyFlow
}

func (r transferJoinSplitProofRunner) ProveJoinSplit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, joinSplitWitness witness.Witness) (groth16.Proof, error) {
	if r.latencyFlow != nil {
		r.latencyFlow.recordPrepareUntil(time.Now())
	}
	return observePrivacyLatencyPhase(r.latencyFlow, "proof", func() (groth16.Proof, error) {
		return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
			return groth16.Prove(r1cs, provingKey, joinSplitWitness)
		})
	})
}

type transferMessageBroadcaster struct {
	broadcaster privacyprovider.CosmosTxBroadcaster
	latencyFlow *privacyLatencyFlow
}

func (b transferMessageBroadcaster) BroadcastTransferMessage(ctx context.Context, msg *types.MsgTransfer) (*sdk.TxResponse, error) {
	startedAt := time.Now()
	res, err := b.broadcaster.BroadcastSDKMessage(ctx, msg)
	txHash := ""
	if res != nil {
		txHash = res.TxHash
	}
	b.latencyFlow.recordSubmit(startedAt, txHash, err)
	return res, err
}

func findZeroNote(notes []FoundNote, excludeIndex int) int {
	return privacytransfer.FindZeroNote(notes, excludeIndex)
}
