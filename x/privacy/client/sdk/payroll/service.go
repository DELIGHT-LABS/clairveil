package payroll

import (
	"context"
	"fmt"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type Service struct {
	Reservation privacyreservation.Service
	Allocator   NoteAllocator
	Now         func() time.Time
}

func (s Service) CreatePlan(_ context.Context, input PayrollInput, treasuryNotes []TreasuryNote) (*PayrollPlan, error) {
	now := s.now()
	if input.CreatedAt.IsZero() {
		input.CreatedAt = now
	}
	items, err := s.Allocator.Allocate(input, treasuryNotes)
	if err != nil {
		return nil, err
	}
	return &PayrollPlan{
		CompanyID: input.CompanyID,
		PayrollID: input.PayrollID,
		BatchID:   input.BatchID,
		Denom:     input.Denom,
		Attempt:   input.Attempt,
		Status:    PlanStatusDraft,
		Items:     items,
		CreatedAt: input.CreatedAt.UTC(),
		UpdatedAt: now,
	}, nil
}

func (s Service) ConfirmPlan(ctx context.Context, plan PayrollPlan) (*PayrollPlan, error) {
	if s.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if plan.Status != PlanStatusDraft {
		return nil, fmt.Errorf("%w: plan must be Draft", ErrInvalidPayrollInput)
	}

	now := s.now()
	confirmed := clonePlan(plan)
	confirmed.Status = PlanStatusConfirmed
	confirmed.UpdatedAt = now

	reservationInputs := make([]privacyreservation.ReserveInput, 0)
	for itemIndex := range confirmed.Items {
		item := &confirmed.Items[itemIndex]
		for noteIndex, note := range item.InputNotes {
			reservationID := reservationID(item.OperationID, note.NoteID)
			reservationInput := privacyreservation.ReserveInput{
				Reservation: privacyreservation.NoteReservation{
					ReservationID:        reservationID,
					CompanyID:            item.CompanyID,
					PayrollID:            item.PayrollID,
					BatchID:              item.BatchID,
					ChunkID:              item.ChunkID,
					ItemID:               item.ItemID,
					NoteID:               note.NoteID,
					OwnerKeyID:           note.OwnerKeyID,
					NullifierLookupKey:   note.NullifierLookupKey,
					NullifierLookupKeyID: note.NullifierLookupKeyID,
					Status:               privacyreservation.StatusReserved,
					OperationID:          item.OperationID,
					CreatedAt:            now,
				},
			}
			if noteIndex == 0 {
				reservationInput.Operation = &privacyreservation.PayrollOperation{
					OperationID:              item.OperationID,
					CompanyID:                item.CompanyID,
					PayrollID:                item.PayrollID,
					BatchID:                  item.BatchID,
					ChunkID:                  item.ChunkID,
					ItemID:                   item.ItemID,
					ReservationID:            reservationID,
					ExpectedOutputCommitment: item.ExpectedOutputCommitment,
					ExpectedDisclosureDigest: item.ExpectedDisclosureDigest,
					ExpectedRecipientHash:    item.ExpectedRecipientHash,
					ExpectedAmountHash:       item.ExpectedAmountHash,
					ExpectedDenom:            item.Denom,
					BatchItemIndex:           itemIndex,
					Status:                   privacyreservation.OperationStatusPlanned,
					CreatedAt:                now,
				}
			}
			reservationInputs = append(reservationInputs, reservationInput)
			item.InputNotes[noteIndex].ReservationID = reservationID
		}
		item.Status = ItemStatusReserved
	}

	if _, err := s.Reservation.ReserveBatch(ctx, reservationInputs); err != nil {
		return nil, err
	}

	return &confirmed, nil
}

func (s Service) ReplanItems(input PayrollInput, treasuryNotes []TreasuryNote, failedItems map[string]struct{}) (*PayrollPlan, error) {
	filtered := input
	filtered.Items = make([]PayrollItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		if _, ok := failedItems[item.ItemID]; ok {
			filtered.Items = append(filtered.Items, item)
		}
	}
	if len(filtered.Items) == 0 {
		return &PayrollPlan{
			CompanyID: input.CompanyID,
			PayrollID: input.PayrollID,
			BatchID:   input.BatchID,
			Denom:     input.Denom,
			Attempt:   input.Attempt + 1,
			Status:    PlanStatusDraft,
			Items:     []PayrollPlanItem{},
			CreatedAt: s.now(),
			UpdatedAt: s.now(),
		}, nil
	}
	filtered.Attempt = input.Attempt + 1
	return s.CreatePlan(context.Background(), filtered, treasuryNotes)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func reservationID(operationID string, noteID string) string {
	return operationID + ":note:" + idComponent(noteID)
}
