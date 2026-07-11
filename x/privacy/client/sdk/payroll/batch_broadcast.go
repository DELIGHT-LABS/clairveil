package payroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type PayrollArtifactCipher interface {
	PayrollEvidenceSealer
	OpenPayrollEvidence(ctx context.Context, ciphertext []byte) ([]byte, error)
}

type SignedBatchTx struct {
	Bytes           []byte
	TxBytesHash     string
	SignDocHash     string
	TxHash          string
	AccountSequence uint64
}

type SignedBatchTxBuilder interface {
	BuildSignedBatchTx(ctx context.Context, msg *privacytypes.MsgBatchTransfer) (*SignedBatchTx, error)
}

type SignedBatchTxSender interface {
	BroadcastSignedBatchTx(ctx context.Context, signedTxBytes []byte) (*BatchBroadcastReceipt, error)
}

type BatchBroadcastReceipt struct {
	TxHash  string
	Height  int64
	Code    uint32
	RawLog  string
	Unknown bool
}

type BatchTxLookupResult struct {
	Found     bool
	Succeeded bool
	Failed    bool
	TxHash    string
	Height    int64
	Code      uint32
}

type BatchChainReconciler interface {
	LookupBatchTx(ctx context.Context, txHash string) (*BatchTxLookupResult, error)
	CheckBatchNullifiers(ctx context.Context, nullifierHexes []string) (map[string]bool, error)
}

type BatchBroadcastOptions struct {
	// ResignWithNewSequence is never automatic. When true, the worker first
	// proves the prior tx hash is absent and every input nullifier is unspent.
	ResignWithNewSequence bool
}

type BatchBroadcastOutcome struct {
	Receipt                 *BatchBroadcastReceipt
	ReconciledExistingTx    bool
	NullifierEvidenceExists bool
	UsedStoredSignedBytes   bool
	TxBytesHash             string
}

type IdempotentBatchBroadcastWorker struct {
	Store      privacyreservation.BatchOperationStore
	Builder    SignedBatchTxBuilder
	Sender     SignedBatchTxSender
	Reconciler BatchChainReconciler
	Cipher     PayrollArtifactCipher
	LeaseOwner string
	LeaseTTL   time.Duration
	Now        func() time.Time
}

