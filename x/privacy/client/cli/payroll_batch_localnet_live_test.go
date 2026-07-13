//go:build session3b_localnet

package cli

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdkkeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	privatefile "github.com/DELIGHT-LABS/clairveil/internal/privatefile"
	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	session3BPayrollStateSchema  = "clairveil.session3b.payroll-live-state.v1"
	session3BPayrollResultSchema = "clairveil.session3b.payroll-live-result.v1"
	session3BLookupKeyID         = "session3b-payroll-live-v1"
)

func init() {
	clairveiltypes.SetConfig()
}

type session3BPayrollState struct {
	SchemaVersion          string                             `json:"schema_version"`
	OperationID            string                             `json:"operation_id"`
	ConflictOperationID    string                             `json:"conflict_operation_id,omitempty"`
	PayloadHash            string                             `json:"payload_hash"`
	TxHash                 string                             `json:"tx_hash,omitempty"`
	TxBytesHash            string                             `json:"tx_bytes_hash,omitempty"`
	TimeoutBytesHash       string                             `json:"timeout_bytes_hash,omitempty"`
	RetryBytesHash         string                             `json:"retry_bytes_hash,omitempty"`
	EffectID               string                             `json:"effect_id,omitempty"`
	StagePIDs              map[string]int                     `json:"stage_pids"`
	TimeoutBeforeSend      bool                               `json:"timeout_before_send"`
	ExactStoredBytesRetry  bool                               `json:"exact_stored_bytes_retry"`
	TxHashFirst            bool                               `json:"tx_hash_first"`
	SpentNullifierConflict bool                               `json:"spent_nullifier_conflict"`
	BroadcastAttempts      int                                `json:"broadcast_attempts"`
	InputCount             int                                `json:"input_count"`
	OutputCount            int                                `json:"output_count"`
	SucceededItems         int                                `json:"succeeded_items"`
	ManualReviewItems      int                                `json:"manual_review_items"`
	ChainStatus            privacyreservation.OperationStatus `json:"chain_status"`
	ConflictStatus         privacyreservation.OperationStatus `json:"conflict_status,omitempty"`
}

type session3BPayrollSummary struct {
	SchemaVersion          string                             `json:"schema_version"`
	Status                 string                             `json:"status"`
	OperationID            string                             `json:"operation_id"`
	ConflictOperationID    string                             `json:"conflict_operation_id"`
	PayloadHash            string                             `json:"payload_hash"`
	TxHash                 string                             `json:"tx_hash"`
	TxBytesHash            string                             `json:"tx_bytes_hash"`
	EffectID               string                             `json:"effect_id"`
	StagePIDs              map[string]int                     `json:"stage_pids"`
	ProcessRestarted       bool                               `json:"process_restarted"`
	TimeoutBeforeSend      bool                               `json:"timeout_before_send"`
	ExactStoredBytesRetry  bool                               `json:"exact_stored_bytes_retry"`
	TxHashFirst            bool                               `json:"tx_hash_first_reconcile"`
	SpentNullifierConflict bool                               `json:"spent_nullifier_conflict"`
	BroadcastAttempts      int                                `json:"broadcast_attempts"`
	InputCount             int                                `json:"input_count"`
	OutputCount            int                                `json:"output_count"`
	ProofCount             int                                `json:"proof_count"`
	TxEnvelopeCount        int                                `json:"tx_envelope_count"`
	SucceededItems         int                                `json:"succeeded_items"`
	ManualReviewItems      int                                `json:"conflict_manual_review_items"`
	ChainStatus            privacyreservation.OperationStatus `json:"chain_status"`
	ConflictStatus         privacyreservation.OperationStatus `json:"conflict_chain_status"`
}

type session3BLiveConfig struct {
	Stage           string
	Home            string
	OutDir          string
	StorePath       string
	StatePath       string
	PreparedPath    string
	ProofPath       string
	ReportPath      string
	SummaryPath     string
	ArtifactKeyPath string
	Node            string
	GRPCAddr        string
	ChainID         string
	ProverURL       string
	Gas             string
	GasPrices       string
	AliceNotesPath  string
	BobAddress      string
	BobDisclosure   string
}

func TestSession3BOneProofPayrollLocalnet(t *testing.T) {
	cfg := loadSession3BLiveConfig(t)
	switch cfg.Stage {
	case "graph":
		runSession3BGraphStage(t, cfg)
	case "prove":
		runSession3BProveStage(t, cfg)
	case "timeout":
		runSession3BTimeoutStage(t, cfg)
	case "retry":
		runSession3BRetryStage(t, cfg)
	case "reconcile":
		runSession3BReconcileStage(t, cfg)
	case "conflict":
		runSession3BConflictStage(t, cfg)
	default:
		t.Fatalf("unsupported %s %q", "CLAIRVEIL_SESSION3B_STAGE", cfg.Stage)
	}
}

