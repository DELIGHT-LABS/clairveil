package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	gnarklogger "github.com/consensys/gnark/logger"

	"github.com/DELIGHT-LABS/clairveil/internal/privatefile"
	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
)

type FoundNote = privacyscan.SecretFoundNote

type LocalWalletData = privacyscan.LocalWalletData

type scanNotesOptions struct {
	logWriter   io.Writer
	forceRescan bool
	diagnostics *scanNotesDiagnostics
}

type listNotesJSONOutput struct {
	Summary     listNotesJSONSummary  `json:"summary"`
	Diagnostics *scanNotesDiagnostics `json:"diagnostics,omitempty"`
	Notes       []listNotesJSONNote   `json:"notes"`
}

type listNotesJSONSummary struct {
	TotalSpendable string `json:"total_spendable"`
	SpendableCount int    `json:"spendable_count"`
	SpentCount     int    `json:"spent_count"`
	TotalCount     int    `json:"total_count"`
}

type listNotesJSONNote struct {
	Index     int           `json:"index"`
	Status    string        `json:"status"`
	Amount    string        `json:"amount"`
	Nullifier string        `json:"nullifier"`
	TxHash    string        `json:"tx_hash"`
	Height    int64         `json:"height"`
	Note      displayedNote `json:"note"`
}

type shieldedAddressSummary struct {
	FromAddress string `json:"from_address"`
	Address     string `json:"address"`
	DerivedFrom string `json:"derived_from"`
	Usage       string `json:"usage"`
}

type viewingKeySummary struct {
	FromAddress        string `json:"from_address"`
	IncomingViewKeyHex string `json:"incoming_viewing_key_hex"`
	ViewPublicKeyHex   string `json:"view_public_key_hex"`
	DerivedFrom        string `json:"derived_from"`
}

type scanNotesDiagnostics struct {
	WalletPath               string `json:"wallet_path,omitempty"`
	LoadedLastHeight         int64  `json:"loaded_last_height"`
	LoadedNoteCount          int    `json:"loaded_note_count"`
	ScannedFromHeight        int64  `json:"scanned_from_height"`
	ScannedToHeight          int64  `json:"scanned_to_height"`
	ForcedRescan             bool   `json:"forced_rescan"`
	RollbackReset            bool   `json:"rollback_reset"`
	RecoveredCorruptCache    bool   `json:"recovered_corrupt_cache"`
	CorruptBackupPath        string `json:"corrupt_backup_path,omitempty"`
	CorruptBackupRenameError string `json:"corrupt_backup_rename_error,omitempty"`
	NormalizedCache          bool   `json:"normalized_cache"`
	NewNotesFound            int    `json:"new_notes_found"`
	FinalNoteCount           int    `json:"final_note_count"`
	SavedWallet              bool   `json:"saved_wallet"`
}

const preparedAuditWithdrawArtifactVersion = "audit-withdraw-v2"

type preparedAuditWithdrawArtifact struct {
	Version      string                 `json:"version"`
	Message      *privacyv2.MsgWithdraw `json:"message"`
	PublicInputs [][]byte               `json:"public_inputs"`
	ArtifactHash []byte                 `json:"artifact_hash"`
}

const (
	defaultPreparedWithdrawExpiry = 30 * time.Minute
	flagWithdrawAutoPlan          = "auto-plan"
	flagListNotesJSON             = "json"
	flagRescanWallet              = "rescan-wallet"
)

var gnarkLoggerOutputMu sync.Mutex

func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		CmdDeposit(),
		CmdWithdraw(),
		CmdPrepareWithdraw(),
		CmdRelayWithdraw(),
		CmdListNotes(),
		CmdTransfer(),
		CmdTransferBatch(),
		CmdTransferBatch16x32(),
		CmdPrepareBatchTransfer(),
		CmdProveBatchTransfer(),
		CmdBroadcastBatchTransfer(),
		CmdShowDisclosurePubKey(),
		CmdDecodeTransferDisclosure(),
		CmdShowShieldedAddress(),
		CmdShowViewingKey(),
	)

	return cmd
}

func privacyCommandOutputJSONEnabled(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	output, err := cmd.Flags().GetString(flags.FlagOutput)
	if err != nil {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(output), "json")
}

func privacyCommandOutputWriter(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return os.Stdout
	}

	return cmd.OutOrStdout()
}

func privacyCommandLogWriter(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return os.Stdout
	}

	if privacyCommandOutputJSONEnabled(cmd) {
		return cmd.ErrOrStderr()
	}

	return cmd.OutOrStdout()
}

func privacyCommandPrintf(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(privacyCommandLogWriter(cmd), format, args...)
}

func privacyCommandPrintln(cmd *cobra.Command, args ...any) {
	fmt.Fprintln(privacyCommandLogWriter(cmd), args...)
}

func privacyCommandOutputPrintf(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(privacyCommandOutputWriter(cmd), format, args...)
}

func privacyCommandOutputPrintln(cmd *cobra.Command, args ...any) {
	fmt.Fprintln(privacyCommandOutputWriter(cmd), args...)
}

