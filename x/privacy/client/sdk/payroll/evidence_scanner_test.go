package payroll

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestEvidenceScannerBuildsTransferBatchEvidence(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	service := Service{Reservation: privacyreservation.Service{Store: store}, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	input.Items[0].ExpectedOutputCommitment = "recipient-commitment-a"
	userDigest := strings.Repeat("0", 63) + "1"
	auditDigest := strings.Repeat("0", 63) + "2"
	selfViewDigest := strings.Repeat("0", 63) + "3"
	input.Items[0].ExpectedDisclosureDigest = auditDigest
	input.Items[0].DisclosurePolicy.UserPrivacyPolicy = privacytypes.TransferPrivacyPolicyDiscloseAmount
	input.Items[0].DisclosurePolicy.UserDisclosureMode = privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
	input.Items[0].DisclosurePolicy.ExpectedUserDisclosureDigest = userDigest
	input.Items[0].DisclosurePolicy.ExpectedAuditDisclosureDigest = auditDigest
	input.Items[0].DisclosurePolicy.ExpectedSelfViewDisclosureDigest = selfViewDigest
	plan, err := service.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := service.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	tx := TxObservation{
		TxHash: "TXHASH",
		Height: 12,
		Code:   0,
		Events: []ChainEvent{{
			Type: privacytypes.EventTypeShieldedTransfer,
			Attributes: []ChainEventAttribute{
				{Key: privacytypes.AttributeKeyNullifier1, Value: "lookup-large"},
				{Key: privacytypes.AttributeKeyNullifier2, Value: "lookup-zero"},
				{Key: privacytypes.AttributeKeyCommitment1, Value: "recipient-commitment-a"},
				{Key: privacytypes.AttributeKeyCommitment2, Value: "change-commitment-a"},
				{Key: privacytypes.AttributeKeyUserDisclosureDigest, Value: userDigest},
				{Key: privacytypes.AttributeKeyAuditDisclosureDigest, Value: auditDigest},
				{Key: privacytypes.AttributeKeySelfViewDisclosureDigest, Value: selfViewDigest},
			},
		}},
	}

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, *confirmed, tx, nil)
	require.NoError(t, err)
	require.True(t, report.TxKnown)
	require.True(t, report.TxSucceeded)
	require.False(t, report.TxFailed)
	require.Equal(t, 1, report.ObservedEvents)
	require.Equal(t, 2, report.ScannedReservations)
	require.Len(t, report.Evidence, 2)
	for _, item := range report.Evidence {
		require.Equal(t, confirmed.Items[0].OperationID, item.OperationID)
		require.Equal(t, "TXHASH", item.Evidence.TxHash)
		require.Equal(t, "recipient-commitment-a", item.Evidence.OutputCommitment)
		require.Equal(t, auditDigest, item.Evidence.DisclosureDigest)
		require.Equal(t, userDigest, item.Evidence.UserDisclosureDigest)
		require.Equal(t, auditDigest, item.Evidence.AuditDisclosureDigest)
		require.Equal(t, selfViewDigest, item.Evidence.SelfViewDisclosureDigest)
		require.True(t, item.Evidence.NullifierSpent)
		require.True(t, item.Evidence.BatchItemIndexKnown)
		require.Equal(t, 0, item.Evidence.BatchItemIndex)
	}
}

