package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
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

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	gnarklogger "github.com/consensys/gnark/logger"

	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type FoundNote = privacyscan.FoundNote

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
	Index     int        `json:"index"`
	Status    string     `json:"status"`
	Amount    string     `json:"amount"`
	Nullifier string     `json:"nullifier"`
	TxHash    string     `json:"tx_hash"`
	Height    int64      `json:"height"`
	Note      types.Note `json:"note"`
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

type PreparedWithdrawPayload = privacywithdraw.PreparedWithdrawPayload

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

func getExplicitKeys(clientCtx client.Context) (*big.Int, *crypto_tedwards.PointAffine, []byte, error) {
	rootSeed, _, err := derivePrivacyRootSeed(clientCtx)
	if err != nil {
		return nil, nil, nil, err
	}

	scalar, pubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)

	return scalar, pubKey, rootSeed, nil
}

func deriveScalarFromSeed(seed []byte) *big.Int {
	return privacyidentity.DeriveScalarFromSeed(seed)
}

func derivePubKeyFromScalar(scalar *big.Int) *crypto_tedwards.PointAffine {
	return privacyidentity.DerivePubKeyFromScalar(scalar)
}

func deriveViewKeys(rootSeed []byte) (*big.Int, *crypto_tedwards.PointAffine, []byte) {
	return privacyidentity.DeriveViewKeys(rootSeed)
}

func deriveDisclosureKeys(rootSeed []byte) (*big.Int, *crypto_tedwards.PointAffine, []byte) {
	return privacyidentity.DeriveDisclosureKeys(rootSeed)
}

func scalarToFixedHex(scalar *big.Int) string {
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

// writePadded mirrors the circuit's field-element byte encoding before hashing.
func writePadded(h hash.Hash, i *big.Int) {
	var elem fr.Element
	elem.SetBigInt(i)
	b := elem.Bytes()
	h.Write(b[:])
}

func manualSign(msg *big.Int, scalar *big.Int, pubKey *crypto_tedwards.PointAffine) ([]byte, error) {
	curve := crypto_tedwards.GetEdwardsCurve()
	frModulus := fr.Modulus()

	for {
		rBig, _ := rand.Int(rand.Reader, &curve.Order)

		var g crypto_tedwards.PointAffine
		g.X.Set(&curve.Base.X)
		g.Y.Set(&curve.Base.Y)

		var pointR crypto_tedwards.PointAffine
		pointR.ScalarMultiplication(&g, rBig)

		hFunc := mimc.NewMiMC()
		rx, ry := new(big.Int), new(big.Int)
		pointR.X.BigInt(rx)
		pointR.Y.BigInt(ry)
		writePadded(hFunc, rx)
		writePadded(hFunc, ry)

		ax, ay := new(big.Int), new(big.Int)
		pubKey.X.BigInt(ax)
		pubKey.Y.BigInt(ay)
		writePadded(hFunc, ax)
		writePadded(hFunc, ay)

		writePadded(hFunc, msg)

		hRam := hFunc.Sum(nil)
		hRamInt := new(big.Int).SetBytes(hRam)

		sPart := new(big.Int).Mul(hRamInt, scalar)
		S := new(big.Int).Add(rBig, sPart)
		S.Mod(S, &curve.Order)

		if S.Cmp(frModulus) >= 0 {
			continue
		}

		rPointBytes := pointR.Bytes()
		sBytesRaw := S.Bytes()
		sBytesPadded := make([]byte, 32)
		copy(sBytesPadded[32-len(sBytesRaw):], sBytesRaw)

		return append(rPointBytes[:], sBytesPadded...), nil
	}
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

func noteAmountString(note types.Note) string {
	if note.Amount == nil {
		return "0"
	}

	return note.Amount.String()
}

func buildListNotesJSONOutput(foundNotes []FoundNote, diagnostics *scanNotesDiagnostics) listNotesJSONOutput {
	_, totalSpendable := privacyscan.SummarizeSpendableNotes(foundNotes)
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
			Note:      info.Note,
		})
	}

	return output
}

func buildDepositNoteAndMsg(
	fromAddr string,
	pubKey *crypto_tedwards.PointAffine,
	amount *big.Int,
	denom string,
	memo string,
	amountStr string,
	seed []byte,
	logWriter io.Writer,
	latencyFlow *privacyLatencyFlow,
) (*types.Note, *types.MsgDeposit, error) {
	viewScalar, viewPubKey, _ := deriveViewKeys(seed)
	_ = viewScalar

	spendPubKeyX, spendPubKeyY := new(big.Int), new(big.Int)
	viewPubKeyX, viewPubKeyY := new(big.Int), new(big.Int)
	pubKey.X.BigInt(spendPubKeyX)
	pubKey.Y.BigInt(spendPubKeyY)
	viewPubKey.X.BigInt(viewPubKeyX)
	viewPubKey.Y.BigInt(viewPubKeyY)

	note, err := types.NewNote(spendPubKeyX, spendPubKeyY, viewPubKeyX, viewPubKeyY, amount, denom, memo)
	if err != nil {
		return nil, nil, err
	}

	commitment := note.ComputeCommitment()
	canonicalCommitment, err := canonicalFieldBytesFromBigInt(commitment)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid note commitment: %w", err)
	}
	encryptedNote, err := crypto.Encrypt(note.Bytes(), seed)
	if err != nil {
		return nil, nil, err
	}
	proof, err := privacydeposit.BuildDepositProof(
		*note,
		depositArtifactProvider{},
		depositProofRunner{logWriter: logWriter, latencyFlow: latencyFlow},
	)
	if err != nil {
		return nil, nil, err
	}

	msg := types.NewMsgDeposit(
		fromAddr,
		amountStr,
		canonicalCommitment,
		encryptedNote,
		proof,
	)

	return note, msg, nil
}