func printCommandJSON(cmd *cobra.Command, value any) error {
	jsonBytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	if cmd == nil {
		fmt.Println(string(jsonBytes))
		return nil
	}

	privacyCommandOutputPrintln(cmd, string(jsonBytes))
	return nil
}

func printLabeledCommandValue(cmd *cobra.Command, label, value string) {
	privacyCommandOutputPrintf(cmd, "%s:\n%s\n", label, value)
}

func withGnarkLoggerOutput[T any](writer io.Writer, fn func() (T, error)) (T, error) {
	gnarkLoggerOutputMu.Lock()
	defer gnarkLoggerOutputMu.Unlock()

	if writer == nil {
		writer = io.Discard
	}

	prev := gnarklogger.Logger()
	gnarklogger.Set(prev.Output(writer))
	defer gnarklogger.Set(prev)

	return fn()
}

func scanNotesPrintf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}

	fmt.Fprintf(w, format, args...)
}

func consumeOneShotBool(value *bool) bool {
	if value == nil || !*value {
		return false
	}

	*value = false
	return true
}

func getExplicitKeys(clientCtx client.Context) (crypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	rootSeed, _, err := derivePrivacyRootSeed(clientCtx)
	if err != nil {
		return crypto.SecretScalar{}, nil, nil, err
	}

	scalar, pubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
	if err != nil {
		return crypto.SecretScalar{}, nil, nil, err
	}

	return scalar, pubKey, rootSeed, nil
}

func deriveScalarFromSeed(seed []byte) (crypto.SecretScalar, error) {
	return privacyidentity.DeriveScalarFromSeed(seed)
}

func derivePubKeyFromScalar(scalar crypto.SecretScalar) (*crypto_tedwards.PointAffine, error) {
	return privacyidentity.DerivePubKeyFromScalar(scalar)
}

func deriveViewKeys(rootSeed []byte) (crypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	return privacyidentity.DeriveViewKeys(rootSeed)
}

func deriveDisclosureKeys(rootSeed []byte) (crypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	return privacyidentity.DeriveDisclosureKeys(rootSeed)
}

func scalarToFixedHex(scalar crypto.SecretScalar) string {
	return privacyidentity.ScalarToFixedHex(scalar)
}

func validateCanonicalFieldBytes32(bz []byte) error {
	return privacyfield.ValidateCanonicalBytes32(bz)
}

func canonicalFieldBytesFromBigInt(v *big.Int) ([]byte, error) {
	return privacyfield.CanonicalBytesFromBigInt(v)
}

func canonicalFieldHexFromBigInt(v *big.Int) (string, error) {
	return privacyfield.CanonicalHexFromBigInt(v)
}

func circuitFieldHexFromBigInt(v *big.Int) (string, error) {
	return privacyfield.CircuitHexFromBigInt(v)
}

func decodeCanonicalFieldHex(value, fieldName string) ([]byte, error) {
	return privacyfield.DecodeCanonicalHex(value, fieldName)
}

// manualSign accepts a public intent field and an opaque signing key. The
// signer derives its own public key and rejects mismatched caller metadata.
func manualSign(msg *big.Int, scalar crypto.SecretScalar, pubKey *crypto_tedwards.PointAffine) ([]byte, error) {
	if pubKey == nil {
		return nil, fmt.Errorf("owner public key is required")
	}
	derived, err := crypto.PublicKey(scalar)
	if err != nil {
		return nil, err
	}
	if !derived.Equal(pubKey) {
		return nil, fmt.Errorf("owner public key does not match signing key")
	}
	raw, err := canonicalFieldBytesFromBigInt(msg)
	if err != nil {
		return nil, err
	}
	var digest [32]byte
	copy(digest[:], raw)
	return crypto.SignOwnerIntent(digest, scalar, nil)
}

func scanNotes(clientCtx client.Context, seed []byte) ([]FoundNote, error) {
	return scanNotesWithOptions(clientCtx, seed, scanNotesOptions{logWriter: os.Stdout})
}

