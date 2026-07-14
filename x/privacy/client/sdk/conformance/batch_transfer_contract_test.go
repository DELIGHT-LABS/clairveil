package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type batchTransferContractFixture struct {
	SchemaVersion   string `json:"schema_version"`
	PayloadVersion  string `json:"payload_version"`
	ProofVersion    string `json:"proof_version"`
	CircuitSetID    string `json:"circuit_set_id"`
	CircuitID       string `json:"circuit_id"`
	ProverRoute     string `json:"prover_route"`
	RequestVersion  string `json:"request_version"`
	ResponseVersion string `json:"response_version"`
	MaxInputs       int    `json:"max_inputs"`
	MaxOutputs      int    `json:"max_outputs"`
	Cases           []struct {
		ID                  string   `json:"id"`
		InputAmounts        []uint64 `json:"input_amounts"`
		PaymentAmounts      []uint64 `json:"payment_amounts"`
		ExpectedOutputRoles []string `json:"expected_output_roles"`
		OutputMode          string   `json:"output_mode"`
		DisclosureModes     []string `json:"disclosure_modes"`
		SelfView            string   `json:"self_view"`
	} `json:"cases"`
	RestartRetry struct {
		StableOperationID             bool `json:"stable_operation_id"`
		ReuseSignedTxBytes            bool `json:"reuse_signed_tx_bytes"`
		ReconcileTxHashFirst          bool `json:"reconcile_tx_hash_first"`
		ReconcileNullifiersBeforeSign bool `json:"reconcile_nullifiers_before_resign"`
		AutomaticMultiProverFailover  bool `json:"automatic_multi_prover_failover"`
		ItemSuccessRequiresEvidence   bool `json:"item_success_requires_output_evidence"`
	} `json:"restart_retry"`
	Scan struct {
		CursorOrder                     []string `json:"cursor_order"`
		TypedQueryRequired              bool     `json:"typed_query_required"`
		ABCIFallbackAfterTypedFailure   bool     `json:"abci_fallback_after_typed_failure"`
		SafeModeDecryptsViewTagMismatch bool     `json:"safe_mode_decrypts_view_tag_mismatch"`
		CommitmentRecomputationRequired bool     `json:"commitment_recomputation_required"`
	} `json:"scan"`
	Payroll struct {
		OperationToProofJob        string   `json:"operation_to_proof_job"`
		OperationToReservations    string   `json:"operation_to_input_reservations"`
		OperationToItemOutputs     string   `json:"operation_to_item_outputs"`
		BatchAndItemStatusSeparate bool     `json:"batch_and_item_status_separate"`
		RequiredOutputEvidence     []string `json:"required_output_evidence"`
	} `json:"payroll"`
}

func TestBatchTransferContract(t *testing.T) {
	var fixture batchTransferContractFixture
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	bz, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "testdata", "privacy_batch_transfer_v1_contract.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(bz, &fixture))

	require.Equal(t, "clairveil.batch-transfer.contract.v1", fixture.SchemaVersion)
	require.Equal(t, privacybatchtransfer.PreparedBatchTransferPayloadVersion, fixture.PayloadVersion)
	require.Equal(t, privacybatchtransfer.PreparedBatchTransferProofVersion, fixture.ProofVersion)
	require.Equal(t, privacytypes.ActiveCircuitSetID, fixture.CircuitSetID)
	require.Equal(t, string(privacyzk.CircuitBatchJoinSplit16x32V1), fixture.CircuitID)
	require.Equal(t, privacyprovertransport.BatchTransferProofPath, fixture.ProverRoute)
	require.Equal(t, privacyprovertransport.BatchTransferProofRequestVersion, fixture.RequestVersion)
	require.Equal(t, privacyprovertransport.BatchTransferProofResponseVersion, fixture.ResponseVersion)
	require.Equal(t, int(privacytypes.BatchJoinSplitV1MaxInputs), fixture.MaxInputs)
	require.Equal(t, int(privacytypes.BatchJoinSplitV1MaxOutputs), fixture.MaxOutputs)

	wantCases := []string{
		"one-input-one-payment",
		"three-input-four-output-mixed-disclosure",
		"thirty-one-payments-plus-change",
		"exact-thirty-two-payments",
		"explicit-zero-padding",
	}
	require.Len(t, fixture.Cases, len(wantCases))
	for i, testCase := range fixture.Cases {
		require.Equal(t, wantCases[i], testCase.ID)
		require.NotEmpty(t, testCase.InputAmounts)
		require.LessOrEqual(t, len(testCase.InputAmounts), fixture.MaxInputs)
		require.NotEmpty(t, testCase.PaymentAmounts)
		require.LessOrEqual(t, len(testCase.ExpectedOutputRoles), fixture.MaxOutputs)
		require.Len(t, testCase.DisclosureModes, len(testCase.ExpectedOutputRoles))
		require.Contains(t, []string{"compact", "exact32"}, testCase.OutputMode)
		require.Contains(t, []string{"enabled", "disabled"}, testCase.SelfView)

		var inputTotal, paymentTotal uint64
		for _, amount := range testCase.InputAmounts {
			inputTotal += amount
		}
		for _, amount := range testCase.PaymentAmounts {
			require.Positive(t, amount)
			paymentTotal += amount
		}
		require.LessOrEqual(t, paymentTotal, inputTotal)
		changeCount, paddingCount := 0, 0
		for _, role := range testCase.ExpectedOutputRoles {
			switch role {
			case "payment":
			case "change":
				changeCount++
			case "padding":
				paddingCount++
			default:
				t.Fatalf("case %s has unknown output role %q", testCase.ID, role)
			}
		}
		require.Equal(t, len(testCase.PaymentAmounts), len(testCase.ExpectedOutputRoles)-changeCount-paddingCount)
		if paymentTotal < inputTotal {
			require.Equal(t, 1, changeCount)
		} else {
			require.Zero(t, changeCount)
		}
		if testCase.OutputMode == "exact32" {
			require.Len(t, testCase.ExpectedOutputRoles, fixture.MaxOutputs)
		}
	}

	require.True(t, fixture.RestartRetry.StableOperationID)
	require.True(t, fixture.RestartRetry.ReuseSignedTxBytes)
	require.True(t, fixture.RestartRetry.ReconcileTxHashFirst)
	require.True(t, fixture.RestartRetry.ReconcileNullifiersBeforeSign)
	require.False(t, fixture.RestartRetry.AutomaticMultiProverFailover)
	require.True(t, fixture.RestartRetry.ItemSuccessRequiresEvidence)
	require.Equal(t, []string{"height", "global_sequence", "output_index"}, fixture.Scan.CursorOrder)
	require.True(t, fixture.Scan.TypedQueryRequired)
	require.False(t, fixture.Scan.ABCIFallbackAfterTypedFailure)
	require.True(t, fixture.Scan.SafeModeDecryptsViewTagMismatch)
	require.True(t, fixture.Scan.CommitmentRecomputationRequired)
	require.Equal(t, "one-to-one", fixture.Payroll.OperationToProofJob)
	require.Equal(t, "one-to-many", fixture.Payroll.OperationToReservations)
	require.Equal(t, "one-to-many", fixture.Payroll.OperationToItemOutputs)
	require.True(t, fixture.Payroll.BatchAndItemStatusSeparate)
	require.Equal(t, []string{"output_index", "commitment", "recipient_hash", "amount", "denom_or_asset_id", "user_digest", "full_digest", "audit_key_id", "audit_key_epoch"}, fixture.Payroll.RequiredOutputEvidence)
}