func TestEvidenceScannerSkipsItemsWithoutObservedEvents(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	for _, input := range []privacyreservation.ReserveInput{
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-1", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-large", OperationID: "op-1"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-1", ItemID: "item-1", Status: privacyreservation.OperationStatusPlanned},
		},
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-2", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-second", OperationID: "op-2"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-2", ItemID: "item-2", Status: privacyreservation.OperationStatusPlanned},
		},
	} {
		_, err := reservationService.Reserve(ctx, input)
		require.NoError(t, err)
	}

	plan := PayrollPlan{Items: []PayrollPlanItem{
		{
			ItemID:           "item-1",
			OperationID:      "op-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(1),
			Denom:            "uclair",
			InputNotes: []TreasuryNote{{
				NoteID:               "large",
				ReservationID:        "reservation-1",
				NullifierLookupKey:   "lookup-large",
				NullifierLookupKeyID: "lookup-v1",
			}},
		},
		{
			ItemID:           "item-2",
			OperationID:      "op-2",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(1),
			Denom:            "uclair",
			InputNotes: []TreasuryNote{{
				NoteID:               "second",
				ReservationID:        "reservation-2",
				NullifierLookupKey:   "lookup-second",
				NullifierLookupKeyID: "lookup-v1",
			}},
		},
	}}
	tx := TxObservation{
		TxHash: "TXHASH",
		Code:   0,
		Events: []ChainEvent{{
			Type: privacytypes.EventTypeShieldedTransfer,
			Attributes: []ChainEventAttribute{
				{Key: privacytypes.AttributeKeyNullifier1, Value: "lookup-large"},
				{Key: privacytypes.AttributeKeyCommitment1, Value: "commitment-a"},
			},
		}},
	}

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, plan, tx, nil)
	require.NoError(t, err)
	require.Equal(t, 1, report.ObservedEvents)
	require.Equal(t, 1, report.ScannedReservations)
	require.Len(t, report.Evidence, 1)
	require.Contains(t, report.Warnings[0], "observed 1 shielded_transfer events for 2 payroll items")
	require.Equal(t, "reservation-1", report.Evidence[0].ReservationID)
	require.Equal(t, "op-1", report.Evidence[0].OperationID)
}

func TestEvidenceScannerMatchesLaterChunkByNullifier(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	for _, input := range []privacyreservation.ReserveInput{
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-1", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-large", OperationID: "op-1"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-1", ItemID: "item-1", Status: privacyreservation.OperationStatusPlanned},
		},
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-2", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-second", OperationID: "op-2"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-2", ItemID: "item-2", Status: privacyreservation.OperationStatusPlanned},
		},
	} {
		_, err := reservationService.Reserve(ctx, input)
		require.NoError(t, err)
	}
	plan := PayrollPlan{Items: []PayrollPlanItem{
		{
			ItemID:           "item-1",
			OperationID:      "op-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(1),
			Denom:            "uclair",
			InputNotes: []TreasuryNote{{
				NoteID:             "large",
				ReservationID:      "reservation-1",
				NullifierLookupKey: "lookup-large",
			}},
		},
		{
			ItemID:           "item-2",
			OperationID:      "op-2",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(1),
			Denom:            "uclair",
			InputNotes: []TreasuryNote{{
				NoteID:             "second",
				ReservationID:      "reservation-2",
				NullifierLookupKey: "lookup-second",
			}},
		},
	}}
	tx := TxObservation{
		TxHash: "TXHASH",
		Code:   0,
		Events: []ChainEvent{{
			Type: privacytypes.EventTypeShieldedTransfer,
			Attributes: []ChainEventAttribute{
				{Key: privacytypes.AttributeKeyNullifier1, Value: "lookup-second"},
				{Key: privacytypes.AttributeKeyCommitment1, Value: "commitment-b"},
			},
		}},
	}

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, plan, tx, nil)
	require.NoError(t, err)
	require.Len(t, report.Evidence, 1)
	require.Equal(t, "reservation-2", report.Evidence[0].ReservationID)
	require.Equal(t, "op-2", report.Evidence[0].OperationID)
	require.Equal(t, 1, report.Evidence[0].Evidence.BatchItemIndex)
	require.Equal(t, "commitment-b", report.Evidence[0].Evidence.OutputCommitment)
}

func TestEvidenceScannerRequiresAllItemNullifiersForEventMatch(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	for _, input := range []privacyreservation.ReserveInput{
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-large", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-large", OperationID: "op-1"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-1", ItemID: "item-1", Status: privacyreservation.OperationStatusPlanned},
		},
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-zero", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-zero", OperationID: "op-1"},
		},
	} {
		_, err := reservationService.Reserve(ctx, input)
		require.NoError(t, err)
	}
	plan := PayrollPlan{Items: []PayrollPlanItem{{
		ItemID:           "item-1",
		OperationID:      "op-1",
		RecipientAddress: testRecipientAddress("1"),
		Amount:           big.NewInt(1),
		Denom:            "uclair",
		InputNotes: []TreasuryNote{
			{NoteID: "large", ReservationID: "reservation-large", NullifierLookupKey: "lookup-large"},
			{NoteID: "zero", ReservationID: "reservation-zero", NullifierLookupKey: "lookup-zero"},
		},
	}}}
	tx := TxObservation{
		TxHash: "TXHASH",
		Code:   0,
		Events: []ChainEvent{{
			Type: privacytypes.EventTypeShieldedTransfer,
			Attributes: []ChainEventAttribute{
				{Key: privacytypes.AttributeKeyNullifier1, Value: "lookup-large"},
				{Key: privacytypes.AttributeKeyCommitment1, Value: "commitment-a"},
			},
		}},
	}

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, plan, tx, nil)
	require.NoError(t, err)
	require.Len(t, report.Evidence, 0)
	require.Equal(t, 0, report.ScannedReservations)
}

