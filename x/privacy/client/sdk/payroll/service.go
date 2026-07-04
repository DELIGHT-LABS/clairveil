package payroll

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type Service struct {
	Reservation privacyreservation.Service
	Allocator   NoteAllocator
	Now         func() time.Time
}

func (s Service) CreatePlan(_ context.Context, input PayrollInput, treasuryNotes []TreasuryNote) (*PayrollPlan, error) {
	now := s.now()
	input = normalizePayrollInput(input)
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
	plan = normalizePayrollPlan(plan)
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
		if err := validatePlanItemForConfirmation(*item); err != nil {
			return nil, err
		}
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
					BatchItemIndexKnown:      true,
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

func validatePlanItemForConfirmation(item PayrollPlanItem) error {
	if strings.TrimSpace(item.OperationID) == "" {
		return fmt.Errorf("%w: item %s has no operation_id", ErrInvalidPayrollInput, item.ItemID)
	}
	if _, err := privacytypes.DecodeShieldedAddressBundle(item.RecipientAddress); err != nil {
		return fmt.Errorf("%w: item %s recipient must be a valid shielded address with prefix %s: %v", ErrInvalidPayrollInput, item.ItemID, privacytypes.ShieldedBech32Prefix, err)
	}
	if item.Amount == nil || item.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: item %s amount must be positive", ErrInvalidPayrollInput, item.ItemID)
	}
	if strings.TrimSpace(item.Denom) == "" {
		return fmt.Errorf("%w: item %s denom is required", ErrInvalidPayrollInput, item.ItemID)
	}
	if len(item.InputNotes) != 2 {
		return fmt.Errorf("%w: item %s must have exactly 2 input notes for the current transfer circuit", ErrInvalidPayrollInput, item.ItemID)
	}
	total := big.NewInt(0)
	for _, note := range item.InputNotes {
		if strings.TrimSpace(note.NoteID) == "" {
			return fmt.Errorf("%w: item %s has an input note without note_id", ErrInvalidPayrollInput, item.ItemID)
		}
		if strings.TrimSpace(note.OwnerKeyID) == "" {
			return fmt.Errorf("%w: note %s has no owner_key_id", ErrInvalidPayrollInput, note.NoteID)
		}
		if strings.TrimSpace(note.NullifierLookupKey) == "" {
			return fmt.Errorf("%w: note %s has no nullifier_lookup_key", ErrInvalidPayrollInput, note.NoteID)
		}
		if strings.TrimSpace(note.ReservationID) != "" {
			return fmt.Errorf("%w: note %s is already reserved", ErrInvalidPayrollInput, note.NoteID)
		}
		if note.IsSpent {
			return fmt.Errorf("%w: note %s is already spent", ErrInvalidPayrollInput, note.NoteID)
		}
		if strings.TrimSpace(note.Denom) != item.Denom {
			return fmt.Errorf("%w: note %s denom %s does not match item denom %s", ErrInvalidPayrollInput, note.NoteID, note.Denom, item.Denom)
		}
		if note.Amount == nil || note.Amount.Sign() < 0 {
			return fmt.Errorf("%w: note %s amount must be non-negative", ErrInvalidPayrollInput, note.NoteID)
		}
		total.Add(total, note.Amount)
	}
	if !finalPayrollOutputsWithinBound(total, item.Amount) {
		return fmt.Errorf("%w: item %s input notes do not fund the transfer within shielded output bounds", ErrInvalidPayrollInput, item.ItemID)
	}
	return nil
}

func (s Service) ReplanItems(input PayrollInput, treasuryNotes []TreasuryNote, failedItems map[string]struct{}) (*PayrollPlan, error) {
	input = normalizePayrollInput(input)
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

func normalizePayrollPlan(plan PayrollPlan) PayrollPlan {
	plan.CompanyID = strings.TrimSpace(plan.CompanyID)
	plan.PayrollID = strings.TrimSpace(plan.PayrollID)
	plan.BatchID = strings.TrimSpace(plan.BatchID)
	plan.Denom = strings.TrimSpace(plan.Denom)
	plan.Items = append([]PayrollPlanItem(nil), plan.Items...)
	for i := range plan.Items {
		plan.Items[i] = normalizePayrollPlanItem(plan.Items[i])
		if plan.Items[i].Denom == "" {
			plan.Items[i].Denom = plan.Denom
		}
		if plan.Items[i].RecipientAddress != "" {
			plan.Items[i].ExpectedRecipientHash = HashRecipient(plan.Items[i].RecipientAddress)
		}
		if plan.Items[i].Denom != "" && plan.Items[i].Amount != nil {
			plan.Items[i].ExpectedAmountHash = HashAmount(plan.Items[i].Denom, plan.Items[i].Amount)
		}
	}
	return plan
}

func normalizePayrollPlanItem(item PayrollPlanItem) PayrollPlanItem {
	item.CompanyID = strings.TrimSpace(item.CompanyID)
	item.PayrollID = strings.TrimSpace(item.PayrollID)
	item.BatchID = strings.TrimSpace(item.BatchID)
	item.ChunkID = strings.TrimSpace(item.ChunkID)
	item.ItemID = strings.TrimSpace(item.ItemID)
	item.EmployeeID = strings.TrimSpace(item.EmployeeID)
	item.OperationID = strings.TrimSpace(item.OperationID)
	item.RecipientAddress = strings.TrimSpace(item.RecipientAddress)
	item.ExpectedRecipientHash = strings.TrimSpace(item.ExpectedRecipientHash)
	item.ExpectedAmountHash = strings.TrimSpace(item.ExpectedAmountHash)
	item.Denom = strings.TrimSpace(item.Denom)
	item.ExpectedOutputCommitment = strings.TrimSpace(item.ExpectedOutputCommitment)
	item.ExpectedDisclosureDigest = strings.TrimSpace(item.ExpectedDisclosureDigest)
	item.InputNotes = append([]TreasuryNote(nil), item.InputNotes...)
	for i := range item.InputNotes {
		item.InputNotes[i] = normalizeTreasuryNote(item.InputNotes[i])
	}
	return item
}

func normalizeTreasuryNote(note TreasuryNote) TreasuryNote {
	note.NoteID = strings.TrimSpace(note.NoteID)
	note.OwnerKeyID = strings.TrimSpace(note.OwnerKeyID)
	note.NullifierLookupKey = strings.TrimSpace(note.NullifierLookupKey)
	note.NullifierLookupKeyID = strings.TrimSpace(note.NullifierLookupKeyID)
	note.Denom = strings.TrimSpace(note.Denom)
	return note
}