func scanNotesWithOptions(clientCtx client.Context, seed []byte, opts scanNotesOptions) ([]FoundNote, error) {
	userAddr := clientCtx.GetFromAddress().String()
	if userAddr == "" {
		return nil, fmt.Errorf("a transparent --from account is required to scan shielded notes")
	}

	loadResult, err := privacyscan.LoadLocalWalletFile(clientCtx.HomeDir, userAddr)
	if err != nil {
		return nil, err
	}
	printLocalWalletLoadRecoveryWarning(os.Stderr, loadResult)

	if opts.diagnostics != nil {
		opts.diagnostics.WalletPath = loadResult.Path
		opts.diagnostics.LoadedLastHeight = loadResult.Wallet.LastHeight
		opts.diagnostics.LoadedNoteCount = len(loadResult.Wallet.Notes)
		opts.diagnostics.RecoveredCorruptCache = loadResult.CorruptBackupPath != "" || loadResult.CorruptBackupRenameErr != nil
		opts.diagnostics.CorruptBackupPath = loadResult.CorruptBackupPath
		if loadResult.CorruptBackupRenameErr != nil {
			opts.diagnostics.CorruptBackupRenameError = loadResult.CorruptBackupRenameErr.Error()
		}
	}

	scanProvider := privacyprovider.NewScanQueryProvider(clientCtx.Client, types.NewQueryClient(clientCtx))
	result, err := privacyscan.SyncNotes(
		context.Background(),
		scanProvider,
		scanProvider,
		newScanNotesObserver(opts.logWriter),
		privacyscan.SyncInput{
			UserAddress: userAddr,
			RootSeed:    seed,
			Wallet:      loadResult.Wallet,
			ForceRescan: opts.forceRescan,
		},
	)
	if err != nil {
		return nil, err
	}

	if opts.diagnostics != nil {
		opts.diagnostics.LoadedLastHeight = result.Diagnostics.LoadedLastHeight
		opts.diagnostics.LoadedNoteCount = result.Diagnostics.LoadedNoteCount
		opts.diagnostics.ScannedFromHeight = result.Diagnostics.ScannedFromHeight
		opts.diagnostics.ScannedToHeight = result.Diagnostics.ScannedToHeight
		opts.diagnostics.ForcedRescan = result.Diagnostics.ForcedRescan
		opts.diagnostics.RollbackReset = result.Diagnostics.RollbackReset
		opts.diagnostics.NormalizedCache = result.Diagnostics.NormalizedCache
		opts.diagnostics.NewNotesFound = result.Diagnostics.NewNotesFound
		opts.diagnostics.FinalNoteCount = result.Diagnostics.FinalNoteCount
	}

	if result.WalletChanged {
		if err := privacyscan.SaveLocalWalletFile(loadResult.Path, result.Wallet); err != nil {
			printLocalWalletSaveWarning(os.Stderr, err)
		} else if opts.diagnostics != nil {
			opts.diagnostics.SavedWallet = true
		}
	}

	return result.Notes, nil
}

func noteAmountString(note types.SecretNoteV1) string {
	return fmt.Sprintf("%d", note.Amount)
}

func buildListNotesJSONOutput(foundNotes []FoundNote, diagnostics *scanNotesDiagnostics) listNotesJSONOutput {
	// This total is deliberately disclosed in the user-requested wallet display.
	totalSpendable := new(big.Int)
	for _, note := range foundNotes {
		if !note.IsSpent {
			totalSpendable.Add(totalSpendable, new(big.Int).SetUint64(note.Note.Amount))
		}
	}
	output := listNotesJSONOutput{
		Summary: listNotesJSONSummary{
			TotalSpendable: totalSpendable.String(),
			TotalCount:     len(foundNotes),
		},
		Diagnostics: diagnostics,
		Notes:       make([]listNotesJSONNote, 0, len(foundNotes)),
	}

	for i, info := range foundNotes {
		status := "spendable"
		if info.IsSpent {
			status = "spent"
			output.Summary.SpentCount++
		} else {
			output.Summary.SpendableCount++
		}

		output.Notes = append(output.Notes, listNotesJSONNote{
			Index:     i + 1,
			Status:    status,
			Amount:    noteAmountString(info.Note),
			Nullifier: info.Nullifier,
			TxHash:    info.TxHash,
			Height:    info.Height,
			Note:      displayedNote{info.Note},
		})
	}

	return output
}

