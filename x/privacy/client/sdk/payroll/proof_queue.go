package payroll

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

// ProofResultOutbox keeps generated proof artifacts invisible until the linked
// reservation batch has durably reached ProofReady. Implementations must keep
// staged results across a process restart so a recovery worker can publish a
// result after a crash between the reservation CAS and the publish step.
type ProofResultOutbox interface {
	ProofResultSink
	StageProofResult(ctx context.Context, result ProofResult) error
	PublishStagedProofResult(ctx context.Context, operationID string) error
	DiscardStagedProofResult(ctx context.Context, operationID string) error
	// DiscardPublishedProofResult must atomically remove staged and published
	// artifacts and durably tombstone the operation so an in-flight recovery
	// cannot publish the stale proof after re-planning begins.
	DiscardPublishedProofResult(ctx context.Context, operationID string) error
	GetStagedProofResult(ctx context.Context, operationID string) (*ProofResult, error)
	GetProofResult(ctx context.Context, operationID string) (*ProofResult, error)
}

type ProofResultStore interface {
	ProofResultOutbox
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
	// NullifierChecker is required immediately before the prover call. Planning
	// time checks alone are insufficient because another worker may consume an
	// input note while a payload is being assembled.
	NullifierChecker BroadcastNullifierChecker
	LeaseOwner       string
	LeaseTTL         time.Duration
}

type ProofResult struct {
	Item                   PayrollPlanItem
	Payload                privacytransfer.PreparedTransferPayload
	Proof                  privacytransfer.PreparedTransferProof
	Message                *privacytypes.MsgTransfer
	ReservationLeases      map[string]string
	ReservationLeaseOwners map[string]string
}

func validateProofResultArtifact(result ProofResult, assembler TransferMessageAssembler) error {
	if assembler == nil {
		assembler = DefaultTransferMessageAssembler{}
	}
	payloadHash := strings.TrimSpace(result.Payload.PayloadHash)
	computedPayloadHash := privacytransfer.ComputePreparedTransferPayloadHash(result.Payload)
	if payloadHash == "" || payloadHash != computedPayloadHash || result.Proof.PayloadHash != payloadHash {
		return fmt.Errorf("proof result payload hash mismatch")
	}
	if result.Proof.Version != privacytransfer.PreparedTransferProofVersion {
		return fmt.Errorf("proof result has unsupported proof version %q", result.Proof.Version)
	}
	if strings.TrimSpace(result.Proof.ProofHex) == "" {
		return fmt.Errorf("proof result has empty proof hex")
	}
	if _, err := hex.DecodeString(result.Proof.ProofHex); err != nil {
		return fmt.Errorf("proof result has invalid proof hex: %w", err)
	}
	expectedMessage, err := assembler.BuildTransferMessage(result.Payload, result.Proof)
	if err != nil {
		return fmt.Errorf("rebuild proof result message: %w", err)
	}
	if result.Message == nil || expectedMessage == nil {
		return fmt.Errorf("proof result message does not match payload and proof")
	}
	actualMessageBytes, err := result.Message.Marshal()
	if err != nil {
		return fmt.Errorf("marshal proof result message: %w", err)
	}
	expectedMessageBytes, err := expectedMessage.Marshal()
	if err != nil {
		return fmt.Errorf("marshal rebuilt proof result message: %w", err)
	}
	if !bytes.Equal(actualMessageBytes, expectedMessageBytes) {
		return fmt.Errorf("proof result message does not match payload and proof")
	}
	return nil
}

const proofArtifactCleanupTimeout = 10 * time.Second
const proofArtifactConfirmationTimeout = 10 * time.Second

func proofArtifactCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), proofArtifactCleanupTimeout)
}

func proofArtifactConfirmationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := proofArtifactConfirmationTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

type MemoryProofResultStore struct {
	mu        sync.Mutex
	results   map[string]ProofResult
	staged    map[string]ProofResult
	discarded map[string]struct{}
}

func NewMemoryProofResultStore() *MemoryProofResultStore {
	return &MemoryProofResultStore{
		results:   make(map[string]ProofResult),
		staged:    make(map[string]ProofResult),
		discarded: make(map[string]struct{}),
	}
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
	if _, discarded := s.discarded[result.Item.OperationID]; discarded {
		return fmt.Errorf("proof result %s was discarded", result.Item.OperationID)
	}
	s.results[result.Item.OperationID] = cloneProofResult(result)
	return nil
}

