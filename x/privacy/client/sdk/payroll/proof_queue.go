package payroll

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type PreparedPayloadBuilder interface {
	BuildPreparedTransferPayload(ctx context.Context, item PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error)
}

type PreparedProofRunner interface {
	BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error)
}

type TransferMessageAssembler interface {
	BuildTransferMessage(payload privacytransfer.PreparedTransferPayload, proof privacytransfer.PreparedTransferProof) (*privacytypes.MsgTransfer, error)
}

type ProofResultSink interface {
	SaveProofResult(ctx context.Context, result ProofResult) error
}

type ProofResultStore interface {
	ProofResultSink
	GetProofResult(ctx context.Context, operationID string) (*ProofResult, error)
}

type DefaultTransferMessageAssembler struct{}

func (DefaultTransferMessageAssembler) BuildTransferMessage(payload privacytransfer.PreparedTransferPayload, proof privacytransfer.PreparedTransferProof) (*privacytypes.MsgTransfer, error) {
	return payload.ToMsg(proof)
}

type ProofWorker struct {
	Reservation     privacyreservation.Service
	PayloadBuilder  PreparedPayloadBuilder
	ProofRunner     PreparedProofRunner
	Assembler       TransferMessageAssembler
	ProofResultSink ProofResultSink
	LeaseOwner      string
	LeaseTTL        time.Duration
}

type ProofResult struct {
	Item              PayrollPlanItem
	Payload           privacytransfer.PreparedTransferPayload
	Proof             privacytransfer.PreparedTransferProof
	Message           *privacytypes.MsgTransfer
	ReservationLeases map[string]string
}

type MemoryProofResultStore struct {
	mu      sync.Mutex
	results map[string]ProofResult
}

func NewMemoryProofResultStore() *MemoryProofResultStore {
	return &MemoryProofResultStore{results: make(map[string]ProofResult)}
}