func autoPrepareDummyNote(cmd *cobra.Command, clientCtx client.Context, denom string) error {
	identity, err := resolveTransferExecutionIdentity(clientCtx)
	if err != nil {
		return err
	}
	// This is deliberately a one-input/two-output batch: the positive self
	// payment keeps value unchanged and one internal owner padding output is
	// the active zero dummy. User-selected output mode is not changed.
	notes, err := scanNotesWithOptions(clientCtx, identity.seed, scanNotesOptions{logWriter: privacyCommandLogWriter(cmd)})
	if err != nil {
		return err
	}
	assetID := types.ComputeAssetIDV1(denom)
	assetBytes := assetID.FillBytes(make([]byte, 32))
	asset, err := crypto.ParseFieldValueBE32(assetBytes)
	if err != nil {
		return err
	}
	var input *FoundNote
	for i := range notes {
		if notes[i].IsSpent || notes[i].Note.Amount == 0 || notes[i].Note.AssetID.Bytes() != asset.Bytes() || (notes[i].AssetDenom != "" && notes[i].AssetDenom != denom) {
			continue
		}
		input = &notes[i]
		break
	}
	if input == nil {
		return fmt.Errorf("v2 dummy preparation requires one spendable positive %s note", denom)
	}
	plan, err := planAutoDummySelfBatch(identity, input.Note)
	if err != nil {
		return fmt.Errorf("plan v2 dummy self batch: %w", err)
	}
	preparedNormal, err := privacybatchtransfer.PrepareBatchTransfer(cmd.Context(), batchTransferMerklePathProvider{inner: privacyprovider.NewTransferQueryProvider(types.NewQueryClient(clientCtx))}, plan)
	if err != nil {
		return fmt.Errorf("prepare v2 dummy self batch: %w", err)
	}
	_, selfView, _, err := deriveDisclosureKeys(identity.seed)
	if err != nil {
		return err
	}
	runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
	if err != nil {
		return err
	}
	payload, err := privacybatchtransfer.BuildPreparedAuditV2BatchTransferPayload(preparedNormal, privacybatchtransfer.BuildPreparedAuditV2BatchTransferPayloadInput{Creator: clientCtx.GetFromAddress().String(), ChainID: clientCtx.ChainID, ExpiresAtUnix: runtime.expiresAt, SelfViewDisclosureTargetPubKey: selfView})
	if err != nil {
		return err
	}
	snapshot, err := runtime.snapshotSource(cmd.Context())
	if err != nil {
		return err
	}
	prepared, err := privacybatchtransfer.PrepareAuditV2FromPreparedAuditV2Payload(snapshot, payload)
	if err != nil {
		return err
	}
	full, err := privacybatchtransfer.BuildAuditV2WitnessFromPayload(prepared, payload, func(intent *big.Int) ([]byte, error) {
		return manualSign(intent, identity.scalar, identity.spendPubKey)
	})
	if err != nil {
		prepared.Clear()
		return err
	}
	msg, err := runtime.prove(cmd, prepared, full)
	if err != nil {
		return err
	}
	batch, ok := msg.(*privacyv2.MsgBatchTransfer)
	if !ok {
		return fmt.Errorf("audit prover returned %T, expected v2 dummy batch transfer", msg)
	}
	printAutoDummyPreparationSummary(cmd, denom, fmt.Sprintf("self %d%s + one active zero padding output", input.Note.Amount, denom))
	response, err := (privacyprovider.CosmosTxBroadcaster{ClientContext: clientCtx, Flags: cmd.Flags(), FromName: clientCtx.GetFromName()}).BroadcastSDKMessage(cmd.Context(), batch)
	if err != nil {
		return err
	}
	if response == nil || response.Code != 0 {
		if response == nil {
			return fmt.Errorf("dummy batch broadcaster returned no response")
		}
		return fmt.Errorf("dummy batch tx failed with code %d: %s", response.Code, response.RawLog)
	}
	printAutoDummySubmitted(cmd, response.TxHash)
	if err := waitForBlock(clientCtx, response.Height); err != nil {
		return fmt.Errorf("waiting to rescan dummy batch: %w", err)
	}
	// The recursive caller immediately scans and reselects after this return.
	return nil
}

func planAutoDummySelfBatch(identity *transferExecutionIdentity, note types.SecretNoteV1) (*privacybatchtransfer.BatchTransferPlan, error) {
	if identity == nil || note.Amount == 0 {
		return nil, fmt.Errorf("positive owner note and identity are required")
	}
	plan, err := privacybatchtransfer.PlanBatchTransfer(privacybatchtransfer.PlanBatchTransferInput{
		Inputs:           []privacybatchtransfer.InputNote{{Note: note}},
		Payments:         []privacybatchtransfer.Payment{{SpendPubKey: identity.spendPubKey, ViewPubKey: identity.viewPubKey, Amount: new(big.Int).SetUint64(note.Amount), PrivacyPolicy: types.TransferPrivacyPolicyAllPrivate, DisclosureMode: types.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE}},
		OwnerSpendPubKey: identity.spendPubKey, OwnerViewPubKey: identity.viewPubKey, Mode: privacybatchtransfer.OutputModeCompact,
	})
	if err != nil {
		return nil, err
	}
	plan.Outputs = append(plan.Outputs, privacybatchtransfer.PlannedOutput{Kind: privacybatchtransfer.OutputPadding, SpendPubKey: identity.spendPubKey, ViewPubKey: identity.viewPubKey, Amount: big.NewInt(0), PrivacyPolicy: types.TransferPrivacyPolicyAllPrivate, DisclosureMode: types.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE})
	return plan, nil
}

func buildAuditV2WithdrawMsg(cmd *cobra.Command, clientCtx client.Context, targetCoin sdk.Coin, recipientAddr sdk.AccAddress, expiresAt time.Time, autoPlan bool, latencyFlow *privacyLatencyFlow) (sdk.Msg, error) {
	artifact, err := buildAuditV2WithdrawArtifact(cmd, clientCtx, targetCoin, recipientAddr, expiresAt, autoPlan, latencyFlow)
	if err != nil {
		return nil, err
	}
	return artifact.Message, nil
}