func TestParseTxObservationJSONAcceptsCosmosTxResponse(t *testing.T) {
	payload := map[string]any{
		"tx_response": map[string]any{
			"txhash": "ABC",
			"height": "7",
			"code":   float64(0),
			"events": []any{
				map[string]any{
					"type": privacytypes.EventTypeShieldedTransfer,
					"attributes": []any{
						map[string]any{"key": privacytypes.AttributeKeyCommitment1, "value": "commitment-a"},
					},
				},
			},
		},
	}
	bz, err := json.Marshal(payload)
	require.NoError(t, err)

	tx, err := ParseTxObservationJSON(bz)
	require.NoError(t, err)
	require.Equal(t, "ABC", tx.TxHash)
	require.EqualValues(t, 7, tx.Height)
	require.Len(t, tx.Events, 1)
	require.Equal(t, privacytypes.EventTypeShieldedTransfer, tx.Events[0].Type)
	require.Equal(t, privacytypes.AttributeKeyCommitment1, tx.Events[0].Attributes[0].Key)
}

func TestEvidenceScannerUsesExplicitNullifierStatuses(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	service := Service{Reservation: privacyreservation.Service{Store: store}, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	input.Items[0].ExpectedOutputCommitment = "recipient-commitment-a"
	plan, err := service.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := service.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, *confirmed, TxObservation{TxHash: "TXHASH", Code: 0}, []NullifierStatus{
		{Nullifier: "lookup-large", Used: true},
		{Nullifier: "lookup-zero", Used: false},
	})
	require.NoError(t, err)
	require.Len(t, report.Evidence, 2)
	require.True(t, report.Evidence[0].Evidence.NullifierSpent)
	require.False(t, report.Evidence[0].Evidence.NullifierUnspentConfirmed)
	require.False(t, report.Evidence[1].Evidence.NullifierSpent)
	require.True(t, report.Evidence[1].Evidence.NullifierUnspentConfirmed)
}

func TestEvidenceScannerEmitsFailedEvidenceWithoutEvents(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	service := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := service.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := service.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	markPayrollPlanSubmittedForScannerTest(t, ctx, reservationService, confirmed.Items[0], "FAILED_TX")

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, *confirmed, TxObservation{
		TxHash: "FAILED_TX",
		Height: 42,
		Code:   7,
		RawLog: "out of gas",
	}, []NullifierStatus{
		{Nullifier: "lookup-large", Used: false},
		{Nullifier: "lookup-zero", Used: false},
	})
	require.NoError(t, err)
	require.True(t, report.TxFailed)
	require.Equal(t, 0, report.ObservedEvents)
	require.Equal(t, 2, report.ScannedReservations)
	require.Len(t, report.Evidence, 2)
	evidences := make([]privacyreservation.OperationReservationEvidence, 0, len(report.Evidence))
	for _, item := range report.Evidence {
		require.True(t, item.Evidence.TxKnown)
		require.True(t, item.Evidence.TxFailed)
		require.False(t, item.Evidence.TxSucceeded)
		require.False(t, item.Evidence.NullifierSpent)
		require.True(t, item.Evidence.NullifierUnspentConfirmed)
		evidences = append(evidences, privacyreservation.OperationReservationEvidence{
			ReservationID: item.ReservationID,
			Evidence:      item.Evidence,
		})
	}
	result, err := reservationService.ReconcileOperation(ctx, confirmed.Items[0].OperationID, evidences)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusFailed, result.ReservationStatus)
	operation, err := store.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusFailed, operation.Status)
}