func (s *MemoryProofResultStore) SaveProofResult(ctx context.Context, result ProofResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if result.Item.OperationID == "" {
		return fmt.Errorf("%w: proof result has no operation_id", ErrInvalidPayrollInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = make(map[string]ProofResult)
	}
	s.results[result.Item.OperationID] = cloneProofResult(result)
	return nil
}

func (s *MemoryProofResultStore) GetProofResult(ctx context.Context, operationID string) (*ProofResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[operationID]
	if !ok {
		return nil, fmt.Errorf("proof result %s not found", operationID)
	}
	cloned := cloneProofResult(result)
	return &cloned, nil
}

func (w ProofWorker) Process(ctx context.Context, item PayrollPlanItem) (_ *ProofResult, runErr error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if w.PayloadBuilder == nil {
		return nil, fmt.Errorf("a prepared transfer payload builder is required")
	}
	if w.ProofRunner == nil {
		return nil, fmt.Errorf("a prepared transfer proof runner is required")
	}
	if w.ProofResultSink == nil {
		return nil, fmt.Errorf("a proof result sink is required")
	}
	assembler := w.Assembler
	if assembler == nil {
		assembler = DefaultTransferMessageAssembler{}
	}
	if w.LeaseOwner == "" {
		return nil, fmt.Errorf("lease owner is required")
	}
	leaseTTL := w.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	if len(item.InputNotes) == 0 {
		return nil, fmt.Errorf("payroll item %s has no reserved input notes", item.ItemID)
	}

	leases := make(map[string]string, len(item.InputNotes))
	acquiredReservations := make([]string, 0, len(item.InputNotes))
	provingReservations := make([]string, 0, len(item.InputNotes))
	defer func() {
		if runErr == nil {
			return
		}
		if cleanupErr := w.rollbackProofReservations(ctx, leases, acquiredReservations, provingReservations); cleanupErr != nil {
			runErr = errors.Join(runErr, cleanupErr)
		}
	}()
	for _, note := range item.InputNotes {
		if note.ReservationID == "" {
			return nil, fmt.Errorf("payroll item %s input note %s has no reservation", item.ItemID, note.NoteID)
		}
		lease, err := w.Reservation.AcquireLeaseForStatus(ctx, note.ReservationID, w.LeaseOwner, privacyreservation.StatusReserved, leaseTTL)
		if err != nil {
			return nil, err
		}
		leases[note.ReservationID] = lease.Token
		acquiredReservations = append(acquiredReservations, note.ReservationID)
		if _, err := w.Reservation.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving); err != nil {
			return nil, err
		}
		provingReservations = append(provingReservations, note.ReservationID)
	}

	proofCtx, stopHeartbeat := w.startProvingHeartbeat(ctx, leases, provingReservations, leaseTTL)
	defer func() {
		if stopHeartbeat == nil {
			return
		}
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil && runErr == nil {
			runErr = heartbeatErr
		}
	}()

	payload, err := w.PayloadBuilder.BuildPreparedTransferPayload(proofCtx, item)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("prepared transfer payload builder returned nil")
	}
	proof, err := w.ProofRunner.BuildPreparedTransferProof(proofCtx, *payload)
	if err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, fmt.Errorf("prepared transfer proof runner returned nil")
	}
	msg, err := assembler.BuildTransferMessage(*payload, *proof)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("transfer message assembler returned nil")
	}
	result := ProofResult{
		Item:              clonePlanItem(item),
		Payload:           *payload,
		Proof:             *proof,
		Message:           msg,
		ReservationLeases: cloneStringMap(leases),
	}
	if err := w.ProofResultSink.SaveProofResult(proofCtx, result); err != nil {
		return nil, err
	}

	refs, err := proofReadyReservationRefs(item, leases)
	if err != nil {
		return nil, err
	}
	update := privacyreservation.ProofReadyOperationUpdate{
		OperationID:              item.OperationID,
		ExpectedDisclosureDigest: preferredDisclosureDigest(*payload),
	}
	if len(payload.Outputs) > 0 {
		update.ExpectedOutputCommitment = payload.Outputs[0].CommitmentHex
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		stopHeartbeat = nil
		return nil, heartbeatErr
	}
	stopHeartbeat = nil
	if _, _, err := w.Reservation.MarkProofReadyBatch(ctx, refs, update); err != nil {
		return nil, err
	}

	return &result, nil
}

func (w ProofWorker) startProvingHeartbeat(ctx context.Context, leases map[string]string, reservationIDs []string, ttl time.Duration) (context.Context, func() error) {
	if len(reservationIDs) == 0 {
		return ctx, func() error { return nil }
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	interval := ttl / 3
	if interval <= 0 {
		interval = ttl
	}
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
				for _, reservationID := range reservationIDs {
					token := leases[reservationID]
					if token == "" {
						continue
					}
					if _, err := w.Reservation.HeartbeatLeaseForStatus(heartbeatCtx, reservationID, token, privacyreservation.StatusProving, ttl); err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
				}
			}
		}
	}()

	var stopOnce sync.Once
	return heartbeatCtx, func() error {
		stopOnce.Do(func() {
			cancel()
			<-done
		})
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}
}

