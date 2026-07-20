package payroll

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type PayrollEvidenceSealer interface {
	SealPayrollEvidence(ctx context.Context, plaintext []byte) ([]byte, error)
}

// PayrollBatchArtifactProtector combines private artifact sealing with the
// keyed nullifier index derivation. BuildBatchOperationGraph requires both so
// a payload cannot consume Note A while reserving Note B's lookup key.
type PayrollBatchArtifactProtector interface {
	PayrollEvidenceSealer
	PayrollNullifierLookupKey(ctx context.Context, keyID string, nullifier []byte) (string, error)
}

// BuildBatchOperationGraph materializes expected output evidence only after
// the final prepared payload has fixed every commitment and disclosure digest.
// The caller persists the returned graph and all input reservations in one
// BatchOperationStore transaction.
func BuildBatchOperationGraph(ctx context.Context, plan BatchPayrollOperationPlan, payload *privacybatchtransfer.PreparedBatchTransferPayload, protector PayrollBatchArtifactProtector, now time.Time) ([]privacyreservation.NoteReservation, privacyreservation.BatchOperationGraph, error) {
	return buildBatchOperationGraph(ctx, plan, payload, protector, now, time.Now)
}

func buildBatchOperationGraph(ctx context.Context, plan BatchPayrollOperationPlan, payload *privacybatchtransfer.PreparedBatchTransferPayload, protector PayrollBatchArtifactProtector, now time.Time, currentTime func() time.Time) ([]privacyreservation.NoteReservation, privacyreservation.BatchOperationGraph, error) {
	if payload == nil || protector == nil {
		return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("prepared batch payload and payroll evidence sealer are required")
	}
	if now.IsZero() {
		now = currentTime().UTC()
	}
	if err := privacybatchtransfer.ValidatePreparedBatchTransferPayloadMetadataAt(payload, now); err != nil {
		return nil, privacyreservation.BatchOperationGraph{}, err
	}
	baseOutputCount := len(plan.Items)
	if plan.HasChange {
		baseOutputCount++
	}
	if plan.OutputCount != baseOutputCount || len(payload.Inputs) != len(plan.InputNotes) || len(payload.Outputs) < baseOutputCount || len(payload.Outputs) > int(privacytypes.BatchJoinSplitV1MaxOutputs) || len(payload.MessageOutputs) != len(payload.Outputs) {
		return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("prepared payload shape does not match the payroll batch plan")
	}
	if len(plan.Items) == 0 || len(plan.Items) > 32 || len(plan.InputNotes) == 0 || len(plan.InputNotes) > 16 {
		return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("invalid payroll batch plan capacity")
	}
	if err := validateBatchPayrollPlanBinding(plan, payload); err != nil {
		return nil, privacyreservation.BatchOperationGraph{}, err
	}
	assetIDHex := hex.EncodeToString(payload.AssetID.FillBytes(make([]byte, 32)))
	preparedPayloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("marshal prepared batch payload: %w", err)
	}
	encryptedPreparedPayload, err := protector.SealPayrollEvidence(ctx, preparedPayloadBytes)
	if err != nil {
		return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("seal prepared batch payload: %w", err)
	}
	reservations := make([]privacyreservation.NoteReservation, len(plan.InputNotes))
	inputs := make([]privacyreservation.OperationInputReservation, len(plan.InputNotes))
	for i, note := range plan.InputNotes {
		if note.NoteID == "" || note.OwnerKeyID == "" || note.NullifierLookupKey == "" || note.NullifierLookupKeyID == "" || !note.IsVerifiedUnspent() || note.ReservationID != "" {
			return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("invalid treasury input note %d for batch reservation", i)
		}
		expectedLookupKey, err := protector.PayrollNullifierLookupKey(ctx, note.NullifierLookupKeyID, payload.Inputs[i].Nullifier)
		if err != nil {
			return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("derive input nullifier lookup key %d: %w", i, err)
		}
		if !strings.EqualFold(strings.TrimSpace(expectedLookupKey), strings.TrimSpace(note.NullifierLookupKey)) {
			return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("prepared input %d nullifier does not match the reserved treasury note lookup key", i)
		}
		encryptedNullifier, err := protector.SealPayrollEvidence(ctx, payload.Inputs[i].Nullifier)
		if err != nil {
			return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("seal input nullifier %d: %w", i, err)
		}
		encryptedAmount, err := protector.SealPayrollEvidence(ctx, []byte(note.Amount.String()))
		if err != nil {
			return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("seal input amount %d: %w", i, err)
		}
		reservationID := ReservationIDForInputNote(plan.OperationID, note.NoteID)
		reservations[i] = privacyreservation.NoteReservation{
			ReservationID: reservationID, BatchID: plan.Items[0].BatchID, PayrollID: plan.Items[0].PayrollID, CompanyID: plan.Items[0].CompanyID,
			NoteID: note.NoteID, OwnerKeyID: note.OwnerKeyID, NullifierLookupKey: note.NullifierLookupKey,
			NullifierLookupKeyID: note.NullifierLookupKeyID, EncryptedNullifier: encryptedNullifier,
			Status: privacyreservation.StatusReserved, OperationID: plan.OperationID, CreatedAt: now, UpdatedAt: now,
		}
		inputs[i] = privacyreservation.OperationInputReservation{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: plan.OperationID, ReservationID: reservationID, InputIndex: i, Commitment: hex.EncodeToString(payload.Inputs[i].Note.ComputeCommitment().FillBytes(make([]byte, 32))), EncryptedAmount: encryptedAmount, CreatedAt: now}
	}

	items := make([]privacyreservation.PayrollItemOutput, len(payload.Outputs))
	evidence := make([]privacyreservation.ExpectedOutputEvidence, len(payload.Outputs))
	for i, output := range payload.Outputs {
		wire := payload.MessageOutputs[i]
		role, item, err := payrollOutputRole(plan, i, output)
		if err != nil {
			return nil, privacyreservation.BatchOperationGraph{}, err
		}
		itemID, employeeID := "", ""
		var recipientHash, amountHash string
		var encryptedRecipient, encryptedAmount []byte
		if item != nil {
			itemID, employeeID = item.ItemID, item.EmployeeID
			recipientHash, amountHash = item.ExpectedRecipientHash, item.ExpectedAmountHash
			encryptedRecipient, err = protector.SealPayrollEvidence(ctx, []byte(item.RecipientAddress))
			if err != nil {
				return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("seal recipient for item %s: %w", item.ItemID, err)
			}
			encryptedAmount, err = protector.SealPayrollEvidence(ctx, []byte(item.Amount.String()))
			if err != nil {
				return nil, privacyreservation.BatchOperationGraph{}, fmt.Errorf("seal amount for item %s: %w", item.ItemID, err)
			}
		}
		items[i] = privacyreservation.PayrollItemOutput{
			SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: plan.OperationID,
			ItemID: itemID, EmployeeID: employeeID, OutputIndex: i, Role: role,
			EvidenceStatus: privacyreservation.BatchItemEvidencePending, CreatedAt: now, UpdatedAt: now,
		}
		evidence[i] = privacyreservation.ExpectedOutputEvidence{
			SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: plan.OperationID, OutputIndex: i,
			Commitment: hex.EncodeToString(wire.Commitment), UserPrivacyPolicy: wire.UserPrivacyPolicy, UserDisclosureMode: wire.UserDisclosureMode,
			UserDisclosureDigest: hex.EncodeToString(wire.UserDisclosureDigest),
			FullDisclosureDigest: hex.EncodeToString(wire.FullDisclosureDigest), RecipientHash: recipientHash,
			EncryptedRecipient: encryptedRecipient, EncryptedAmount: encryptedAmount, AmountHash: amountHash,
			Denom: plan.Items[0].Denom, AssetID: assetIDHex, Role: role,
			AuditKeyID: payload.AuditKeyID, AuditKeyEpoch: payload.AuditKeyEpoch, CreatedAt: now, UpdatedAt: now,
		}
	}

	graph := privacyreservation.BatchOperationGraph{
		Operation: privacyreservation.BatchOperation{
			SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: plan.OperationID,
			CompanyID: plan.Items[0].CompanyID, PayrollID: plan.Items[0].PayrollID, BatchID: plan.Items[0].BatchID,
			OwnerKeyID: plan.InputNotes[0].OwnerKeyID, AssetID: assetIDHex, Denom: plan.Items[0].Denom,
			InputCount: len(inputs), OutputCount: len(items), Status: privacyreservation.OperationStatusPlanned,
			PreparedPayloadCiphertext: encryptedPreparedPayload, PreparedPayloadHash: payload.PayloadHash,
			CreatedAt: now, UpdatedAt: now,
		},
		Inputs: inputs, Items: items, Evidence: evidence,
	}
	return reservations, graph, nil
}