func TestEvidenceScannerLimitsFailedEvidenceWithoutEventsToMatchingTx(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	for _, input := range []privacyreservation.ReserveInput{
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-1", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-1", OperationID: "op-1"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-1", ItemID: "item-1", Status: privacyreservation.OperationStatusPlanned},
		},
		{
			Reservation: privacyreservation.NoteReservation{ReservationID: "reservation-2", OwnerKeyID: "owner-a", NullifierLookupKey: "lookup-2", OperationID: "op-2"},
			Operation:   &privacyreservation.PayrollOperation{OperationID: "op-2", ItemID: "item-2", Status: privacyreservation.OperationStatusPlanned},
		},
	} {
		_, err := reservationService.Reserve(ctx, input)
		require.NoError(t, err)
	}
	markReservationSubmittedForScannerTest(t, ctx, reservationService, "reservation-1", "op-1", "FAILED_TX")
	markReservationSubmittedForScannerTest(t, ctx, reservationService, "reservation-2", "op-2", "OTHER_TX")
	plan := PayrollPlan{Items: []PayrollPlanItem{
		{
			ItemID:           "item-1",
			OperationID:      "op-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(1),
			Denom:            "uclair",
			InputNotes:       []TreasuryNote{{NoteID: "note-1", ReservationID: "reservation-1", NullifierLookupKey: "lookup-1"}},
		},
		{
			ItemID:           "item-2",
			OperationID:      "op-2",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(1),
			Denom:            "uclair",
			InputNotes:       []TreasuryNote{{NoteID: "note-2", ReservationID: "reservation-2", NullifierLookupKey: "lookup-2"}},
		},
	}}

	report, err := (EvidenceScanner{Store: store}).ScanTransferBatch(ctx, plan, TxObservation{
		TxHash: "FAILED_TX",
		Height: 42,
		Code:   7,
		RawLog: "out of gas",
	}, []NullifierStatus{
		{Nullifier: "lookup-1", Used: false},
		{Nullifier: "lookup-2", Used: false},
	})
	require.NoError(t, err)
	require.Len(t, report.Evidence, 1)
	require.Equal(t, "reservation-1", report.Evidence[0].ReservationID)
	require.True(t, report.Evidence[0].Evidence.TxFailed)
	require.True(t, report.Evidence[0].Evidence.NullifierUnspentConfirmed)
}

func markPayrollPlanSubmittedForScannerTest(t *testing.T, ctx context.Context, service privacyreservation.Service, item PayrollPlanItem, txHash string) {
	t.Helper()
	reservationIDs := make([]string, 0, len(item.InputNotes))
	for _, note := range item.InputNotes {
		reservationIDs = append(reservationIDs, note.ReservationID)
	}
	refs := markOperationProofReadyForScannerTest(t, ctx, service, item.OperationID, reservationIDs)
	_, _, err := service.MarkBroadcastAttempting(ctx, refs, []string{item.OperationID}, privacyreservation.BroadcastAttemptStart{Reason: "scanner test submission", TxHash: txHash})
	require.NoError(t, err)
	_, _, err = service.MarkSubmittedBatch(ctx, refs, []string{item.OperationID}, privacyreservation.SubmittedReservationUpdate{TxHash: txHash})
	require.NoError(t, err)
}

func markReservationSubmittedForScannerTest(t *testing.T, ctx context.Context, service privacyreservation.Service, reservationID string, operationID string, txHash string) {
	t.Helper()
	refs := markOperationProofReadyForScannerTest(t, ctx, service, operationID, []string{reservationID})
	_, _, err := service.MarkBroadcastAttempting(ctx, refs, []string{operationID}, privacyreservation.BroadcastAttemptStart{Reason: "scanner test submission", TxHash: txHash})
	require.NoError(t, err)
	_, _, err = service.MarkSubmittedBatch(ctx, refs, []string{operationID}, privacyreservation.SubmittedReservationUpdate{TxHash: txHash})
	require.NoError(t, err)
}

func markOperationProofReadyForScannerTest(t *testing.T, ctx context.Context, service privacyreservation.Service, operationID string, reservationIDs []string) []privacyreservation.SubmittedReservationRef {
	t.Helper()
	refs, _, err := service.BeginProvingOperation(ctx, operationID, reservationIDs, "scanner-test", time.Minute)
	require.NoError(t, err)
	_, _, err = service.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID: operationID,
		PayloadHash: "scanner-test-payload-" + operationID,
	})
	require.NoError(t, err)
	return refs
}
