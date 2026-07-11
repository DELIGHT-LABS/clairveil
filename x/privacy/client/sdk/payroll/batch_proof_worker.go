package payroll

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

// BatchPayrollProver represents exactly one local prover or one explicitly
// selected remote endpoint. No pool/failover behavior is part of this API.
type BatchPayrollProver interface {
	ProveBatchPayroll(ctx context.Context, payload *privacybatchtransfer.PreparedBatchTransferPayload, now time.Time) (*privacybatchtransfer.PreparedBatchTransferProof, error)
}

type LocalBatchPayrollProver struct {
	Artifacts privacybatchtransfer.BatchJoinSplitArtifactProvider
	Runner    privacybatchtransfer.BatchJoinSplitProofRunner
}

func (p LocalBatchPayrollProver) ProveBatchPayroll(_ context.Context, payload *privacybatchtransfer.PreparedBatchTransferPayload, now time.Time) (*privacybatchtransfer.PreparedBatchTransferProof, error) {
	return privacybatchtransfer.BuildPreparedBatchTransferProofAt(payload, p.Artifacts, p.Runner, now)
}

type RemoteBatchProofClient interface {
	ProveBatchTransfer(ctx context.Context, request privacyprovertransport.BatchTransferProofRequest) (*privacyprovertransport.BatchTransferProofResponse, error)
}

type RemoteBatchPayrollProver struct{ Client RemoteBatchProofClient }

func (p RemoteBatchPayrollProver) ProveBatchPayroll(ctx context.Context, payload *privacybatchtransfer.PreparedBatchTransferPayload, now time.Time) (*privacybatchtransfer.PreparedBatchTransferProof, error) {
	if p.Client == nil {
		return nil, fmt.Errorf("one explicit remote batch prover client is required")
	}
	request, err := privacyprovertransport.NewBatchTransferProofRequestAt(*payload, now)
	if err != nil {
		return nil, err
	}
	response, err := p.Client.ProveBatchTransfer(ctx, *request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("remote batch prover returned no response")
	}
	if err := privacyprovertransport.ValidateBatchTransferProofResponseAt(*request, *response, now); err != nil {
		return nil, err
	}
	proof := response.Proof
	return &proof, nil
}

type BatchProofWorker struct {
	Store      privacyreservation.BatchOperationStore
	Prover     BatchPayrollProver
	Sealer     PayrollEvidenceSealer
	LeaseOwner string
	LeaseTTL   time.Duration
	Now        func() time.Time
}