type depositArtifactProvider struct{}

func (depositArtifactProvider) DepositR1CS() (constraint.ConstraintSystem, error) {
	return zk.GetDepositR1CS()
}

func (depositArtifactProvider) DepositProvingKey() (groth16.ProvingKey, error) {
	return zk.GetDepositProvingKey()
}

type depositProofRunner struct {
	logWriter   io.Writer
	latencyFlow *privacyLatencyFlow
}

func (r depositProofRunner) ProveDeposit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, depositWitness witness.Witness) (groth16.Proof, error) {
	if r.latencyFlow != nil {
		r.latencyFlow.recordPrepareUntil(time.Now())
	}
	return observePrivacyLatencyPhase(r.latencyFlow, "proof", func() (groth16.Proof, error) {
		return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
			return groth16.Prove(r1cs, provingKey, depositWitness)
		})
	})
}

func autoPrepareDummyNote(cmd *cobra.Command, clientCtx client.Context, denom string) error {
	_, pubKey, seed, err := getExplicitKeys(clientCtx)
	if err != nil {
		return err
	}

	amount := big.NewInt(0)
	amountStr := fmt.Sprintf("0%s", denom)
	_, msg, err := buildDepositNoteAndMsg(
		clientCtx.GetFromAddress().String(),
		pubKey,
		amount,
		denom,
		"AutoDummy",
		amountStr,
		seed,
		privacyCommandLogWriter(cmd),
		nil,
	)
	if err != nil {
		return err
	}

	printAutoDummyPreparationSummary(cmd, denom, amountStr)
	res, err := privacyprovider.CosmosTxBroadcaster{
		ClientContext: clientCtx,
		Flags:         cmd.Flags(),
		FromName:      clientCtx.GetFromName(),
	}.BroadcastSDKMessage(cmd.Context(), msg)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("dummy-note tx failed with code %d: %s", res.Code, res.RawLog)
	}

	printAutoDummySubmitted(cmd, res.TxHash)
	if err := waitForBlock(clientCtx, res.Height); err != nil {
		return fmt.Errorf("polling failed: %w", err)
	}

	return nil
}

func buildWithdrawPayload(cmd *cobra.Command, clientCtx client.Context, targetCoin sdk.Coin, recipientAddr sdk.AccAddress, expiresAt time.Time, autoPlan bool, latencyFlow *privacyLatencyFlow) (*PreparedWithdrawPayload, error) {
	scalar, pubKey, seed, err := getExplicitKeys(clientCtx)
	if err != nil {
		return nil, err
	}
	forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
	if err != nil {
		return nil, err
	}

	result, err := privacywithdraw.BuildWithdrawPayload(
		context.Background(),
		&withdrawExactMatchNoteSource{
			clientCtx:   clientCtx,
			seed:        seed,
			logWriter:   privacyCommandLogWriter(cmd),
			forceRescan: forceRescan,
		},
		withdrawExactMatchAutoPlanner{
			cmd:       cmd,
			clientCtx: clientCtx,
		},
		privacyprovider.NewWithdrawQueryProvider(types.NewQueryClient(clientCtx)),
		manualSpendNoteHashSigner{scalar: scalar, pubKey: pubKey},
		withdrawSpendArtifactProvider{},
		withdrawSpendProofRunner{logWriter: privacyCommandLogWriter(cmd), latencyFlow: latencyFlow},
		privacywithdraw.BuildWithdrawPayloadInput{
			TargetCoin: targetCoin,
			Recipient:  recipientAddr,
			ChainID:    clientCtx.ChainID,
			ExpiresAt:  expiresAt,
			AutoPlan:   autoPlan,
		},
	)
	if err != nil {
		return nil, err
	}
	printSelectedWithdrawNote(cmd, targetCoin.String())
	return result.Payload, nil
}

type manualSpendNoteHashSigner struct {
	scalar *big.Int
	pubKey *crypto_tedwards.PointAffine
}

func (s manualSpendNoteHashSigner) SignSpendNoteHash(msgHash *big.Int) ([]byte, error) {
	return manualSign(msgHash, s.scalar, s.pubKey)
}

type withdrawSpendArtifactProvider struct{}