func (w IdempotentBatchBroadcastWorker) Submit(ctx context.Context, operationID string, payload *privacybatchtransfer.PreparedBatchTransferPayload, proof *privacybatchtransfer.PreparedBatchTransferProof, creator string, options BatchBroadcastOptions) (*BatchBroadcastOutcome, error) {
	if w.Store == nil || w.Builder == nil || w.Sender == nil || w.Reconciler == nil || w.Cipher == nil || strings.TrimSpace(w.LeaseOwner) == "" {
		return nil, fmt.Errorf("batch broadcast store, builder, sender, reconciler, cipher, and lease owner are required")
	}
	if err := privacybatchtransfer.ValidatePreparedBatchTransferProofAt(payload, proof, w.now()); err != nil {
		return nil, err
	}
	graph, err := w.Store.GetBatchOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if graph.Operation.PreparedPayloadHash != payload.PayloadHash {
		return nil, fmt.Errorf("batch operation payload hash mismatch")
	}
	nullifiers := batchPayloadNullifierHexes(payload)
	outcome := &BatchBroadcastOutcome{}
	priorLookup := &BatchTxLookupResult{}
	if graph.Operation.TxHash != "" {
		priorLookup, err = w.Reconciler.LookupBatchTx(ctx, graph.Operation.TxHash)
		if err != nil {
			return nil, fmt.Errorf("lookup prior batch tx hash: %w", err)
		}
		if priorLookup != nil && priorLookup.Found {
			if priorLookup.Succeeded == priorLookup.Failed {
				return nil, fmt.Errorf("found prior batch tx must be exactly one of succeeded or failed")
			}
			if priorLookup.TxHash != "" && !strings.EqualFold(strings.TrimPrefix(priorLookup.TxHash, "0x"), strings.TrimPrefix(graph.Operation.TxHash, "0x")) {
				return nil, fmt.Errorf("prior batch tx lookup hash does not match the durable tx hash")
			}
			outcome.ReconciledExistingTx = true
			outcome.Receipt = &BatchBroadcastReceipt{TxHash: priorLookup.TxHash, Height: priorLookup.Height, Code: priorLookup.Code}
			if priorLookup.Failed {
				return outcome, fmt.Errorf("prior batch tx failed on chain with code %d", priorLookup.Code)
			}
			return outcome, nil
		}
	}
	used, err := w.Reconciler.CheckBatchNullifiers(ctx, nullifiers)
	if err != nil {
		return nil, fmt.Errorf("check batch nullifiers before broadcast: %w", err)
	}
	allKnown, anySpent := batchNullifierStatuses(nullifiers, used)
	if !allKnown {
		return nil, fmt.Errorf("batch nullifier reconciliation response is incomplete")
	}
	if anySpent {
		outcome.NullifierEvidenceExists = true
		return outcome, nil
	}
	if options.ResignWithNewSequence && graph.Operation.TxBytesHash != "" && graph.Operation.Status != privacyreservation.OperationStatusUnknown {
		return nil, fmt.Errorf("new-sequence re-sign requires an Unknown prior broadcast")
	}

	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := w.now()
	leaseToken, err := newBatchLeaseToken()
	if err != nil {
		return nil, err
	}
	lease, err := w.Store.AcquireBatchOperationLease(ctx, operationID, w.LeaseOwner, leaseToken, now.Add(ttl), now)
	if err != nil {
		return nil, err
	}
	leaseHeld := true
	defer func() {
		if leaseHeld {
			_, _ = w.Store.ReleaseBatchOperationLease(context.Background(), operationID, lease.LeaseToken, w.now())
		}
	}()
	freshGraph, err := w.Store.GetBatchOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if freshGraph.Operation.Status != graph.Operation.Status || !strings.EqualFold(freshGraph.Operation.TxBytesHash, graph.Operation.TxBytesHash) || !strings.EqualFold(freshGraph.Operation.TxHash, graph.Operation.TxHash) {
		return nil, fmt.Errorf("batch operation changed during broadcast admission; reconcile and retry")
	}
	graph = freshGraph

	msg, err := privacybatchtransfer.BuildMsgBatchTransfer(payload, proof, creator)
	if err != nil {
		return nil, err
	}
	var signed *SignedBatchTx
	if graph.Operation.TxBytesHash != "" && !options.ResignWithNewSequence {
		storedBytes, err := w.Cipher.OpenPayrollEvidence(ctx, graph.Operation.SignedTxBytesCiphertext)
		if err != nil {
			return nil, fmt.Errorf("open stored signed batch tx: %w", err)
		}
		digest := sha256.Sum256(storedBytes)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), graph.Operation.TxBytesHash) {
			return nil, fmt.Errorf("stored signed batch tx hash mismatch")
		}
		signed = &SignedBatchTx{Bytes: storedBytes, TxBytesHash: graph.Operation.TxBytesHash, SignDocHash: graph.Operation.SignDocHash, TxHash: graph.Operation.TxHash, AccountSequence: graph.Operation.AccountSequence}
		if err := validateSignedBatchTx(signed); err != nil {
			return nil, err
		}
		outcome.UsedStoredSignedBytes = true
	} else {
		signed, err = w.Builder.BuildSignedBatchTx(ctx, msg)
		if err != nil {
			return nil, err
		}
		if err := validateSignedBatchTx(signed); err != nil {
			return nil, err
		}
	}
	encryptedSignedBytes, err := w.Cipher.SealPayrollEvidence(context.Background(), signed.Bytes)
	if err != nil {
		return nil, fmt.Errorf("seal signed batch tx: %w", err)
	}
	if _, err := w.Store.SaveBatchSignedTx(context.Background(), operationID, lease.LeaseToken, privacyreservation.BatchSignedTxUpdate{
		SignedTxBytesCiphertext: encryptedSignedBytes, TxBytesHash: signed.TxBytesHash, SignDocHash: signed.SignDocHash,
		TxHash: signed.TxHash, AccountSequence: signed.AccountSequence,
	}, w.now()); err != nil {
		return nil, err
	}
	usedImmediatelyBeforeBroadcast, err := w.Reconciler.CheckBatchNullifiers(ctx, nullifiers)
	if err != nil {
		return nil, fmt.Errorf("check batch nullifiers immediately before broadcast: %w", err)
	}
	allKnown, anySpent = batchNullifierStatuses(nullifiers, usedImmediatelyBeforeBroadcast)
	if !allKnown {
		return nil, fmt.Errorf("pre-broadcast batch nullifier reconciliation response is incomplete")
	}
	if anySpent {
		outcome.NullifierEvidenceExists = true
		return outcome, nil
	}

	receipt, broadcastErr := w.Sender.BroadcastSignedBatchTx(ctx, signed.Bytes)
	unknown := broadcastErr != nil || receipt == nil || receipt.Unknown || (receipt != nil && receipt.Code != 0)
	txHash := signed.TxHash
	lastError := ""
	if receipt != nil && receipt.TxHash != "" {
		if !strings.EqualFold(strings.TrimPrefix(receipt.TxHash, "0x"), strings.TrimPrefix(signed.TxHash, "0x")) {
			broadcastErr = errors.Join(broadcastErr, fmt.Errorf("broadcast receipt tx hash does not match the signed bytes"))
			unknown = true
		} else {
			txHash = receipt.TxHash
		}
	}
	if broadcastErr != nil {
		lastError = broadcastErr.Error()
	} else if receipt != nil && receipt.Code != 0 {
		lastError = fmt.Sprintf("batch tx returned code %d: %s", receipt.Code, receipt.RawLog)
	}
	_, recordErr := w.Store.RecordBatchBroadcast(context.Background(), operationID, lease.LeaseToken, privacyreservation.BatchBroadcastUpdate{
		SignedTxBytesCiphertext: encryptedSignedBytes, TxBytesHash: signed.TxBytesHash, SignDocHash: signed.SignDocHash,
		TxHash: txHash, AccountSequence: signed.AccountSequence, LastBroadcastError: lastError, Unknown: unknown,
	}, w.now())
	if recordErr == nil {
		leaseHeld = false
	}
	outcome.Receipt = receipt
	outcome.TxBytesHash = signed.TxBytesHash
	if recordErr != nil {
		return outcome, errors.Join(broadcastErr, recordErr)
	}
	if broadcastErr != nil {
		return outcome, broadcastErr
	}
	if receipt != nil && receipt.Code != 0 {
		return outcome, fmt.Errorf("batch tx failed with code %d: %s", receipt.Code, receipt.RawLog)
	}
	return outcome, nil
}