func (w BatchProofWorker) Process(ctx context.Context, operationID string, payload *privacybatchtransfer.PreparedBatchTransferPayload) (_ *privacybatchtransfer.PreparedBatchTransferProof, runErr error) {
	if w.Store == nil || w.Prover == nil || w.Sealer == nil || w.LeaseOwner == "" {
		return nil, fmt.Errorf("batch proof worker store, prover, sealer, and lease owner are required")
	}
	if payload == nil {
		return nil, fmt.Errorf("prepared batch payload is required")
	}
	now := w.now()
	if err := privacybatchtransfer.ValidatePreparedBatchTransferPayloadMetadataAt(payload, now); err != nil {
		return nil, err
	}
	graph, err := w.Store.GetBatchOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if graph.Operation.PreparedPayloadHash != payload.PayloadHash {
		return nil, fmt.Errorf("durable batch operation does not match the prepared payload")
	}
	if graph.Operation.Status == privacyreservation.OperationStatusProving && !graph.Operation.LeaseUntil.After(now) {
		if err := w.recoverExpiredProvingOperation(ctx, operationID, now); err != nil {
			return nil, err
		}
		graph, err = w.Store.GetBatchOperation(ctx, operationID)
		if err != nil {
			return nil, err
		}
	}
	if graph.Operation.Status != privacyreservation.OperationStatusPlanned {
		return nil, fmt.Errorf("durable batch operation is not ready for proving: %s", graph.Operation.Status)
	}
	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	leaseToken, err := newBatchLeaseToken()
	if err != nil {
		return nil, err
	}
	lease, err := w.Store.AcquireBatchOperationLease(ctx, operationID, w.LeaseOwner, leaseToken, now.Add(ttl), now)
	if err != nil {
		return nil, err
	}
	if _, err := w.Store.CompareAndSetBatchOperationStatus(ctx, operationID, lease.LeaseToken, privacyreservation.OperationStatusPlanned, privacyreservation.OperationStatusProving, now); err != nil {
		return nil, err
	}
	proofCtx, stopHeartbeat := w.startHeartbeat(ctx, operationID, lease.LeaseToken, ttl)
	defer func() {
		if stopHeartbeat != nil {
			if heartbeatErr := stopHeartbeat(); heartbeatErr != nil && runErr == nil {
				runErr = heartbeatErr
			}
		}
		if runErr != nil {
			_, rollbackErr := w.Store.CompareAndSetBatchOperationStatus(context.Background(), operationID, lease.LeaseToken, privacyreservation.OperationStatusProving, privacyreservation.OperationStatusPlanned, w.now())
			if rollbackErr != nil && !errors.Is(rollbackErr, privacyreservation.ErrCompareAndSetFailed) && !errors.Is(rollbackErr, privacyreservation.ErrLeaseMismatch) {
				runErr = errors.Join(runErr, rollbackErr)
			}
		}
	}()

	proof, err := w.Prover.ProveBatchPayroll(proofCtx, payload, now)
	if err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, fmt.Errorf("batch prover returned nil proof")
	}
	if err := privacybatchtransfer.ValidatePreparedBatchTransferProofAt(payload, proof, w.now()); err != nil {
		return nil, err
	}
	proofBytes, err := json.Marshal(proof)
	if err != nil {
		return nil, err
	}
	encryptedProof, err := w.Sealer.SealPayrollEvidence(context.Background(), proofBytes)
	if err != nil {
		return nil, fmt.Errorf("seal batch proof artifact: %w", err)
	}
	proofDigest := sha256.Sum256(proof.Proof)
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		stopHeartbeat = nil
		return nil, heartbeatErr
	}
	stopHeartbeat = nil
	if _, err := w.Store.SaveBatchProofArtifacts(context.Background(), operationID, lease.LeaseToken, privacyreservation.BatchProofArtifactUpdate{
		ProofCiphertext: encryptedProof, ProofHash: hex.EncodeToString(proofDigest[:]),
	}, w.now()); err != nil {
		return nil, err
	}
	return proof, nil
}

func (w BatchProofWorker) recoverExpiredProvingOperation(ctx context.Context, operationID string, now time.Time) error {
	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	token, err := newBatchLeaseToken()
	if err != nil {
		return err
	}
	lease, err := w.Store.AcquireBatchOperationLease(ctx, operationID, w.LeaseOwner, token, now.Add(ttl), now)
	if err != nil {
		return fmt.Errorf("acquire expired proving operation for recovery: %w", err)
	}
	if _, err := w.Store.CompareAndSetBatchOperationStatus(ctx, operationID, lease.LeaseToken, privacyreservation.OperationStatusProving, privacyreservation.OperationStatusPlanned, now); err != nil {
		_, _ = w.Store.ReleaseBatchOperationLease(context.Background(), operationID, lease.LeaseToken, now)
		return fmt.Errorf("recover expired proving operation: %w", err)
	}
	return nil
}

func (w BatchProofWorker) startHeartbeat(ctx context.Context, operationID, leaseToken string, ttl time.Duration) (context.Context, func() error) {
	// The lease heartbeat is intentionally independent of caller cancellation:
	// a local prover may not be interruptible, so its permit/lease must remain
	// held until the actual Prove call returns. Heartbeat failure still cancels
	// cooperative local/remote provers.
	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	proofCtx, cancelProof := context.WithCancel(ctx)
	interval := ttl / 3
	if interval <= 0 {
		interval = time.Second
	}
	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				now := w.now()
				if _, err := w.Store.HeartbeatBatchOperationLease(heartbeatCtx, operationID, leaseToken, now.Add(ttl), now); err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancelProof()
					return
				}
			}
		}
	}()
	var once sync.Once
	return proofCtx, func() error {
		once.Do(func() {
			cancelHeartbeat()
			<-done
			cancelProof()
		})
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}
}

func (w BatchProofWorker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func newBatchLeaseToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