func loadSession3BLiveConfig(t *testing.T) session3BLiveConfig {
	t.Helper()
	outDir := mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_OUT_DIR")
	cfg := session3BLiveConfig{
		Stage:           mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_STAGE"),
		Home:            mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_HOME"),
		OutDir:          outDir,
		StorePath:       mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_STORE_PATH"),
		StatePath:       filepath.Join(outDir, ".session3b-payroll-state.json"),
		PreparedPath:    filepath.Join(outDir, "payroll-prepared.json"),
		ProofPath:       filepath.Join(outDir, "payroll-proof.json"),
		ReportPath:      filepath.Join(outDir, "payroll-operation-report.json"),
		SummaryPath:     filepath.Join(outDir, "session3b-payroll-live-summary.json"),
		ArtifactKeyPath: filepath.Join(outDir, ".session3b-payroll-artifact-key"),
		Node:            mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_NODE"),
		GRPCAddr:        mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_GRPC_ADDR"),
		ChainID:         mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_CHAIN_ID"),
		ProverURL:       mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_PROVER_URL"),
		Gas:             mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_GAS"),
		GasPrices:       mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_GAS_PRICES"),
		AliceNotesPath:  mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_ALICE_NOTES_PATH"),
		BobAddress:      mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_BOB_ADDRESS"),
		BobDisclosure:   mustSession3BEnv(t, "CLAIRVEIL_SESSION3B_BOB_DISCLOSURE_PUBKEY"),
	}
	if value := strings.TrimSpace(os.Getenv("CLAIRVEIL_SESSION3B_PREPARED_PATH")); value != "" {
		cfg.PreparedPath = value
	}
	if value := strings.TrimSpace(os.Getenv("CLAIRVEIL_SESSION3B_PROOF_PATH")); value != "" {
		cfg.ProofPath = value
	}
	for _, path := range []string{cfg.Home, cfg.OutDir, cfg.StorePath, cfg.PreparedPath, cfg.ProofPath, cfg.AliceNotesPath} {
		if !filepath.IsAbs(path) {
			t.Fatalf("Session 3B live paths must be absolute: %q", path)
		}
	}
	require.NoError(t, os.MkdirAll(cfg.OutDir, 0o700))
	return cfg
}

func mustSession3BEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required; live integration tests never skip", name)
	}
	return value
}

func runSession3BGraphStage(t *testing.T, cfg session3BLiveConfig) {
	ctx := context.Background()
	payload := readSession3BPayload(t, cfg.PreparedPath)
	require.Len(t, payload.Inputs, 3)
	require.Len(t, payload.Outputs, 4)
	require.Equal(t, []int64{5, 7, 9}, session3BInputAmounts(payload))
	require.Equal(t, []int64{4, 5, 9, 3}, session3BOutputAmounts(payload))

	protector := newSession3BProtector(t, cfg.ArtifactKeyPath, true)
	operation, notes := session3BPayrollOperation(t, cfg, payload, protector, "")
	require.Len(t, operation.InputNotes, 3)
	require.Equal(t, 4, operation.OutputCount)
	require.Equal(t, int64(3), operation.Change.Int64())
	reservations, graph, err := privacypayroll.BuildBatchOperationGraph(ctx, operation, payload, protector, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, reservations, 3)
	require.Len(t, graph.Items, 4)
	store, err := privacyreservation.OpenDurableFileStore(cfg.StorePath)
	require.NoError(t, err)
	created, err := privacypayroll.ConfirmBatchPayrollOperation(ctx, store, reservations, graph)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusPlanned, created.Operation.Status)
	for i := range created.Inputs {
		require.Equal(t, notes[i].NoteID, operation.InputNotes[i].NoteID)
		require.Equal(t, i, created.Inputs[i].InputIndex)
	}
	state := session3BPayrollState{
		SchemaVersion: session3BPayrollStateSchema, OperationID: operation.OperationID,
		PayloadHash: payload.PayloadHash, StagePIDs: map[string]int{"graph": os.Getpid()},
		InputCount: len(created.Inputs), OutputCount: len(created.Items), ChainStatus: created.Operation.Status,
	}
	writeSession3BJSON(t, cfg.StatePath, state)
}