func (s *MemoryProofResultStore) StageProofResult(ctx context.Context, result ProofResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if result.Item.OperationID == "" {
		return fmt.Errorf("%w: proof result has no operation_id", ErrInvalidPayrollInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.staged == nil {
		s.staged = make(map[string]ProofResult)
	}
	if _, discarded := s.discarded[result.Item.OperationID]; discarded {
		return fmt.Errorf("proof result %s was discarded", result.Item.OperationID)
	}
	if _, published := s.results[result.Item.OperationID]; published {
		return fmt.Errorf("proof result %s is already published", result.Item.OperationID)
	}
	if _, staged := s.staged[result.Item.OperationID]; staged {
		return fmt.Errorf("proof result %s is already staged", result.Item.OperationID)
	}
	s.staged[result.Item.OperationID] = cloneProofResult(result)
	return nil
}

func (s *MemoryProofResultStore) PublishStagedProofResult(ctx context.Context, operationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, discarded := s.discarded[operationID]; discarded {
		return fmt.Errorf("proof result %s was discarded", operationID)
	}
	result, ok := s.staged[operationID]
	if !ok {
		if _, published := s.results[operationID]; published {
			return nil
		}
		return fmt.Errorf("staged proof result %s not found", operationID)
	}
	if s.results == nil {
		s.results = make(map[string]ProofResult)
	}
	s.results[operationID] = cloneProofResult(result)
	delete(s.staged, operationID)
	return nil
}

func (s *MemoryProofResultStore) DiscardStagedProofResult(ctx context.Context, operationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.staged, operationID)
	return nil
}

func (s *MemoryProofResultStore) DiscardPublishedProofResult(ctx context.Context, operationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discarded == nil {
		s.discarded = make(map[string]struct{})
	}
	s.discarded[operationID] = struct{}{}
	delete(s.staged, operationID)
	delete(s.results, operationID)
	return nil
}

func (s *MemoryProofResultStore) GetStagedProofResult(ctx context.Context, operationID string) (*ProofResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, discarded := s.discarded[operationID]; discarded {
		return nil, fmt.Errorf("staged proof result %s was discarded", operationID)
	}
	result, ok := s.staged[operationID]
	if !ok {
		return nil, fmt.Errorf("staged proof result %s not found", operationID)
	}
	cloned := cloneProofResult(result)
	return &cloned, nil
}

func (s *MemoryProofResultStore) GetProofResult(ctx context.Context, operationID string) (*ProofResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, discarded := s.discarded[operationID]; discarded {
		return nil, fmt.Errorf("proof result %s was discarded", operationID)
	}
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
	outbox, ok := w.ProofResultSink.(ProofResultOutbox)
	if !ok {
		return nil, fmt.Errorf("a staged proof result outbox is required")
	}
	assembler := w.Assembler
	if assembler == nil {
		assembler = DefaultTransferMessageAssembler{}
	}
	leaseOwner := strings.TrimSpace(w.LeaseOwner)
	if leaseOwner == "" {
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
	provingReservations := make([]string, 0, len(item.InputNotes))
	provingRefs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	proofStaged := false
	proofReadyRecorded := false
	defer func() {
		if runErr == nil {
			return
		}
		if proofStaged && !proofReadyRecorded {
			discardCtx, cancelDiscard := proofArtifactCleanupContext(ctx)
			discardErr := outbox.DiscardStagedProofResult(discardCtx, item.OperationID)
			cancelDiscard()
			if discardErr != nil {
				refs, refsErr := proofReadyReservationRefs(item, leases, leaseOwner)
				if refsErr != nil {
					runErr = errors.Join(runErr, discardErr, refsErr)
					return
				}
				manualReviewCtx, cancelManualReview := proofArtifactCleanupContext(ctx)
				_, _, manualReviewErr := w.Reservation.MarkProofArtifactCleanupFailedBatch(
					manualReviewCtx,
					refs,
					[]string{item.OperationID},
					discardErr.Error(),
				)
				cancelManualReview()
				runErr = errors.Join(runErr, discardErr, manualReviewErr)
				return
			}
		}
		if proofReadyRecorded {
			return
		}
		rollbackCtx, cancelRollback := proofArtifactCleanupContext(ctx)
		cleanupErr := w.rollbackProofReservations(rollbackCtx, item.OperationID, provingRefs)
		cancelRollback()
		if cleanupErr != nil {
			runErr = errors.Join(runErr, cleanupErr)
		}
	}()
	reservationIDs := make([]string, 0, len(item.InputNotes))
	for _, note := range item.InputNotes {
		if note.ReservationID == "" {
			return nil, fmt.Errorf("payroll item %s input note %s has no reservation", item.ItemID, note.NoteID)
		}
		reservationIDs = append(reservationIDs, note.ReservationID)
	}
	claimed, _, err := w.Reservation.BeginProvingOperation(ctx, item.OperationID, reservationIDs, leaseOwner, leaseTTL)
	if err != nil {
		return nil, err
	}
	for _, ref := range claimed {
		leases[ref.ReservationID] = ref.LeaseToken
		provingReservations = append(provingReservations, ref.ReservationID)
		provingRefs = append(provingRefs, ref)
	}

	proofCtx, stopHeartbeat := w.startProvingHeartbeat(ctx, leases, provingReservations, leaseTTL, leaseOwner)
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
	if err := ensurePreparedPayloadNullifiersUnspent(proofCtx, resolveProofNullifierChecker(w.NullifierChecker, w.PayloadBuilder), *payload); err != nil {
		return nil, err
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
		Item:                   clonePlanItem(item),
		Payload:                *payload,
		Proof:                  *proof,
		Message:                msg,
		ReservationLeases:      cloneStringMap(leases),
		ReservationLeaseOwners: reservationLeaseOwners(leases, leaseOwner),
	}
	if err := validateProofResultArtifact(result, assembler); err != nil {
		return nil, fmt.Errorf("validate prepared proof result: %w", err)
	}
	if err := outbox.StageProofResult(proofCtx, result); err != nil {
		confirmationCtx, cancelConfirmation := proofArtifactConfirmationContext(ctx)
		staged, readErr := outbox.GetStagedProofResult(confirmationCtx, item.OperationID)
		cancelConfirmation()
		if readErr == nil && staged != nil && proofResultArtifactsMatch(*staged, result) {
			proofStaged = true
		}
		if readErr != nil {
			return nil, errors.Join(err, fmt.Errorf("confirm staged proof result: %w", readErr))
		}
		return nil, err
	}
	proofStaged = true

	refs, err := proofReadyReservationRefs(item, leases, leaseOwner)
	if err != nil {
		return nil, err
	}
	update := privacyreservation.ProofReadyOperationUpdate{
		OperationID:                      item.OperationID,
		PayloadHash:                      payload.PayloadHash,
		ExpectedDisclosureDigest:         preferredDisclosureDigest(*payload),
		ExpectedUserDisclosureDigest:     payload.UserDisclosureDigestHex,
		ExpectedAuditDisclosureDigest:    payload.AuditDisclosureDigestHex,
		ExpectedSelfViewDisclosureDigest: payload.SelfViewDisclosureDigestHex,
	}
	if len(payload.Outputs) > 0 {
		update.ExpectedOutputCommitment = payload.Outputs[0].CommitmentHex
	}
	if _, _, err := w.Reservation.MarkProofReadyBatch(ctx, refs, update); err != nil {
		confirmationCtx, cancelConfirmation := proofArtifactConfirmationContext(ctx)
		matches, readErr := proofReadyBatchMatches(confirmationCtx, w.Reservation.Store, item, payload.PayloadHash)
		cancelConfirmation()
		if readErr != nil {
			return nil, errors.Join(err, fmt.Errorf("confirm proof-ready reservation batch: %w", readErr))
		}
		if !matches {
			return nil, err
		}
	}
	proofReadyRecorded = true
	if err := outbox.PublishStagedProofResult(proofCtx, item.OperationID); err != nil {
		return nil, fmt.Errorf("publish staged proof result for operation %s: %w", item.OperationID, err)
	}
	// A heartbeat racing after the durable ProofReady CAS observes the terminal
	// transition, not a failed proof. Do not turn that successful result into an
	// error that leaves callers without their prepared artifact.
	_ = stopHeartbeat()
	stopHeartbeat = nil

	return &result, nil
}

func proofResultArtifactsMatch(left ProofResult, right ProofResult) bool {
	if left.Item.OperationID != right.Item.OperationID || left.Payload.PayloadHash != right.Payload.PayloadHash || left.Proof.PayloadHash != right.Proof.PayloadHash || left.Proof.ProofHex != right.Proof.ProofHex {
		return false
	}
	if left.Message == nil || right.Message == nil {
		return left.Message == nil && right.Message == nil
	}
	leftBytes, leftErr := left.Message.Marshal()
	rightBytes, rightErr := right.Message.Marshal()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func proofReadyBatchMatches(ctx context.Context, store privacyreservation.Store, item PayrollPlanItem, payloadHash string) (bool, error) {
	if store == nil || item.OperationID == "" || payloadHash == "" || len(item.InputNotes) == 0 {
		return false, nil
	}
	operation, err := store.GetOperation(ctx, item.OperationID)
	if err != nil {
		return false, err
	}
	if operation.Status != privacyreservation.OperationStatusProofReady || operation.PayloadHash != payloadHash {
		return false, nil
	}
	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		if err != nil {
			return false, err
		}
		if reservation.Status != privacyreservation.StatusProofReady || reservation.OperationID != item.OperationID || reservation.PayloadHash != payloadHash {
			return false, nil
		}
	}
	return true, nil
}

// RecoverStagedProofResult publishes a proof left in the outbox by a crash
// after the ProofReady CAS but before publish. It never publishes a staged
// artifact unless every linked reservation is still ProofReady for the same
// operation.
func (w ProofWorker) RecoverStagedProofResult(ctx context.Context, operationID string) (*ProofResult, error) {
	outbox, ok := w.ProofResultSink.(ProofResultOutbox)
	if !ok {
		return nil, fmt.Errorf("a staged proof result outbox is required")
	}
	result, err := outbox.GetStagedProofResult(ctx, operationID)
	alreadyPublished := false
	if err != nil {
		result, err = outbox.GetProofResult(ctx, operationID)
		if err != nil {
			return nil, err
		}
		alreadyPublished = true
	}
	if result.Item.OperationID != operationID {
		return nil, fmt.Errorf("staged proof result operation mismatch")
	}
	payloadHash := strings.TrimSpace(result.Payload.PayloadHash)
	operation, err := w.Reservation.Store.GetOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if operation.PayloadHash != payloadHash {
		return nil, fmt.Errorf("staged proof result payload hash does not match operation %s", operationID)
	}
	expectedReservationIDs := make(map[string]struct{}, len(result.Item.InputNotes))
	for _, note := range result.Item.InputNotes {
		reservationID := strings.TrimSpace(note.ReservationID)
		if reservationID == "" {
			return nil, fmt.Errorf("staged proof result %s has an empty reservation id", operationID)
		}
		if _, duplicate := expectedReservationIDs[reservationID]; duplicate {
			return nil, fmt.Errorf("staged proof result %s has duplicate reservation %s", operationID, reservationID)
		}
		expectedReservationIDs[reservationID] = struct{}{}
		reservation, err := w.Reservation.Store.GetReservation(ctx, note.ReservationID)
		if err != nil {
			return nil, err
		}
		if reservation.Status != privacyreservation.StatusProofReady || reservation.OperationID != operationID || reservation.PayloadHash != payloadHash {
			return nil, fmt.Errorf("staged proof result %s is not recoverable from reservation %s status %s", operationID, note.ReservationID, reservation.Status)
		}
	}
	linkedReservations, err := w.Reservation.Store.ListReservations(ctx, privacyreservation.ReservationFilter{
		OperationID: operationID,
	})
	if err != nil {
		return nil, err
	}
	linkedCount := 0
	for _, reservation := range linkedReservations {
		if reservation.OperationID != operationID {
			continue
		}
		linkedCount++
		if _, expected := expectedReservationIDs[reservation.ReservationID]; !expected {
			return nil, fmt.Errorf("staged proof result %s is missing linked reservation %s", operationID, reservation.ReservationID)
		}
	}
	if linkedCount != len(expectedReservationIDs) {
		return nil, fmt.Errorf("staged proof result %s reservation set does not match the linked operation", operationID)
	}
	if err := validateProofResultArtifact(*result, w.Assembler); err != nil {
		return nil, fmt.Errorf("staged proof result validation failed: %w", err)
	}
	if alreadyPublished {
		return result, nil
	}
	if err := outbox.PublishStagedProofResult(ctx, operationID); err != nil {
		return nil, err
	}
	return outbox.GetProofResult(ctx, operationID)
}

// DiscardPublishedProofResultAndReplan makes artifact deletion durable before
// the exact ProofReady operation set is made available for planning again.
func (w ProofWorker) DiscardPublishedProofResultAndReplan(ctx context.Context, operationID string, refs []privacyreservation.SubmittedReservationRef) ([]privacyreservation.NoteReservation, error) {
	outbox, ok := w.ProofResultSink.(ProofResultOutbox)
	if !ok {
		return nil, fmt.Errorf("a staged proof result outbox is required")
	}
	cleanupCtx, cancel := proofArtifactCleanupContext(ctx)
	defer cancel()
	if _, err := w.Reservation.BeginProofDiscardOperation(cleanupCtx, operationID, refs); err != nil {
		return nil, fmt.Errorf("begin proof discard for operation %s: %w", operationID, err)
	}
	if err := outbox.DiscardPublishedProofResult(cleanupCtx, operationID); err != nil {
		return nil, fmt.Errorf("discard published proof result for operation %s: %w", operationID, err)
	}
	return w.Reservation.ReplanProofReadyOperationAfterDiscard(cleanupCtx, operationID, refs, privacyreservation.ProofDiscardEvidence{
		NoBroadcastAttempt: true,
		ProofDiscarded:     true,
	})
}

func (w ProofWorker) startProvingHeartbeat(ctx context.Context, leases map[string]string, reservationIDs []string, ttl time.Duration, leaseOwner string) (context.Context, func() error) {
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
					if _, err := w.Reservation.HeartbeatLease(heartbeatCtx, reservationID, leaseOwner, token, ttl); err != nil {
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

func ensurePreparedPayloadNullifiersUnspent(ctx context.Context, checker BroadcastNullifierChecker, payload privacytransfer.PreparedTransferPayload) error {
	if checker == nil {
		return fmt.Errorf("a nullifier checker is required before proof generation")
	}
	nullifiers := make([]string, 0, len(payload.Inputs))
	seen := make(map[string]struct{}, len(payload.Inputs))
	for _, input := range payload.Inputs {
		nullifier := strings.ToLower(strings.TrimSpace(input.NullifierHex))
		if nullifier == "" {
			return fmt.Errorf("prepared transfer payload has an empty nullifier")
		}
		if _, exists := seen[nullifier]; exists {
			continue
		}
		seen[nullifier] = struct{}{}
		nullifiers = append(nullifiers, nullifier)
	}
	if len(nullifiers) == 0 {
		return fmt.Errorf("prepared transfer payload has no nullifiers")
	}
	usedByNullifier, err := checker.CheckNullifiersUsed(ctx, nullifiers)
	if err != nil {
		return fmt.Errorf("verify nullifiers before proof generation: %w", err)
	}
	for _, nullifier := range nullifiers {
		used, ok := usedByNullifier[nullifier]
		if !ok {
			return fmt.Errorf("verify nullifiers before proof generation: missing status for nullifier")
		}
		if used {
			return &SpentNullifierError{}
		}
	}
	return nil
}

func resolveProofNullifierChecker(explicit BroadcastNullifierChecker, builder PreparedPayloadBuilder) BroadcastNullifierChecker {
	if explicit != nil {
		return explicit
	}
	checker, _ := builder.(BroadcastNullifierChecker)
	return checker
}

func (w ProofWorker) rollbackProofReservations(ctx context.Context, operationID string, refs []privacyreservation.SubmittedReservationRef) error {
	if len(refs) == 0 {
		return nil
	}
	_, _, err := w.Reservation.RollbackProvingOperation(ctx, operationID, refs)
	return err
}

func cloneProofResult(result ProofResult) ProofResult {
	result.Item = clonePlanItem(result.Item)
	result.Payload = clonePreparedTransferPayload(result.Payload)
	result.Message = cloneTransferMessage(result.Message)
	result.ReservationLeases = cloneStringMap(result.ReservationLeases)
	result.ReservationLeaseOwners = cloneStringMap(result.ReservationLeaseOwners)
	return result
}

func reservationLeaseOwners(leases map[string]string, owner string) map[string]string {
	owners := make(map[string]string, len(leases))
	for reservationID := range leases {
		owners[reservationID] = owner
	}
	return owners
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

func proofReadyReservationRefs(item PayrollPlanItem, leases map[string]string, leaseOwner string) ([]privacyreservation.SubmittedReservationRef, error) {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	for _, note := range item.InputNotes {
		token := leases[note.ReservationID]
		if note.ReservationID == "" || token == "" {
			return nil, fmt.Errorf("payroll item %s has no lease token for reservation %s", item.ItemID, note.ReservationID)
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{
			ReservationID: note.ReservationID,
			LeaseOwner:    leaseOwner,
			LeaseToken:    token,
		})
	}
	return refs, nil
}