func buildAuditV2WithdrawArtifact(cmd *cobra.Command, clientCtx client.Context, targetCoin sdk.Coin, recipientAddr sdk.AccAddress, expiresAt time.Time, autoPlan bool, latencyFlow *privacyLatencyFlow) (*preparedAuditWithdrawArtifact, error) {
	scalar, pubKey, seed, err := getExplicitKeys(clientCtx)
	if err != nil {
		return nil, err
	}
	forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
	if err != nil {
		return nil, err
	}
	runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
	if err != nil {
		return nil, err
	}
	runtime.expiresAt = expiresAt.Unix()
	selected, err := privacywithdraw.ResolveExactMatchSpendableNote(cmd.Context(), &withdrawExactMatchNoteSource{clientCtx: clientCtx, seed: seed, logWriter: privacyCommandLogWriter(cmd), forceRescan: forceRescan}, withdrawExactMatchAutoPlanner{cmd: cmd, clientCtx: clientCtx}, targetCoin, autoPlan)
	if err != nil {
		return nil, err
	}
	legacy, err := privacywithdraw.PrepareSpendWithdraw(cmd.Context(), privacyprovider.NewWithdrawQueryProvider(types.NewQueryClient(clientCtx)), manualSpendIntentSigner{scalar: scalar, pubKey: pubKey}, privacywithdraw.PrepareSpendWithdrawInput{Note: *selected, RecipientBytes: recipientAddr.Bytes(), ChainID: clientCtx.ChainID, ExpiresAtUnix: runtime.expiresAt})
	if err != nil {
		return nil, err
	}
	snapshot, err := runtime.snapshotSource(cmd.Context())
	if err != nil {
		return nil, err
	}
	prepared, err := privacywithdraw.PrepareAuditV2FromSpend(snapshot, legacy, clientCtx.GetFromAddress().String(), recipientAddr.String(), targetCoin.String())
	if err != nil {
		return nil, err
	}
	public := prepared.PublicInputs()
	publicBytes := make([][]byte, len(public))
	for i := range public {
		publicBytes[i] = public[i].Bytes()
	}
	full, err := privacywithdraw.BuildAuditV2Witness(prepared, legacy, func(intent *big.Int) ([]byte, error) { return manualSign(intent, scalar, pubKey) })
	if err != nil {
		prepared.Clear()
		return nil, err
	}
	if latencyFlow != nil {
		latencyFlow.recordPrepareUntil(time.Now())
	}
	msg, err := runtime.prove(cmd, prepared, full)
	if err != nil {
		return nil, err
	}
	withdraw, ok := msg.(*privacyv2.MsgWithdraw)
	if !ok {
		return nil, fmt.Errorf("audit prover returned %T, expected v2 withdraw", msg)
	}
	printSelectedWithdrawNote(cmd, targetCoin.String())
	hash := snapshot.ArtifactHash
	return &preparedAuditWithdrawArtifact{Version: preparedAuditWithdrawArtifactVersion, Message: withdraw, PublicInputs: publicBytes, ArtifactHash: hash[:]}, nil
}

type manualSpendIntentSigner struct {
	scalar crypto.SecretScalar
	pubKey *crypto_tedwards.PointAffine
}

func (s manualSpendIntentSigner) SignSpendIntent(msgHash *big.Int) ([]byte, error) {
	return manualSign(msgHash, s.scalar, s.pubKey)
}

type withdrawExactMatchNoteSource struct {
	clientCtx   client.Context
	seed        []byte
	logWriter   io.Writer
	forceRescan bool
}

func (s *withdrawExactMatchNoteSource) LoadFoundNotes(_ context.Context) ([]FoundNote, error) {
	opts := scanNotesOptions{
		logWriter:   s.logWriter,
		forceRescan: consumeOneShotBool(&s.forceRescan),
	}
	return scanNotesWithOptions(s.clientCtx, s.seed, opts)
}

type withdrawExactMatchAutoPlanner struct {
	cmd       *cobra.Command
	clientCtx client.Context
}

func (p withdrawExactMatchAutoPlanner) AutoPlanExactMatchNote(_ context.Context, targetCoin sdk.Coin) error {
	return autoPlanWithdrawExactMatchNote(p.cmd, p.clientCtx, targetCoin)
}

func autoPlanWithdrawExactMatchNote(cmd *cobra.Command, clientCtx client.Context, targetCoin sdk.Coin) error {
	if cmd == nil {
		return fmt.Errorf("withdraw auto-planner requires a command context")
	}
	autoDummy, err := cmd.Flags().GetBool(flagAutoDummy)
	if err != nil {
		return err
	}

	identity, err := resolveTransferExecutionIdentity(clientCtx)
	if err != nil {
		return err
	}

	selfShieldedAddress, err := types.EncodeShieldedAddressWithView(identity.spendPubKey, identity.viewPubKey)
	if err != nil {
		return fmt.Errorf("failed to encode planner shielded address: %w", err)
	}

	printPlannerSelfTransferSummary(cmd, targetCoin.String(), selfShieldedAddress)
	expiresAtUnix, err := resolveTransferExpiresAtUnix(cmd)
	if err != nil {
		return err
	}

	res, err := executeTransferFlowWithIdentity(
		cmd,
		clientCtx,
		identity,
		identity.spendPubKey,
		identity.viewPubKey,
		targetCoin.Amount.BigInt(),
		targetCoin.Denom,
		autoDummy,
		expiresAtUnix,
		privacytransfer.StepDisclosureConfig{
			UserPrivacyPolicy:  types.TransferPrivacyPolicyAllPrivate,
			UserDisclosureMode: types.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		},
		nil,
	)
	if err != nil {
		return err
	}

	printPlannerTransferSubmitted(cmd, res.TxHash)
	if err := waitForBlock(clientCtx, res.Height); err != nil {
		return fmt.Errorf("polling failed: %w", err)
	}

	return nil
}