func runSession3BProveStage(t *testing.T, cfg session3BLiveConfig) {
	ctx := context.Background()
	state := readSession3BState(t, cfg.StatePath)
	payload := readSession3BPayload(t, cfg.PreparedPath)
	store, err := privacyreservation.OpenDurableFileStore(cfg.StorePath)
	require.NoError(t, err)
	protector := newSession3BProtector(t, cfg.ArtifactKeyPath, false)
	proof, err := (privacypayroll.BatchProofWorker{
		Store: store,
		Prover: privacypayroll.RemoteBatchPayrollProver{Client: privacyprovertransport.HTTPProverClient{
			BaseURL: cfg.ProverURL, Client: &http.Client{Timeout: 30 * time.Minute},
			BearerToken: strings.TrimSpace(os.Getenv(privacyprovertransport.BearerTokenEnv)),
		}},
		Sealer: protector, LeaseOwner: fmt.Sprintf("session3b-proof-%d", os.Getpid()), LeaseTTL: 2 * time.Minute,
	}).Process(ctx, state.OperationID, payload)
	require.NoError(t, err)
	require.NoError(t, privacybatchtransfer.WritePreparedBatchTransferProof(cfg.ProofPath, proof))
	graph, err := store.GetBatchOperation(ctx, state.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, graph.Operation.Status)
	require.NotEmpty(t, graph.Operation.ProofCiphertext)
	require.NotEmpty(t, graph.Operation.ProofHash)
	recordSession3BStage(t, cfg, &state, "prove")
}