func ConfirmBatchPayrollOperation(ctx context.Context, store privacyreservation.BatchOperationStore, reservations []privacyreservation.NoteReservation, graph privacyreservation.BatchOperationGraph) (*privacyreservation.BatchOperationGraph, error) {
	if store == nil {
		return nil, fmt.Errorf("batch operation store is required")
	}
	return store.CreateBatchOperation(ctx, reservations, graph)
}

func payrollOutputRole(plan BatchPayrollOperationPlan, index int, output privacybatchtransfer.PreparedBatchTransferOutput) (privacyreservation.BatchOutputRole, *PayrollPlanItem, error) {
	if index < len(plan.Items) {
		item := &plan.Items[index]
		if output.Kind != privacybatchtransfer.OutputPayment || output.Note.Amount.Cmp(item.Amount) != 0 {
			return "", nil, fmt.Errorf("prepared payment output %d does not match payroll item %s", index, item.ItemID)
		}
		bundle, err := privacytypes.DecodeShieldedAddressBundle(item.RecipientAddress)
		if err != nil {
			return "", nil, fmt.Errorf("decode payroll item %s recipient: %w", item.ItemID, err)
		}
		if !noteRecipientMatches(output.Note, bundle) {
			return "", nil, fmt.Errorf("prepared payment output %d recipient does not match payroll item %s", index, item.ItemID)
		}
		if output.PrivacyPolicy != item.DisclosurePolicy.UserPrivacyPolicy || output.DisclosureMode != item.DisclosurePolicy.UserDisclosureMode {
			return "", nil, fmt.Errorf("prepared payment output %d disclosure policy does not match payroll item %s", index, item.ItemID)
		}
		expectedTarget := strings.TrimSpace(item.DisclosurePolicy.UserDisclosureTargetPubKeyHex)
		if expectedTarget == "" {
			if len(output.DisclosureTargetPubKey) != 0 {
				return "", nil, fmt.Errorf("prepared payment output %d has an unexpected disclosure target", index)
			}
		} else {
			targetBytes, err := hex.DecodeString(expectedTarget)
			if err != nil || !bytes.Equal(targetBytes, output.DisclosureTargetPubKey) {
				return "", nil, fmt.Errorf("prepared payment output %d disclosure target does not match payroll item %s", index, item.ItemID)
			}
		}
		return privacyreservation.BatchOutputRolePayment, item, nil
	}
	if index == len(plan.Items) && plan.HasChange {
		if output.Kind != privacybatchtransfer.OutputChange || output.Note.Amount.Cmp(plan.Change) != 0 {
			return "", nil, fmt.Errorf("prepared change output does not match payroll plan")
		}
		return privacyreservation.BatchOutputRoleChange, nil, nil
	}
	if output.Kind != privacybatchtransfer.OutputPadding || output.Note.Amount.Cmp(new(big.Int)) != 0 {
		return "", nil, fmt.Errorf("prepared padding output %d is not canonical", index)
	}
	return privacyreservation.BatchOutputRolePadding, nil, nil
}