type scanNotesObserver struct {
	logWriter io.Writer
}

func newScanNotesObserver(logWriter io.Writer) privacyscan.SyncObserver {
	if logWriter == nil {
		return nil
	}

	return scanNotesObserver{logWriter: logWriter}
}

func (o scanNotesObserver) OnForcedRescan() {
	scanNotesPrintf(
		o.logWriter,
		"Forcing a shielded wallet rescan from genesis; clearing the local note cache first.\n",
	)
}

func (o scanNotesObserver) OnRollbackReset(cachedHeight, currentHeight int64) {
	scanNotesPrintf(
		o.logWriter,
		"Cached shielded wallet height %d is ahead of node height %d; resetting the local note cache and rescanning from genesis.\n",
		cachedHeight,
		currentHeight,
	)
}

func (o scanNotesObserver) OnSyncRange(fromHeight, toHeight int64) {
	scanNotesPrintf(o.logWriter, "Syncing notes from block %d to %d...\n", fromHeight, toHeight)
}

func (o scanNotesObserver) OnNotesFound(txHash string, count int) {
	scanNotesPrintf(o.logWriter, "Found %d new note(s) in tx %s.\n", count, txHash)
}

func CmdDeposit() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit [amount]",
		Short: "Deposit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			_, pubKey, seed, err := getExplicitKeys(clientCtx)
			if err != nil {
				return err
			}

			coin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}
			if !coin.IsPositive() {
				return fmt.Errorf("deposit requires a positive canonical amount")
			}
			runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
			if err != nil {
				return err
			}

			latencyFlow := newPrivacyLatencyFlow("deposit")
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()

			output, note, _, err := buildAuditV2DepositOutput(pubKey, seed, coin)
			if err != nil {
				runErr = err
				return err
			}
			snapshot, err := runtime.snapshotSource(cmd.Context())
			if err != nil {
				runErr = err
				return err
			}
			from := clientCtx.GetFromAddress()
			if len(from) == 0 {
				runErr = fmt.Errorf("a canonical --from account is required")
				return runErr
			}
			prepared, err := privacydeposit.PrepareAuditV2FromNormalNote(snapshot, from.String(), coin.String(), note, output, runtime.expiresAt)
			if err != nil {
				runErr = err
				return err
			}
			full, err := privacydeposit.BuildAuditV2Witness(prepared, note)
			if err != nil {
				prepared.Clear()
				runErr = err
				return err
			}
			msg, err := runtime.prove(cmd, prepared, full)
			if err != nil {
				runErr = err
				return err
			}

			submitStartedAt := time.Now()
			runErr = privacyprovider.CosmosTxBroadcaster{
				ClientContext: clientCtx,
				Flags:         cmd.Flags(),
			}.GenerateOrBroadcast(msg)
			latencyFlow.recordSubmit(submitStartedAt, "", runErr)
			return runErr
		},
	}
	addAuditV2Flags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdWithdraw() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw [amount]",
		Short: "Withdraw from shielded pool",
		Long: `Withdraw a spendable shielded note to a transparent recipient.

Default behavior:
- withdraw first looks for one spendable note that exactly matches the requested amount
- if none exists, it automatically tries to create that exact-match note with a shielded self-transfer

Current limitation:
- if the preparatory self-transfer must split one larger note, the current two-input transfer circuit may need a same-denom zero-value dummy note in the second input slot
- the CLI now auto-prepares that dummy note by default
- this command still does not build a direct change note inside the withdraw proof

Use list-notes first if you are not sure whether you already have an exact-match note.
If you want exact-match-only behavior without the planner, run with --auto-plan=false.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			recipientStr, err := cmd.Flags().GetString("recipient")
			if err != nil {
				return err
			}

			recipientAddr := clientCtx.GetFromAddress()
			if recipientStr != "" {
				recipientAddr, err = sdk.AccAddressFromBech32(recipientStr)
				if err != nil {
					return fmt.Errorf("invalid recipient: %w", err)
				}
			}

			amountStr := args[0]
			targetCoin, err := sdk.ParseCoinNormalized(amountStr)
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}
			autoPlan, err := cmd.Flags().GetBool(flagWithdrawAutoPlan)
			if err != nil {
				return err
			}
			autoDummy, err := cmd.Flags().GetBool(flagAutoDummy)
			if err != nil {
				return err
			}
			latencyFlow := newPrivacyLatencyFlow("withdraw_direct")
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()
			expiresAt := time.Now().Add(defaultPreparedWithdrawExpiry)
			printWithdrawCommandSummary(cmd, "Shielded withdraw", recipientAddr.String(), targetCoin.String(), autoPlan, autoDummy, clientCtx.ChainID, expiresAt.Unix())
			msg, err := buildAuditV2WithdrawMsg(cmd, clientCtx, targetCoin, recipientAddr, expiresAt, autoPlan, latencyFlow)
			if err != nil {
				runErr = err
				return err
			}

			submitStartedAt := time.Now()
			runErr = privacyprovider.CosmosTxBroadcaster{
				ClientContext: clientCtx,
				Flags:         cmd.Flags(),
			}.GenerateOrBroadcast(msg)
			latencyFlow.recordSubmit(submitStartedAt, "", runErr)
			return runErr
		},
	}
	cmd.Flags().String("recipient", "", "recipient public address (default: sender address)")
	cmd.Flags().Bool(flagWithdrawAutoPlan, true, "Automatically create an exact-match note with a preparatory shielded self-transfer when needed")
	cmd.Flags().Bool(flagAutoDummy, true, "Automatically create a zero-value dummy note with a preparatory deposit when the planner needs it")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before exact-match note selection")
	addAuditV2Flags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdPrepareWithdraw() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prepare-withdraw [amount]",
		Short: "Prepare withdraw payload for relayer broadcast",
		Long: `Prepare a withdraw payload for relayer broadcast.