func runSession3BTimeoutStage(t *testing.T, cfg session3BLiveConfig) {
	ctx := context.Background()
	state := readSession3BState(t, cfg.StatePath)
	payload := readSession3BPayload(t, cfg.PreparedPath)
	proof := readSession3BProof(t, cfg.ProofPath)
	store, err := privacyreservation.OpenDurableFileStore(cfg.StorePath)
	require.NoError(t, err)
	protector := newSession3BProtector(t, cfg.ArtifactKeyPath, false)
	live := newSession3BLiveClients(t, ctx, cfg)
	defer live.Close()
	timeoutSender := &session3BTimeoutSender{}
	outcome, err := (privacypayroll.IdempotentBatchBroadcastWorker{
		Store: store, Builder: live.Adapter, Sender: timeoutSender, Reconciler: live.Chain,
		Cipher: protector, LeaseOwner: fmt.Sprintf("session3b-timeout-%d", os.Getpid()), LeaseTTL: 2 * time.Minute,
	}).Submit(ctx, state.OperationID, payload, proof, live.Client.GetFromAddress().String(), privacypayroll.BatchBroadcastOptions{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotNil(t, outcome)
	require.Equal(t, 1, timeoutSender.calls)
	graph, err := store.GetBatchOperation(ctx, state.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, graph.Operation.Status)
	require.Equal(t, 1, graph.Operation.BroadcastAttemptCount)
	require.NotEmpty(t, graph.Operation.SignedTxBytesCiphertext)
	signedBytes, err := protector.OpenPayrollEvidence(ctx, graph.Operation.SignedTxBytesCiphertext)
	require.NoError(t, err)
	digest := sha256.Sum256(signedBytes)
	wantHash := hex.EncodeToString(digest[:])
	require.Equal(t, strings.ToLower(graph.Operation.TxBytesHash), wantHash)
	state.TxHash = graph.Operation.TxHash
	state.TxBytesHash = graph.Operation.TxBytesHash
	state.TimeoutBytesHash = wantHash
	state.TimeoutBeforeSend = true
	state.BroadcastAttempts = graph.Operation.BroadcastAttemptCount
	state.ChainStatus = graph.Operation.Status
	recordSession3BStage(t, cfg, &state, "timeout")
}

func runSession3BRetryStage(t *testing.T, cfg session3BLiveConfig) {
	ctx := context.Background()
	state := readSession3BState(t, cfg.StatePath)
	payload := readSession3BPayload(t, cfg.PreparedPath)
	proof := readSession3BProof(t, cfg.ProofPath)
	store, err := privacyreservation.OpenDurableFileStore(cfg.StorePath)
	require.NoError(t, err)
	protector := newSession3BProtector(t, cfg.ArtifactKeyPath, false)
	live := newSession3BLiveClients(t, ctx, cfg)
	defer live.Close()
	builder := &session3BRejectBuilder{}
	sender := &session3BExactBytesSender{inner: live.Adapter, expectedHash: state.TimeoutBytesHash}
	outcome, err := (privacypayroll.IdempotentBatchBroadcastWorker{
		Store: store, Builder: builder, Sender: sender, Reconciler: live.Chain,
		Cipher: protector, LeaseOwner: fmt.Sprintf("session3b-retry-%d", os.Getpid()), LeaseTTL: 2 * time.Minute,
	}).Submit(ctx, state.OperationID, payload, proof, live.Client.GetFromAddress().String(), privacypayroll.BatchBroadcastOptions{})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.True(t, outcome.UsedStoredSignedBytes)
	require.Zero(t, builder.calls)
	require.Equal(t, 1, sender.calls)
	require.Equal(t, state.TimeoutBytesHash, sender.observedHash)
	require.NotNil(t, outcome.Receipt)
	require.Equal(t, uint32(0), outcome.Receipt.Code)
	require.Equal(t, strings.ToLower(state.TxHash), strings.ToLower(outcome.Receipt.TxHash))
	require.True(t, waitSession3BTx(t, ctx, live.TxService, state.TxHash, 2*time.Minute))
	graph, err := store.GetBatchOperation(ctx, state.OperationID)
	require.NoError(t, err)
	require.Equal(t, 2, graph.Operation.BroadcastAttemptCount)
	require.Contains(t, []privacyreservation.OperationStatus{privacyreservation.OperationStatusSubmitted, privacyreservation.OperationStatusUnknown}, graph.Operation.Status)
	state.RetryBytesHash = sender.observedHash
	state.ExactStoredBytesRetry = true
	state.BroadcastAttempts = graph.Operation.BroadcastAttemptCount
	state.ChainStatus = graph.Operation.Status
	recordSession3BStage(t, cfg, &state, "retry")
	require.NoError(t, privatefile.Write(filepath.Join(cfg.OutDir, "payroll.txhash"), []byte(state.TxHash+"\n")))
}

func runSession3BReconcileStage(t *testing.T, cfg session3BLiveConfig) {
	ctx := context.Background()
	state := readSession3BState(t, cfg.StatePath)
	payload := readSession3BPayload(t, cfg.PreparedPath)
	store, err := privacyreservation.OpenDurableFileStore(cfg.StorePath)
	require.NoError(t, err)
	live := newSession3BLiveClients(t, ctx, cfg)
	defer live.Close()
	graph, err := store.GetBatchOperation(ctx, state.OperationID)
	require.NoError(t, err)
	effectID, outputs := findSession3BBatchOutputs(t, ctx, live.Scan, state.TxHash)
	require.Len(t, outputs, len(payload.Outputs))
	observed := make([]privacyreservation.ObservedOutputEvidence, len(outputs))
	for i, output := range outputs {
		require.Equal(t, uint32(i), output.OutputIndex)
		require.Equal(t, payload.MessageOutputs[i].Commitment, output.Commitment)
		observed[i] = privacyreservation.ObservedOutputEvidence{
			OutputIndex: i, Commitment: hex.EncodeToString(output.Commitment),
			UserDisclosureDigest: hex.EncodeToString(output.UserDisclosureDigest),
			FullDisclosureDigest: hex.EncodeToString(output.FullDisclosureDigest),
			RecipientHash:        graph.Evidence[i].RecipientHash,
		}
	}
	ordered := &session3BOrderedReconciler{inner: live.Chain}
	result, err := (privacypayroll.BatchReconcileWorker{Store: store, Reconciler: ordered}).Reconcile(ctx, privacypayroll.BatchReconcileRequest{
		OperationID: state.OperationID, Payload: payload, ObservedOutputs: observed,
	})
	require.NoError(t, err)
	require.NotEmpty(t, ordered.calls)
	require.True(t, strings.HasPrefix(ordered.calls[0], "lookup:"), ordered.calls)
	require.Equal(t, "nullifiers", ordered.calls[len(ordered.calls)-1])
	require.Equal(t, privacyreservation.OperationStatusSucceeded, result.Graph.Operation.Status)
	succeededPayments := 0
	for _, item := range result.Graph.Items {
		require.Equal(t, privacyreservation.BatchItemEvidenceSucceeded, item.EvidenceStatus)
		if item.Role == privacyreservation.BatchOutputRolePayment {
			succeededPayments++
		}
	}
	require.Equal(t, 3, succeededPayments)
	report, err := privacypayroll.BuildBatchOperationReport(*result.Graph, effectID)
	require.NoError(t, err)
	require.Equal(t, 1, report.ProofCount)
	require.Equal(t, 1, report.TxEnvelopeCount)
	require.Equal(t, 2, report.BroadcastAttemptCount)
	require.Equal(t, 4, report.OutputCount)
	require.Equal(t, 3, report.SucceededItems)
	writeSession3BJSON(t, cfg.ReportPath, report)
	state.TxHashFirst = true
	state.EffectID = effectID
	state.SucceededItems = report.SucceededItems
	state.BroadcastAttempts = report.BroadcastAttemptCount
	state.InputCount = report.InputCount
	state.OutputCount = report.OutputCount
	state.ChainStatus = report.ChainStatus
	recordSession3BStage(t, cfg, &state, "reconcile")
}

func runSession3BConflictStage(t *testing.T, cfg session3BLiveConfig) {
	ctx := context.Background()
	state := readSession3BState(t, cfg.StatePath)
	payload := readSession3BPayload(t, cfg.PreparedPath)
	store, err := privacyreservation.OpenDurableFileStore(cfg.StorePath)
	require.NoError(t, err)
	protector := newSession3BProtector(t, cfg.ArtifactKeyPath, false)
	operation, _ := session3BPayrollOperation(t, cfg, payload, protector, ":spent-conflict")
	reservations, graph, err := privacypayroll.BuildBatchOperationGraph(ctx, operation, payload, protector, time.Now().UTC())
	require.NoError(t, err)
	created, err := privacypayroll.ConfirmBatchPayrollOperation(ctx, store, reservations, graph)
	require.NoError(t, err)
	live := newSession3BLiveClients(t, ctx, cfg)
	defer live.Close()
	conflictReconciler := &session3BAbsentTxReconciler{inner: live.Chain}
	result, err := (privacypayroll.BatchReconcileWorker{Store: store, Reconciler: conflictReconciler}).Reconcile(ctx, privacypayroll.BatchReconcileRequest{
		OperationID: created.Operation.OperationID, Payload: payload, FailureReason: "live spent-nullifier conflict",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"nullifiers"}, conflictReconciler.calls)
	require.Equal(t, privacyreservation.OperationStatusManualReview, result.Graph.Operation.Status)
	manualPayments := 0
	for _, item := range result.Graph.Items {
		require.Equal(t, privacyreservation.BatchItemEvidenceManualReview, item.EvidenceStatus)
		if item.Role == privacyreservation.BatchOutputRolePayment {
			manualPayments++
		}
	}
	require.Equal(t, 3, manualPayments)
	state.ConflictOperationID = created.Operation.OperationID
	state.SpentNullifierConflict = true
	state.ManualReviewItems = manualPayments
	state.ConflictStatus = result.Graph.Operation.Status
	recordSession3BStage(t, cfg, &state, "conflict")
	assertSession3BFinalState(t, state)
	summary := session3BPayrollSummary{
		SchemaVersion: session3BPayrollResultSchema, Status: "passed", OperationID: state.OperationID,
		ConflictOperationID: state.ConflictOperationID, PayloadHash: state.PayloadHash, TxHash: state.TxHash,
		TxBytesHash: state.TxBytesHash, EffectID: state.EffectID, StagePIDs: state.StagePIDs,
		ProcessRestarted: true, TimeoutBeforeSend: state.TimeoutBeforeSend,
		ExactStoredBytesRetry: state.ExactStoredBytesRetry, TxHashFirst: state.TxHashFirst,
		SpentNullifierConflict: state.SpentNullifierConflict, BroadcastAttempts: state.BroadcastAttempts,
		InputCount: state.InputCount, OutputCount: state.OutputCount, ProofCount: 1, TxEnvelopeCount: 1,
		SucceededItems: state.SucceededItems, ManualReviewItems: state.ManualReviewItems, ChainStatus: state.ChainStatus,
		ConflictStatus: state.ConflictStatus,
	}
	writeSession3BJSON(t, cfg.SummaryPath, summary)
}

func session3BPayrollOperation(t *testing.T, cfg session3BLiveConfig, payload *privacybatchtransfer.PreparedBatchTransferPayload, protector session3BProtector, operationSuffix string) (privacypayroll.BatchPayrollOperationPlan, []privacypayroll.TreasuryNote) {
	t.Helper()
	var wallet listNotesJSONOutput
	readSession3BJSON(t, cfg.AliceNotesPath, &wallet)
	notes := make([]privacypayroll.TreasuryNote, len(payload.Inputs))
	for i, input := range payload.Inputs {
		commitment := hex.EncodeToString(input.Note.ComputeCommitment().FillBytes(make([]byte, 32)))
		matched := false
		for _, candidate := range wallet.Notes {
			candidateCommitment := hex.EncodeToString(candidate.Note.ComputeCommitment().FillBytes(make([]byte, 32)))
			if candidate.Status == "spendable" && candidateCommitment == commitment {
				matched = true
				break
			}
		}
		require.True(t, matched, "prepared input %d must be a live spendable wallet note", i)
		lookup, err := protector.PayrollNullifierLookupKey(context.Background(), session3BLookupKeyID, input.Nullifier)
		require.NoError(t, err)
		notes[i] = privacypayroll.TreasuryNote{
			NoteID: fmt.Sprintf("%02d:%s", i, commitment), OwnerKeyID: "alice", NullifierLookupKey: lookup,
			NullifierLookupKeyID: session3BLookupKeyID, Denom: "uclair", Amount: new(big.Int).Set(input.Note.Amount),
		}
	}
	items := []privacypayroll.PayrollItemInput{
		{ItemID: "item-0", EmployeeID: "employee-0", RecipientAddress: cfg.BobAddress, Amount: big.NewInt(4), Denom: "uclair", DisclosurePolicySet: true, DisclosurePolicy: privacypayroll.PayrollDisclosurePolicy{}},
		{ItemID: "item-1", EmployeeID: "employee-1", RecipientAddress: cfg.BobAddress, Amount: big.NewInt(5), Denom: "uclair", DisclosurePolicySet: true, DisclosurePolicy: privacypayroll.PayrollDisclosurePolicy{UserPrivacyPolicy: privacytypes.TransferPrivacyPolicyDiscloseAmount, UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC}},
		{ItemID: "item-2", EmployeeID: "employee-2", RecipientAddress: cfg.BobAddress, Amount: big.NewInt(9), Denom: "uclair", DisclosurePolicySet: true, DisclosurePolicy: privacypayroll.PayrollDisclosurePolicy{UserPrivacyPolicy: privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom, UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, UserDisclosureTargetPubKeyHex: cfg.BobDisclosure}},
	}
	plan, err := (privacypayroll.BatchPayrollPlanner{}).Plan(privacypayroll.PayrollInput{
		CompanyID: "session3b-company", PayrollID: "session3b-payroll", BatchID: "session3b-batch" + operationSuffix,
		Denom: "uclair", Attempt: 1, Items: items, CreatedAt: time.Now().UTC(),
	}, notes)
	require.NoError(t, err)
	require.Len(t, plan.Operations, 1)
	operation := plan.Operations[0]
	if operationSuffix != "" {
		operation.OperationID += operationSuffix
		for i := range operation.Items {
			operation.Items[i].OperationID = operation.OperationID
		}
	}
	return operation, notes
}

type session3BNoteSource map[string]privacytypes.Note

func (s session3BNoteSource) LoadBatchInputNote(_ context.Context, noteID string) (privacytypes.Note, error) {
	note, ok := s[noteID]
	if !ok {
		return privacytypes.Note{}, fmt.Errorf("note %s not found", noteID)
	}
	return note, nil
}

type session3BProtector struct{ key []byte }

func newSession3BProtector(t *testing.T, path string, create bool) session3BProtector {
	t.Helper()
	if create {
		key := make([]byte, 32)
		_, err := rand.Read(key)
		require.NoError(t, err)
		require.NoError(t, privatefile.Write(path, []byte(hex.EncodeToString(key)+"\n")))
	}
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	key, err := hex.DecodeString(strings.TrimSpace(string(bz)))
	require.NoError(t, err)
	require.Len(t, key, 32)
	return session3BProtector{key: key}
}

func (p session3BProtector) SealPayrollEvidence(_ context.Context, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, []byte(session3BPayrollStateSchema)), nil
}

func (p session3BProtector) OpenPayrollEvidence(_ context.Context, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, fmt.Errorf("sealed payroll evidence is truncated")
	}
	return aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(session3BPayrollStateSchema))
}