func validateBatchPayrollPlanBinding(plan BatchPayrollOperationPlan, payload *privacybatchtransfer.PreparedBatchTransferPayload) error {
	if strings.TrimSpace(plan.OperationID) == "" || plan.InputTotal == nil || plan.PaymentTotal == nil || plan.Change == nil {
		return fmt.Errorf("payroll batch plan totals and operation ID are required")
	}
	denom := plan.Items[0].Denom
	companyID, payrollID, batchID := plan.Items[0].CompanyID, plan.Items[0].PayrollID, plan.Items[0].BatchID
	if strings.TrimSpace(denom) == "" || strings.TrimSpace(companyID) == "" || strings.TrimSpace(payrollID) == "" || strings.TrimSpace(batchID) == "" {
		return fmt.Errorf("payroll batch identity and denom are required")
	}
	if payload.AssetID.Cmp(privacytypes.ComputeAssetIDV1(denom)) != 0 {
		return fmt.Errorf("prepared payload asset does not match payroll denom %q", denom)
	}
	inputTotal := new(big.Int)
	ownerKeyID := plan.InputNotes[0].OwnerKeyID
	for i, note := range plan.InputNotes {
		if strings.TrimSpace(ownerKeyID) == "" || note.OwnerKeyID != ownerKeyID || note.Denom != denom || note.Amount == nil || payload.Inputs[i].Note.Amount.Cmp(note.Amount) != 0 || payload.Inputs[i].Note.AssetID.Cmp(payload.AssetID) != 0 {
			return fmt.Errorf("prepared input %d does not match the reserved treasury note", i)
		}
		inputTotal.Add(inputTotal, note.Amount)
	}
	if inputTotal.Cmp(plan.InputTotal) != 0 {
		return fmt.Errorf("payroll batch input total is inconsistent")
	}
	paymentTotal := new(big.Int)
	for i, item := range plan.Items {
		if item.OperationID != plan.OperationID || item.CompanyID != companyID || item.PayrollID != payrollID || item.BatchID != batchID || item.Denom != denom || item.Amount == nil || item.Amount.Sign() <= 0 {
			return fmt.Errorf("payroll item %d is not bound to the batch operation", i)
		}
		recipientHash, err := HashRecipient(item.RecipientAddress)
		if err != nil {
			return fmt.Errorf("payroll item %s recipient hash: %w", item.ItemID, err)
		}
		amountHash, err := HashAmount(item.Denom, item.Amount)
		if err != nil {
			return fmt.Errorf("payroll item %s amount hash: %w", item.ItemID, err)
		}
		if item.ExpectedRecipientHash != recipientHash || item.ExpectedAmountHash != amountHash {
			return fmt.Errorf("payroll item %s expected evidence hashes are inconsistent", item.ItemID)
		}
		paymentTotal.Add(paymentTotal, item.Amount)
	}
	if paymentTotal.Cmp(plan.PaymentTotal) != 0 || new(big.Int).Sub(inputTotal, paymentTotal).Cmp(plan.Change) != 0 || plan.HasChange != (plan.Change.Sign() > 0) {
		return fmt.Errorf("payroll batch payment/change totals are inconsistent")
	}
	return nil
}

func noteRecipientMatches(note privacytypes.Note, bundle *privacytypes.ShieldedAddressBundle) bool {
	if bundle == nil || bundle.SpendPubKey == nil || bundle.ViewPubKey == nil {
		return false
	}
	sx, sy, vx, vy := new(big.Int), new(big.Int), new(big.Int), new(big.Int)
	bundle.SpendPubKey.X.BigInt(sx)
	bundle.SpendPubKey.Y.BigInt(sy)
	bundle.ViewPubKey.X.BigInt(vx)
	bundle.ViewPubKey.Y.BigInt(vy)
	return note.ReceiverSpendPubKeyX.Cmp(sx) == 0 && note.ReceiverSpendPubKeyY.Cmp(sy) == 0 && note.ReceiverViewPubKeyX.Cmp(vx) == 0 && note.ReceiverViewPubKeyY.Cmp(vy) == 0
}
