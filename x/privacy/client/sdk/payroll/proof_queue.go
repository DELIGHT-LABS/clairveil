package payroll

import (
	"context"
	"fmt"
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

type DefaultTransferMessageAssembler struct{}

func (DefaultTransferMessageAssembler) BuildTransferMessage(payload privacytransfer.PreparedTransferPayload, proof privacytransfer.PreparedTransferProof) (*privacytypes.MsgTransfer, error) {
	return payload.ToMsg(proof)
}

type ProofWorker struct {
	Reservation    privacyreservation.Service
	PayloadBuilder PreparedPayloadBuilder
	ProofRunner    PreparedProofRunner
	Assembler      TransferMessageAssembler
	LeaseOwner     string
	LeaseTTL       time.Duration
}

type ProofResult struct {
	Item              PayrollPlanItem
	Payload           privacytransfer.PreparedTransferPayload
	Proof             privacytransfer.PreparedTransferProof
	Message           *privacytypes.MsgTransfer
	ReservationLeases map[string]string
}

func (w ProofWorker) Process(ctx context.Context, item PayrollPlanItem) (*ProofResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if w.PayloadBuilder == nil {
		return nil, fmt.Errorf("a prepared transfer payload builder is required")
	}
	if w.ProofRunner == nil {
		return nil, fmt.Errorf("a prepared transfer proof runner is required")
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
	for _, note := range item.InputNotes {
		if note.ReservationID == "" {
			return nil, fmt.Errorf("payroll item %s input note %s has no reservation", item.ItemID, note.NoteID)
		}
		lease, err := w.Reservation.AcquireLease(ctx, note.ReservationID, w.LeaseOwner, leaseTTL)
		if err != nil {
			return nil, err
		}
		leases[note.ReservationID] = lease.Token
		if _, err := w.Reservation.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving); err != nil {
			return nil, err
		}
	}

	payload, err := w.PayloadBuilder.BuildPreparedTransferPayload(ctx, item)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("prepared transfer payload builder returned nil")
	}
	proof, err := w.ProofRunner.BuildPreparedTransferProof(ctx, *payload)
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

	if err := w.updateOperationProofReady(ctx, item.OperationID, *payload); err != nil {
		return nil, err
	}
	for _, note := range item.InputNotes {
		if _, err := w.Reservation.TransitionWithLease(ctx, note.ReservationID, leases[note.ReservationID], privacyreservation.StatusProving, privacyreservation.StatusProofReady); err != nil {
			return nil, err
		}
	}

	return &ProofResult{
		Item:              clonePlanItem(item),
		Payload:           *payload,
		Proof:             *proof,
		Message:           msg,
		ReservationLeases: leases,
	}, nil
}

func (w ProofWorker) updateOperationProofReady(ctx context.Context, operationID string, payload privacytransfer.PreparedTransferPayload) error {
	if operationID == "" {
		return nil
	}
	operation, err := w.Reservation.Store.GetOperation(ctx, operationID)
	if err != nil {
		return err
	}
	if len(payload.Outputs) > 0 && operation.ExpectedOutputCommitment == "" {
		operation.ExpectedOutputCommitment = payload.Outputs[0].CommitmentHex
	}
	if operation.ExpectedDisclosureDigest == "" {
		operation.ExpectedDisclosureDigest = preferredDisclosureDigest(payload)
	}
	operation.Status = privacyreservation.OperationStatusProofReady
	operation.UpdatedAt = reservationNow(w.Reservation)
	_, err = w.Reservation.Store.UpdateOperation(ctx, *operation)
	return err
}

func preferredDisclosureDigest(payload privacytransfer.PreparedTransferPayload) string {
	if payload.UserDisclosureDigestHex != "" {
		return payload.UserDisclosureDigestHex
	}
	if payload.SelfViewDisclosureDigestHex != "" {
		return payload.SelfViewDisclosureDigestHex
	}
	return payload.AuditDisclosureDigestHex
}

func reservationNow(svc privacyreservation.Service) time.Time {
	if svc.Now != nil {
		return svc.Now().UTC()
	}
	return time.Now().UTC()
}