func (p session3BProtector) PayrollNullifierLookupKey(_ context.Context, keyID string, nullifier []byte) (string, error) {
	indexKey := append(append([]byte(nil), p.key...), []byte(keyID)...)
	return privacyreservation.NullifierLookupKey(indexKey, nullifier)
}

type session3BLiveClients struct {
	Client    client.Context
	Conn      *grpc.ClientConn
	TxService txtypes.ServiceClient
	Scan      privacyprovider.ScanQueryProvider
	Chain     privacypayroll.CosmosBatchChainReconciler
	Adapter   privacypayroll.CosmosBatchTxAdapter
}

func newSession3BLiveClients(t *testing.T, ctx context.Context, cfg session3BLiveConfig) session3BLiveClients {
	t.Helper()
	encoding := moduletestutil.MakeTestEncodingConfig()
	authtypes.RegisterLegacyAminoCodec(encoding.Amino)
	authtypes.RegisterInterfaces(encoding.InterfaceRegistry)
	privacytypes.RegisterLegacyAminoCodec(encoding.Amino)
	privacytypes.RegisterInterfaces(encoding.InterfaceRegistry)
	keyring, err := sdkkeyring.New(sdk.KeyringServiceName(), sdkkeyring.BackendTest, cfg.Home, nil, encoding.Codec)
	require.NoError(t, err)
	record, err := keyring.Key("alice")
	require.NoError(t, err)
	address, err := record.GetAddress()
	require.NoError(t, err)
	rpcClient, err := client.NewClientFromNode(cfg.Node)
	require.NoError(t, err)
	conn, err := grpc.NewClient(cfg.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	clientCtx := client.Context{}.
		WithCodec(encoding.Codec).
		WithLegacyAmino(encoding.Amino).
		WithInterfaceRegistry(encoding.InterfaceRegistry).
		WithTxConfig(encoding.TxConfig).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithKeyring(keyring).
		WithKeyringDir(cfg.Home).
		WithHomeDir(cfg.Home).
		WithFrom("alice").
		WithFromName("alice").
		WithFromAddress(address).
		WithChainID(cfg.ChainID).
		WithNodeURI(cfg.Node).
		WithClient(rpcClient).
		WithGRPCClient(conn).
		WithBroadcastMode(flags.BroadcastSync).
		WithSkipConfirmation(true).
		WithOutput(io.Discard).
		WithCmdContext(ctx)
	cmd := &cobra.Command{}
	flags.AddTxFlagsToCmd(cmd)
	require.NoError(t, cmd.Flags().Set(flags.FlagGas, cfg.Gas))
	require.NoError(t, cmd.Flags().Set(flags.FlagGasPrices, cfg.GasPrices))
	require.NoError(t, cmd.Flags().Set(flags.FlagBroadcastMode, flags.BroadcastSync))
	require.NoError(t, cmd.Flags().Set(flags.FlagChainID, cfg.ChainID))
	broadcaster := privacyprovider.CosmosTxBroadcaster{ClientContext: clientCtx, Flags: cmd.Flags(), FromName: "alice"}
	adapter := privacypayroll.CosmosBatchTxAdapter{Broadcaster: broadcaster}
	scan := privacyprovider.NewScanQueryProvider(rpcClient, privacytypes.NewQueryClient(conn))
	txService := txtypes.NewServiceClient(conn)
	return session3BLiveClients{
		Client: clientCtx, Conn: conn, TxService: txService, Scan: scan,
		Chain: privacypayroll.CosmosBatchChainReconciler{TxService: txService, Nullifiers: scan}, Adapter: adapter,
	}
}

func (c session3BLiveClients) Close() { _ = c.Conn.Close() }

type session3BTimeoutSender struct{ calls int }

func (s *session3BTimeoutSender) BroadcastSignedBatchTx(context.Context, []byte) (*privacypayroll.BatchBroadcastReceipt, error) {
	s.calls++
	return nil, context.DeadlineExceeded
}

type session3BRejectBuilder struct{ calls int }

func (b *session3BRejectBuilder) BuildSignedBatchTx(context.Context, *privacytypes.MsgBatchTransfer) (*privacypayroll.SignedBatchTx, error) {
	b.calls++
	return nil, fmt.Errorf("unexpected re-sign of durable batch transaction")
}

type session3BExactBytesSender struct {
	inner        privacypayroll.SignedBatchTxSender
	expectedHash string
	observedHash string
	calls        int
}

func (s *session3BExactBytesSender) BroadcastSignedBatchTx(ctx context.Context, signedTxBytes []byte) (*privacypayroll.BatchBroadcastReceipt, error) {
	s.calls++
	digest := sha256.Sum256(signedTxBytes)
	s.observedHash = hex.EncodeToString(digest[:])
	if !strings.EqualFold(s.expectedHash, s.observedHash) {
		return nil, fmt.Errorf("retry signed bytes differ from the timeout-staged bytes")
	}
	return s.inner.BroadcastSignedBatchTx(ctx, signedTxBytes)
}

type session3BOrderedReconciler struct {
	inner privacypayroll.BatchChainReconciler
	calls []string
}

func (r *session3BOrderedReconciler) LookupBatchTx(ctx context.Context, txHash string) (*privacypayroll.BatchTxLookupResult, error) {
	r.calls = append(r.calls, "lookup:"+strings.ToLower(txHash))
	return r.inner.LookupBatchTx(ctx, txHash)
}

func (r *session3BOrderedReconciler) CheckBatchNullifiers(ctx context.Context, values []string) (map[string]bool, error) {
	r.calls = append(r.calls, "nullifiers")
	return r.inner.CheckBatchNullifiers(ctx, values)
}

type session3BAbsentTxReconciler struct {
	inner privacypayroll.BatchChainReconciler
	calls []string
}

func (r *session3BAbsentTxReconciler) LookupBatchTx(context.Context, string) (*privacypayroll.BatchTxLookupResult, error) {
	r.calls = append(r.calls, "lookup")
	return &privacypayroll.BatchTxLookupResult{Found: false}, nil
}

func (r *session3BAbsentTxReconciler) CheckBatchNullifiers(ctx context.Context, values []string) (map[string]bool, error) {
	r.calls = append(r.calls, "nullifiers")
	return r.inner.CheckBatchNullifiers(ctx, values)
}

func findSession3BBatchOutputs(t *testing.T, ctx context.Context, scan privacyprovider.ScanQueryProvider, txHash string) (string, []*privacytypes.PrivacyScanOutputV2) {
	t.Helper()
	wantHash, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(txHash), "0x"))
	require.NoError(t, err)
	var cursor *privacytypes.PrivacyScanCursorV1
	for page := 0; page < 32; page++ {
		response, queryErr := scan.PrivacyScan(ctx, cursor, 128, 128, 4<<20, []string{privacytypes.EventTypeBatchTransferV1})
		require.NoError(t, queryErr)
		for _, summary := range response.Summaries {
			if !bytes.Equal(summary.TxHash, wantHash) {
				continue
			}
			outputs := make([]*privacytypes.PrivacyScanOutputV2, 0, summary.OutputCount)
			for _, output := range response.Outputs {
				if output.Height == summary.Height && output.GlobalSequence == summary.GlobalSequence {
					outputs = append(outputs, output)
				}
			}
			require.Len(t, outputs, int(summary.OutputCount))
			sort.Slice(outputs, func(i, j int) bool { return outputs[i].OutputIndex < outputs[j].OutputIndex })
			return hex.EncodeToString(summary.EffectId), outputs
		}
		if !response.HasMore {
			break
		}
		cursor = response.NextCursor
	}
	t.Fatalf("typed privacy scan did not return payroll tx %s", txHash)
	return "", nil
}

