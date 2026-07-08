package payroll

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

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
	require.False(t, report.Evidence[1].Evidence.NullifierSpent)
}