func (w ProofWorker) rollbackProofReservations(ctx context.Context, leases map[string]string, acquiredReservationIDs []string, provingReservationIDs []string) error {
	var cleanupErr error
	proving := make(map[string]struct{}, len(provingReservationIDs))
	for _, reservationID := range provingReservationIDs {
		proving[reservationID] = struct{}{}
		token := leases[reservationID]
		if token == "" {
			continue
		}
		if _, err := w.Reservation.TransitionWithLease(ctx, reservationID, token, privacyreservation.StatusProving, privacyreservation.StatusReserved); err != nil {
			if !errors.Is(err, privacyreservation.ErrCompareAndSetFailed) && !errors.Is(err, privacyreservation.ErrLeaseMismatch) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			continue
		}
		if _, err := w.Reservation.ClearLease(ctx, reservationID, token); err != nil && !errors.Is(err, privacyreservation.ErrLeaseUnavailable) && !errors.Is(err, privacyreservation.ErrLeaseMismatch) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	for _, reservationID := range acquiredReservationIDs {
		if _, ok := proving[reservationID]; ok {
			continue
		}
		token := leases[reservationID]
		if token == "" {
			continue
		}
		if _, err := w.Reservation.ClearLease(ctx, reservationID, token); err != nil && !errors.Is(err, privacyreservation.ErrLeaseUnavailable) && !errors.Is(err, privacyreservation.ErrLeaseMismatch) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func cloneProofResult(result ProofResult) ProofResult {
	result.Item = clonePlanItem(result.Item)
	result.Payload = clonePreparedTransferPayload(result.Payload)
	result.Message = cloneTransferMessage(result.Message)
	result.ReservationLeases = cloneStringMap(result.ReservationLeases)
	return result
}

func clonePreparedTransferPayload(payload privacytransfer.PreparedTransferPayload) privacytransfer.PreparedTransferPayload {
	payload.Inputs = append([]privacytransfer.PreparedTransferInput(nil), payload.Inputs...)
	for i := range payload.Inputs {
		payload.Inputs[i].MerklePath = append([]string(nil), payload.Inputs[i].MerklePath...)
		payload.Inputs[i].MerklePathHelper = append([]uint32(nil), payload.Inputs[i].MerklePathHelper...)
	}
	payload.Outputs = append([]privacytransfer.PreparedTransferOutput(nil), payload.Outputs...)
	payload.CipherTextHexes = append([]string(nil), payload.CipherTextHexes...)
	payload.ViewTagHexes = append([]string(nil), payload.ViewTagHexes...)
	return payload
}

func cloneTransferMessage(message *privacytypes.MsgTransfer) *privacytypes.MsgTransfer {
	if message == nil {
		return nil
	}
	cloned := *message
	cloned.Proof = append([]byte(nil), message.Proof...)
	cloned.Root = append([]byte(nil), message.Root...)
	cloned.Nullifiers = cloneBytesSlice(message.Nullifiers)
	cloned.NewCommitments = cloneBytesSlice(message.NewCommitments)
	cloned.CipherTexts = cloneBytesSlice(message.CipherTexts)
	cloned.UserDisclosureDigest = append([]byte(nil), message.UserDisclosureDigest...)
	cloned.UserDisclosureTargetPubkey = append([]byte(nil), message.UserDisclosureTargetPubkey...)
	cloned.UserDisclosurePayload = append([]byte(nil), message.UserDisclosurePayload...)
	cloned.AuditDisclosureDigest = append([]byte(nil), message.AuditDisclosureDigest...)
	cloned.AuditDisclosureTargetPubkey = append([]byte(nil), message.AuditDisclosureTargetPubkey...)
	cloned.AuditDisclosurePayload = append([]byte(nil), message.AuditDisclosurePayload...)
	cloned.SelfViewDisclosureDigest = append([]byte(nil), message.SelfViewDisclosureDigest...)
	cloned.SelfViewDisclosurePayload = append([]byte(nil), message.SelfViewDisclosurePayload...)
	cloned.ViewTags = cloneBytesSlice(message.ViewTags)
	return &cloned
}

func cloneBytesSlice(values [][]byte) [][]byte {
	out := make([][]byte, len(values))
	for i := range values {
		out[i] = append([]byte(nil), values[i]...)
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func preferredDisclosureDigest(payload privacytransfer.PreparedTransferPayload) string {
	return payload.AuditDisclosureDigestHex
}

func proofReadyReservationRefs(item PayrollPlanItem, leases map[string]string) ([]privacyreservation.SubmittedReservationRef, error) {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	for _, note := range item.InputNotes {
		token := leases[note.ReservationID]
		if note.ReservationID == "" || token == "" {
			return nil, fmt.Errorf("payroll item %s has no lease token for reservation %s", item.ItemID, note.ReservationID)
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{
			ReservationID: note.ReservationID,
			LeaseToken:    token,
		})
	}
	return refs, nil
}