func waitSession3BTx(t *testing.T, ctx context.Context, service txtypes.ServiceClient, txHash string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := service.GetTx(ctx, &txtypes.GetTxRequest{Hash: txHash})
		if err == nil && response != nil && response.TxResponse != nil {
			require.Equal(t, uint32(0), response.TxResponse.Code)
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func readSession3BPayload(t *testing.T, path string) *privacybatchtransfer.PreparedBatchTransferPayload {
	t.Helper()
	payload, err := privacybatchtransfer.ReadPreparedBatchTransferPayload(path)
	require.NoError(t, err)
	require.NoError(t, privacybatchtransfer.ValidatePreparedBatchTransferPayloadMetadataAt(payload, time.Now()))
	return payload
}

func readSession3BProof(t *testing.T, path string) *privacybatchtransfer.PreparedBatchTransferProof {
	t.Helper()
	proof, err := privacybatchtransfer.ReadPreparedBatchTransferProof(path)
	require.NoError(t, err)
	return proof
}

func readSession3BState(t *testing.T, path string) session3BPayrollState {
	t.Helper()
	var state session3BPayrollState
	readSession3BJSON(t, path, &state)
	require.Equal(t, session3BPayrollStateSchema, state.SchemaVersion)
	require.NotEmpty(t, state.OperationID)
	if state.StagePIDs == nil {
		state.StagePIDs = make(map[string]int)
	}
	return state
}

func recordSession3BStage(t *testing.T, cfg session3BLiveConfig, state *session3BPayrollState, stage string) {
	t.Helper()
	if state.StagePIDs == nil {
		state.StagePIDs = make(map[string]int)
	}
	state.StagePIDs[stage] = os.Getpid()
	writeSession3BJSON(t, cfg.StatePath, state)
}

func assertSession3BFinalState(t *testing.T, state session3BPayrollState) {
	t.Helper()
	require.True(t, state.TimeoutBeforeSend)
	require.True(t, state.ExactStoredBytesRetry)
	require.True(t, state.TxHashFirst)
	require.True(t, state.SpentNullifierConflict)
	require.Equal(t, state.TimeoutBytesHash, state.RetryBytesHash)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, state.ChainStatus)
	require.Equal(t, 2, state.BroadcastAttempts)
	require.Equal(t, 3, state.InputCount)
	require.Equal(t, 4, state.OutputCount)
	require.Equal(t, 3, state.SucceededItems)
	require.Equal(t, 3, state.ManualReviewItems)
	require.Equal(t, privacyreservation.OperationStatusManualReview, state.ConflictStatus)
	wantStages := []string{"graph", "prove", "timeout", "retry", "reconcile", "conflict"}
	seen := make(map[int]string, len(wantStages))
	for _, stage := range wantStages {
		pid := state.StagePIDs[stage]
		require.Positive(t, pid, stage)
		if previous := seen[pid]; previous != "" {
			t.Fatalf("stage %s reused process %d from %s", stage, pid, previous)
		}
		seen[pid] = stage
	}
}

func writeSession3BJSON(t *testing.T, path string, value any) {
	t.Helper()
	bz, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	bz = append(bz, '\n')
	require.NoError(t, privatefile.Write(path, bz))
}

func readSession3BJSON(t *testing.T, path string, value any) {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(bz))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(value))
}

func session3BInputAmounts(payload *privacybatchtransfer.PreparedBatchTransferPayload) []int64 {
	values := make([]int64, len(payload.Inputs))
	for i := range payload.Inputs {
		values[i] = payload.Inputs[i].Note.Amount.Int64()
	}
	return values
}

func session3BOutputAmounts(payload *privacybatchtransfer.PreparedBatchTransferPayload) []int64 {
	values := make([]int64, len(payload.Outputs))
	for i := range payload.Outputs {
		values[i] = payload.Outputs[i].Note.Amount.Int64()
	}
	return values
}