func (withdrawSpendArtifactProvider) SpendR1CS() (constraint.ConstraintSystem, error) {
	return zk.GetSpendR1CS()
}

func (withdrawSpendArtifactProvider) SpendProvingKey() (groth16.ProvingKey, error) {
	return zk.GetSpendProvingKey()
}

type withdrawSpendProofRunner struct {
	logWriter   io.Writer
	latencyFlow *privacyLatencyFlow
}

func (r withdrawSpendProofRunner) ProveSpend(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, spendWitness witness.Witness) (groth16.Proof, error) {
	if r.latencyFlow != nil {
		r.latencyFlow.recordPrepareUntil(time.Now())
	}
	return observePrivacyLatencyPhase(r.latencyFlow, "proof", func() (groth16.Proof, error) {
		return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
			return groth16.Prove(r1cs, provingKey, spendWitness)
		})
	})
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

	auditPubKey, auditPubKeyBz, err := queryAuditDisclosureTarget(clientCtx)
	if err != nil {
		return err
	}

	printPlannerSelfTransferSummary(cmd, targetCoin.String(), selfShieldedAddress)

	res, err := executeTransferFlowWithIdentity(
		cmd,
		clientCtx,
		identity,
		identity.spendPubKey,
		identity.viewPubKey,
		targetCoin.Amount.BigInt(),
		targetCoin.Denom,
		autoDummy,
		privacytransfer.StepDisclosureConfig{
			UserPrivacyPolicy:             types.TransferPrivacyPolicyAllPrivate,
			UserDisclosureMode:            types.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			AuditDisclosureTargetPubKey:   auditPubKey,
			AuditDisclosureTargetPubKeyBz: auditPubKeyBz,
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
			amountBig := coin.Amount.BigInt()
			memo, _ := cmd.Flags().GetString("memo")

			latencyFlow := newPrivacyLatencyFlow("deposit")
			var runErr error
			defer func() {
				latencyFlow.finish(runErr)
			}()

			note, msg, err := buildDepositNoteAndMsg(
				clientCtx.GetFromAddress().String(),
				pubKey,
				amountBig,
				coin.Denom,
				memo,
				coin.String(),
				seed,
				privacyCommandLogWriter(cmd),
				latencyFlow,
			)
			if err != nil {
				runErr = err
				return err
			}

			privacyCommandPrintf(cmd, "Deposit note (JSON):\n%s\n", string(note.Bytes()))

			submitStartedAt := time.Now()
			runErr = privacyprovider.CosmosTxBroadcaster{
				ClientContext: clientCtx,
				Flags:         cmd.Flags(),
			}.GenerateOrBroadcast(msg)
			latencyFlow.recordSubmit(submitStartedAt, "", runErr)
			return runErr
		},
	}
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
			printWithdrawCommandSummary(cmd, "Shielded withdraw", recipientAddr.String(), targetCoin.String(), autoPlan, autoDummy)

			expiresAt := time.Now().Add(defaultPreparedWithdrawExpiry)
			payload, err := buildWithdrawPayload(cmd, clientCtx, targetCoin, recipientAddr, expiresAt, autoPlan, latencyFlow)
			if err != nil {
				runErr = err
				return err
			}

			msg, err := payload.ToMsg(clientCtx.GetFromAddress().String())
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
			printWithdrawCommandSummary(cmd, "Prepare shielded withdraw", recipientAddr.String(), targetCoin.String(), autoPlan, autoDummy)

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

			payload, err := buildWithdrawPayload(cmd, clientCtx, targetCoin, recipientAddr, expiresAt, autoPlan, latencyFlow)
			if err != nil {
				runErr = err
				return err
			}

			outPath, err := cmd.Flags().GetString("out")
			if err != nil {
				return err
			}

			if outPath != "" {
				if err := payload.WriteJSONFile(outPath); err != nil {
					runErr = err
					return err
				}
				printPreparedWithdrawPayloadSaved(cmd, outPath)
			}

			runErr = printCommandJSON(cmd, payload)
			return runErr
		},
	}

	cmd.Flags().String("recipient", "", "recipient public address (default: sender address)")
	cmd.Flags().String("out", "", "output file path for prepared payload")
	cmd.Flags().Int64("expires-in", int64(defaultPreparedWithdrawExpiry/time.Second), "prepared payload validity window in seconds")
	cmd.Flags().Bool(flagWithdrawAutoPlan, true, "Automatically create an exact-match note with a preparatory shielded self-transfer when needed")
	cmd.Flags().Bool(flagAutoDummy, true, "Automatically create a zero-value dummy note with a preparatory deposit when the planner needs it")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before exact-match note selection")
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
			msg, err := privacywithdraw.BuildRelayWithdrawMsgFromFile(args[0], clientCtx.GetFromAddress().String())
			latencyFlow.recordPhase("prepare", prepareStartedAt, err)
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
			_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
			_, viewPubKey, _ := deriveViewKeys(rootSeed)

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

			viewScalar, viewPubKey, _ := deriveViewKeys(rootSeed)
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