func validateSignedBatchTx(signed *SignedBatchTx) error {
	if signed == nil || len(signed.Bytes) == 0 || strings.TrimSpace(signed.TxBytesHash) == "" || strings.TrimSpace(signed.TxHash) == "" {
		return fmt.Errorf("signed batch tx bytes/hash and tx hash are required")
	}
	digest := sha256.Sum256(signed.Bytes)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), signed.TxBytesHash) {
		return fmt.Errorf("signed batch tx bytes/hash mismatch")
	}
	if !strings.EqualFold(hex.EncodeToString(digest[:]), strings.TrimPrefix(signed.TxHash, "0x")) {
		return fmt.Errorf("signed batch tx CometBFT hash mismatch")
	}
	return nil
}

func batchPayloadNullifierHexes(payload *privacybatchtransfer.PreparedBatchTransferPayload) []string {
	values := make([]string, len(payload.Inputs))
	for i := range payload.Inputs {
		values[i] = hex.EncodeToString(payload.Inputs[i].Nullifier)
	}
	return values
}

func batchNullifierStatuses(nullifiers []string, statuses map[string]bool) (bool, bool) {
	anySpent := false
	for _, nullifier := range nullifiers {
		spent, ok := statuses[nullifier]
		if !ok {
			return false, anySpent
		}
		anySpent = anySpent || spent
	}
	return true, anySpent
}

func (w IdempotentBatchBroadcastWorker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