Default behavior:
- prepare-withdraw first looks for one spendable note that exactly matches the requested amount
- if none exists, it automatically tries to create that exact-match note with a shielded self-transfer before preparing the payload

Current limitation:
- if the preparatory self-transfer must split one larger note, the current two-input transfer circuit may need a same-denom zero-value dummy note in the second input slot
- the CLI now auto-prepares that dummy note by default
- payload generation still does not build a direct change note inside the withdraw proof

Use list-notes first if you are not sure whether you already have an exact-match note.
If you want exact-match-only behavior without the planner, run with --auto-plan=false.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			recipientStr, err := cmd.Flags().GetString("recipient")
			if err != nil {
				return err
			}

			recipientAddr := clientCtx.GetFromAddress()
			if recipientStr != "" {
				recipientAddr, err = sdk.AccAddressFromBech32(recipientStr)
				if err != nil {
					return fmt.Errorf("invalid recipient: %w", err)
				}
			}
			targetCoin, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}
			autoPlan, err := cmd.Flags().GetBool(flagWithdrawAutoPlan)
			if err != nil {
				return err
			}
			autoDummy, err := cmd.Flags().GetBool(flagAutoDummy)
			if err != nil {
				return err
			}
			expiresInSec, err := cmd.Flags().GetInt64("expires-in")
			if err != nil {
				return err
			}
			if expiresInSec <= 0 {
				return fmt.Errorf("expires-in must be positive")
			}

			latencyFlow := newPrivacyLatencyFlow("relayed_withdraw_prepare")
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()

			expiresAt := time.Now().Add(time.Duration(expiresInSec) * time.Second)
			printWithdrawCommandSummary(cmd, "Prepare shielded withdraw", recipientAddr.String(), targetCoin.String(), autoPlan, autoDummy, clientCtx.ChainID, expiresAt.Unix())

			artifact, err := buildAuditV2WithdrawArtifact(cmd, clientCtx, targetCoin, recipientAddr, expiresAt, autoPlan, latencyFlow)
			if err != nil {
				runErr = err
				return err
			}

			outPath, err := cmd.Flags().GetString("out")
			if err != nil {
				return err
			}

			if outPath != "" {
				encoded, err := json.MarshalIndent(artifact, "", "  ")
				if err != nil {
					return err
				}
				if err := privatefile.Write(outPath, encoded); err != nil {
					runErr = err
					return err
				}
				printPreparedWithdrawPayloadSaved(cmd, outPath)
			}

			runErr = printCommandJSON(cmd, artifact)
			return runErr
		},
	}

	cmd.Flags().String("recipient", "", "recipient public address (default: sender address)")
	cmd.Flags().String("out", "", "output file path for prepared payload")
	cmd.Flags().Int64("expires-in", int64(defaultPreparedWithdrawExpiry/time.Second), "prepared payload validity window in seconds")
	cmd.Flags().Bool(flagWithdrawAutoPlan, true, "Automatically create an exact-match note with a preparatory shielded self-transfer when needed")
	cmd.Flags().Bool(flagAutoDummy, true, "Automatically create a zero-value dummy note with a preparatory deposit when the planner needs it")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before exact-match note selection")
	addAuditV2Flags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdRelayWithdraw() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay-withdraw [payload-file]",
		Short: "Relay prepared withdraw payload (supports standard tx fee-granter flags)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			latencyFlow := newPrivacyLatencyFlow("relayed_withdraw_relay")
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				runErr = err
				return err
			}

			prepareStartedAt := time.Now()
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var artifact preparedAuditWithdrawArtifact
			if err := json.Unmarshal(raw, &artifact); err != nil {
				return fmt.Errorf("invalid v2 withdraw artifact: %w", err)
			}
			if artifact.Version != preparedAuditWithdrawArtifactVersion || artifact.Message == nil || len(artifact.ArtifactHash) != 32 {
				return fmt.Errorf("unsupported withdraw artifact; regenerate it with prepare-withdraw")
			}
			if time.Now().Unix() >= artifact.Message.ExpiresAtUnix {
				return fmt.Errorf("prepared withdraw artifact expired; prepare again")
			}
			if _, err := types.ValidateAuditMessage(artifact.Message); err != nil {
				return fmt.Errorf("invalid v2 withdraw artifact: %w", err)
			}
			runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
			if err != nil {
				return err
			}
			snapshot, err := runtime.snapshotSource(cmd.Context())
			if err != nil {
				return err
			}
			keyID := snapshot.Key.IDBytes()
			if snapshot.Epoch != artifact.Message.Audit.Epoch || !bytes.Equal(keyID[:], artifact.Message.Audit.KeyId) || !bytes.Equal(snapshot.ArtifactHash[:], artifact.ArtifactHash) {
				return fmt.Errorf("prepared withdraw artifact is stale; prepare again")
			}
			if err := privacyaudit.VerifyFinalMessageBinding(snapshot, artifact.Message, artifact.PublicInputs); err != nil {
				return fmt.Errorf("prepared withdraw message/proof binding failed: %w", err)
			}
			err = privacyaudit.VerifyFinalProof(2, artifact.PublicInputs, artifact.Message.Proof, runtime.registry, runtime.identity)
			latencyFlow.recordPhase("prepare", prepareStartedAt, err)
			if err != nil {
				runErr = err
				return err
			}
			msg := *artifact.Message
			msg.Creator = clientCtx.GetFromAddress().String()

			submitStartedAt := time.Now()
			runErr = privacyprovider.CosmosTxBroadcaster{
				ClientContext: clientCtx,
				Flags:         cmd.Flags(),
			}.GenerateOrBroadcast(&msg)
			latencyFlow.recordSubmit(submitStartedAt, "", runErr)
			return runErr
		},
	}
	addAuditV2Flags(cmd)

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdListNotes() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-notes",
		Short: "Scan blockchain for my notes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			_, _, seed, err := getExplicitKeys(clientCtx)
			if err != nil {
				return err
			}

			jsonOutput, err := cmd.Flags().GetBool(flagListNotesJSON)
			if err != nil {
				return err
			}
			forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
			if err != nil {
				return err
			}

			if !jsonOutput {
				printListNotesScanStart(cmd)
			}

			diagnostics := &scanNotesDiagnostics{}
			opts := scanNotesOptions{
				logWriter:   privacyCommandOutputWriter(cmd),
				forceRescan: forceRescan,
				diagnostics: diagnostics,
			}
			if jsonOutput {
				opts.logWriter = nil
			}
			foundNotes, err := scanNotesWithOptions(clientCtx, seed, opts)
			if err != nil {
				return err
			}

			if jsonOutput {
				return printCommandJSON(cmd, buildListNotesJSONOutput(foundNotes, diagnostics))
			}

			fmt.Fprint(privacyCommandOutputWriter(cmd), renderListNotesText(foundNotes))
			return nil
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().Bool(flagListNotesJSON, false, "output notes as a machine-readable JSON document")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before listing notes")
	return cmd
}

func CmdShowShieldedAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-address",
		Short: "Show my shielded address (starts with clairs1...)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			rootSeed, fromAddress, err := derivePrivacyRootSeed(clientCtx)
			if err != nil {
				return err
			}
			_, spendPubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
			if err != nil {
				return err
			}
			_, viewPubKey, _, err := deriveViewKeys(rootSeed)
			if err != nil {
				return err
			}

			addrStr, err := types.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
			if err != nil {
				return err
			}

			summary := shieldedAddressSummary{
				FromAddress: fromAddress.String(),
				Address:     addrStr,
				DerivedFrom: "transparent-keyring-root",
				Usage:       "share this full shielded address when someone needs to send you private funds",
			}
			if privacyCommandOutputJSONEnabled(cmd) {
				return printCommandJSON(cmd, summary)
			}

			printShieldedAddressSummary(cmd, addrStr)
			return nil
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdShowViewingKey() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-view-key",
		Short: "Show incoming viewing key and view public key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			rootSeed, fromAddress, err := derivePrivacyRootSeed(clientCtx)
			if err != nil {
				return err
			}

			viewScalar, viewPubKey, _, err := deriveViewKeys(rootSeed)
			if err != nil {
				return err
			}
			viewPubKeyHex := encodePointHex(viewPubKey)
			summary := viewingKeySummary{
				FromAddress:        fromAddress.String(),
				IncomingViewKeyHex: scalarToFixedHex(viewScalar),
				ViewPublicKeyHex:   viewPubKeyHex,
				DerivedFrom:        "transparent-keyring-root",
			}
			if privacyCommandOutputJSONEnabled(cmd) {
				return printCommandJSON(cmd, summary)
			}

			printViewingKeySummary(cmd, summary.IncomingViewKeyHex, summary.ViewPublicKeyHex)
			return nil
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func removeQuotes(s string) string {
	if len(s) > 0 && s[0] == '"' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == '"' {
		s = s[:len(s)-1]
	}
	return s
}

func encodePointHex(pubKey *crypto_tedwards.PointAffine) string {
	if pubKey == nil {
		return ""
	}
	bz := pubKey.Bytes()
	return hex.EncodeToString(bz[:])
}
