package conformance_test

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type noteReservationContract struct {
	Version                   int        `json:"version"`
	ActiveReservationStatuses []string   `json:"active_reservation_statuses"`
	AllowedTransitions        [][]string `json:"allowed_transitions"`
	RejectedTransitions       [][]string `json:"rejected_transitions"`
	BatchReserve              struct {
		Atomic bool `json:"atomic"`
	} `json:"batch_reserve"`
	NullifierLookupKey struct {
		TestVectors []struct {
			IndexKeyUTF8  string `json:"index_key_utf8"`
			NullifierUTF8 string `json:"nullifier_utf8"`
			LookupKeyHex  string `json:"lookup_key_hex"`
		} `json:"test_vectors"`
	} `json:"nullifier_lookup_key"`
	LeaseTransitionPreconditions struct {
		TokenRequiredFor                   [][]string `json:"token_required_for"`
		RecoveryWithoutTokenAfterExpiryFor [][]string `json:"recovery_without_token_after_expiry_for"`
	} `json:"lease_transition_preconditions"`
	OperationHashTestVectors []struct {
		Recipient     string `json:"recipient"`
		RecipientHash string `json:"recipient_hash"`
		Denom         string `json:"denom"`
		Amount        string `json:"amount"`
		AmountHash    string `json:"amount_hash"`
	} `json:"operation_hash_test_vectors"`
	OperationHashRejectionVectors []struct {
		Name       string `json:"name"`
		Recipient  string `json:"recipient"`
		Denom      string `json:"denom"`
		Amount     string `json:"amount"`
		RejectHash string `json:"reject_hash"`
	} `json:"operation_hash_rejection_vectors"`
	TransitionEvidencePreconditions []struct {
		Name             string          `json:"name"`
		Transition       []string        `json:"transition"`
		RequiredEvidence []string        `json:"required_evidence"`
		Positive         map[string]bool `json:"positive"`
		Negative         map[string]bool `json:"negative"`
	} `json:"transition_evidence_preconditions"`
	ManualReviewResolution struct {
		RequiredEvidence []string       `json:"required_evidence"`
		Positive         map[string]any `json:"positive"`
		Negative         map[string]any `json:"negative"`
	} `json:"manual_review_resolution"`
	RelayHandoff struct {
		Status            string         `json:"status"`
		LeaseMustRemain   bool           `json:"lease_must_remain"`
		RecordRequires    []string       `json:"record_requires"`
		ProofDiscardAfter string         `json:"proof_discard_after_handoff"`
		WriteOnceEvidence []string       `json:"write_once_evidence"`
		Positive          map[string]any `json:"positive"`
		Negative          map[string]any `json:"negative"`
		NegativeVectors   []struct {
			Name                         string `json:"name"`
			PayloadHashMatches           bool   `json:"payload_hash_matches"`
			AllReservationsProofReady    bool   `json:"all_reservations_proof_ready"`
			OperationReservationSetExact bool   `json:"operation_reservation_set_exact"`
		} `json:"negative_vectors"`
	} `json:"relay_handoff"`
	InitialStatePreconditions struct {
		ReservationStatus            string          `json:"reservation_status"`
		OperationStatus              string          `json:"operation_status"`
		ForbiddenReservationEvidence []string        `json:"forbidden_reservation_evidence"`
		ForbiddenOperationEvidence   []string        `json:"forbidden_operation_evidence"`
		Positive                     map[string]bool `json:"positive"`
		Negative                     map[string]bool `json:"negative"`
	} `json:"initial_state_preconditions"`
	FailClosedRuntimePolicy struct {
		NullifierSpentEvidence struct {
			SpentValue   bool   `json:"spent_value"`
			UnspentValue bool   `json:"unspent_value"`
			OtherValues  string `json:"other_values"`
		} `json:"nullifier_spent_evidence"`
		RelaySubmission struct {
			ChainTimeSource                   string `json:"chain_time_source"`
			ChainTimeRequired                 bool   `json:"chain_time_required"`
			RecheckImmediatelyBeforeBroadcast bool   `json:"recheck_immediately_before_broadcast"`
			OnUnavailable                     string `json:"on_unavailable"`
		} `json:"relay_submission"`
		Heartbeat struct {
			Coverage                []string `json:"coverage"`
			AwaitInFlightBeforeStop bool     `json:"await_in_flight_before_stop"`
		} `json:"heartbeat"`
		BroadcastBoundary struct {
			DurableAttemptBeforeExternalCall bool `json:"durable_attempt_before_external_call"`
			RetryBlockedUntilReconciled      bool `json:"retry_blocked_until_reconciled"`
		} `json:"broadcast_boundary"`
	} `json:"fail_closed_runtime_policy"`
	EvidenceImmutability struct {
		WriteOnceFields          []string       `json:"write_once_fields"`
		MonotonicFields          []string       `json:"monotonic_fields"`
		Negative                 map[string]any `json:"negative"`
		MutationRejectionVectors []struct {
			Field    string `json:"field"`
			Original any    `json:"original"`
			Mutation any    `json:"mutation"`
		} `json:"mutation_rejection_vectors"`
	} `json:"evidence_immutability"`
	SpentSiblingQuarantine struct {
		MatchFields  []string       `json:"match_fields"`
		TargetStatus string         `json:"target_status"`
		Positive     map[string]int `json:"positive"`
		Negative     map[string]int `json:"negative"`
	} `json:"spent_sibling_quarantine"`
	SuccessEvidenceRequired   []string `json:"success_evidence_required"`
	BatchItemIndexPolicy      string   `json:"batch_item_index_policy"`
	OperationIdentityEvidence struct {
		Required string `json:"required"`
		Vectors  []struct {
			Name              string `json:"name"`
			StoredTxHash      string `json:"stored_tx_hash"`
			StoredTxBytesHash string `json:"stored_tx_bytes_hash"`
			StoredSignDocHash string `json:"stored_sign_doc_hash"`
			TxResult          struct {
				Code        int    `json:"code"`
				TxHash      string `json:"txhash"`
				TxBytesHash string `json:"tx_bytes_hash"`
				SignDocHash string `json:"sign_doc_hash"`
			} `json:"tx_result"`
			OperationStatus string `json:"operation_status"`
		} `json:"vectors"`
	} `json:"operation_identity_evidence"`
	FixtureMigration struct {
		FromVersion      int    `json:"from_version"`
		ToVersion        int    `json:"to_version"`
		DownstreamAction string `json:"downstream_action"`
	} `json:"fixture_migration"`
	OperationSuccessExamples []struct {
		Name            string `json:"name"`
		NullifierSpent  bool   `json:"nullifier_spent"`
		EvidenceMatches bool   `json:"evidence_matches_expected_values"`
		NoteStatus      string `json:"note_status"`
		OperationStatus string `json:"operation_status"`
	} `json:"operation_success_examples"`
}

func TestNoteReservationContractFixtureMatchesGoSDK(t *testing.T) {
	data := readNoteReservationContractFixture(t)
	var contract noteReservationContract
	require.NoError(t, json.Unmarshal(data, &contract))
	require.Equal(t, 3, contract.Version)
	require.Equal(t, 1, contract.FixtureMigration.FromVersion)
	require.Equal(t, 3, contract.FixtureMigration.ToVersion)
	require.NotEmpty(t, contract.FixtureMigration.DownstreamAction)

	for _, status := range contract.ActiveReservationStatuses {
		require.True(t, privacyreservation.IsActiveReservationStatus(privacyreservation.ReservationStatus(status)), "expected %s to be active", status)
	}
	for _, transition := range contract.AllowedTransitions {
		require.Len(t, transition, 2)
		require.True(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected transition %s -> %s to be allowed", transition[0], transition[1])
	}
	fixtureTransitions := make(map[string]struct{}, len(contract.AllowedTransitions))
	for _, transition := range contract.AllowedTransitions {
		fixtureTransitions[transitionKey(transition[0], transition[1])] = struct{}{}
	}
	for _, from := range allReservationStatuses() {
		for _, to := range allReservationStatuses() {
			if !privacyreservation.CanTransitionReservation(from, to) {
				continue
			}
			_, ok := fixtureTransitions[transitionKey(string(from), string(to))]
			require.True(t, ok, "fixture is missing allowed transition %s -> %s", from, to)
		}
	}
	for _, transition := range contract.RejectedTransitions {
		require.Len(t, transition, 2)
		require.False(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected transition %s -> %s to be rejected", transition[0], transition[1])
	}
	require.True(t, contract.BatchReserve.Atomic)
	require.Equal(t, []string{
		"matching_persisted_tx_identity",
		"expected_output_commitment",
		"expected_disclosure_digest",
		"expected_recipient_hash",
		"expected_amount_hash",
		"expected_denom",
		"batch_item_index",
		"batch_item_index_known",
	}, contract.SuccessEvidenceRequired)
	require.NotEmpty(t, contract.BatchItemIndexPolicy)
	for _, transition := range contract.LeaseTransitionPreconditions.TokenRequiredFor {
		require.Len(t, transition, 2)
		require.True(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected lease-guarded transition %s -> %s to be allowed", transition[0], transition[1])
		require.True(t, privacyreservation.RequiresLeaseToken(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected transition %s -> %s to require a lease token", transition[0], transition[1])
	}
	fixtureLeaseTransitions := make(map[string]struct{}, len(contract.LeaseTransitionPreconditions.TokenRequiredFor))
	for _, transition := range contract.LeaseTransitionPreconditions.TokenRequiredFor {
		fixtureLeaseTransitions[transitionKey(transition[0], transition[1])] = struct{}{}
	}
	for _, from := range allReservationStatuses() {
		for _, to := range allReservationStatuses() {
			if !privacyreservation.RequiresLeaseToken(from, to) {
				continue
			}
			_, ok := fixtureLeaseTransitions[transitionKey(string(from), string(to))]
			require.True(t, ok, "fixture is missing lease transition %s -> %s", from, to)
		}
	}
	for _, transition := range contract.LeaseTransitionPreconditions.RecoveryWithoutTokenAfterExpiryFor {
		require.Len(t, transition, 2)
		require.True(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected recovery transition %s -> %s to be allowed", transition[0], transition[1])
		require.True(t, privacyreservation.RequiresLeaseToken(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected recovery transition %s -> %s to remain lease-guarded", transition[0], transition[1])
		require.True(t, privacyreservation.CanRecoverAfterLeaseExpiry(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected recovery transition %s -> %s to allow expired-lease recovery", transition[0], transition[1])
	}
	require.NotEmpty(t, contract.OperationHashTestVectors)
	for _, vector := range contract.OperationHashTestVectors {
		require.NotEmpty(t, vector.Recipient)
		require.NotEmpty(t, vector.RecipientHash)
		require.NotEmpty(t, vector.Denom)
		require.NotEmpty(t, vector.Amount)
		require.NotEmpty(t, vector.AmountHash)
		recipientHash, err := privacypayroll.HashRecipient(vector.Recipient)
		require.NoError(t, err)
		require.Equal(t, vector.RecipientHash, recipientHash)
		amount, ok := new(big.Int).SetString(vector.Amount, 10)
		require.True(t, ok, "operation amount must be a base-10 integer")
		amountHash, err := privacypayroll.HashAmount(vector.Denom, amount)
		require.NoError(t, err)
		require.Equal(t, vector.AmountHash, amountHash)
	}
	require.NotEmpty(t, contract.OperationHashRejectionVectors)
	for _, vector := range contract.OperationHashRejectionVectors {
		require.NotEmpty(t, vector.Name)
		switch vector.RejectHash {
		case "recipient":
			_, err := privacypayroll.HashRecipient(vector.Recipient)
			require.Error(t, err, vector.Name)
		case "amount":
			amount, ok := new(big.Int).SetString(vector.Amount, 10)
			require.True(t, ok, vector.Name)
			_, err := privacypayroll.HashAmount(vector.Denom, amount)
			require.Error(t, err, vector.Name)
		default:
			t.Fatalf("unknown operation hash rejection kind %q", vector.RejectHash)
		}
	}
	require.Len(t, contract.TransitionEvidencePreconditions, 2)
	for _, vector := range contract.TransitionEvidencePreconditions {
		require.NotEmpty(t, vector.Name)
		require.Len(t, vector.Transition, 2)
		require.True(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(vector.Transition[0]),
			privacyreservation.ReservationStatus(vector.Transition[1]),
		))
		for _, evidence := range vector.RequiredEvidence {
			require.True(t, vector.Positive[evidence], "%s positive %s", vector.Name, evidence)
		}
		missingRequiredEvidence := false
		for _, evidence := range vector.RequiredEvidence {
			if !vector.Negative[evidence] {
				missingRequiredEvidence = true
			}
		}
		require.True(t, missingRequiredEvidence, "%s negative vector must omit required evidence", vector.Name)
	}
	require.Equal(t, []string{"operator_approved", "operator_id", "operator_approval_reference"}, contract.ManualReviewResolution.RequiredEvidence)
	require.Equal(t, true, contract.ManualReviewResolution.Positive["operator_approved"])
	require.Empty(t, contract.ManualReviewResolution.Negative["operator_id"])
	require.Equal(t, string(privacyreservation.StatusProofReady), contract.RelayHandoff.Status)
	require.True(t, contract.RelayHandoff.LeaseMustRemain)
	require.Equal(t, []string{"ProofReady", "lease_owner", "lease_token", "payload_hash_matches"}, contract.RelayHandoff.RecordRequires)
	require.Equal(t, "reject", contract.RelayHandoff.ProofDiscardAfter)
	require.Equal(t, []string{"payload_hash", "relay_handed_off", "relay_handed_off_at"}, contract.RelayHandoff.WriteOnceEvidence)
	require.Equal(t, true, contract.RelayHandoff.Positive["relay_handed_off"])
	require.Equal(t, true, contract.RelayHandoff.Positive["lease_owner_present"])
	require.Equal(t, true, contract.RelayHandoff.Positive["lease_token_present"])
	require.Equal(t, true, contract.RelayHandoff.Positive["payload_hash_matches"])
	require.Equal(t, true, contract.RelayHandoff.Negative["relay_handed_off"])
	require.Equal(t, false, contract.RelayHandoff.Negative["lease_owner_present"])
	require.Equal(t, false, contract.RelayHandoff.Negative["lease_token_present"])
	require.Equal(t, false, contract.RelayHandoff.Negative["payload_hash_matches"])
	require.Len(t, contract.RelayHandoff.NegativeVectors, 3)
	require.False(t, contract.RelayHandoff.NegativeVectors[0].PayloadHashMatches)
	require.False(t, contract.RelayHandoff.NegativeVectors[1].AllReservationsProofReady)
	require.False(t, contract.RelayHandoff.NegativeVectors[2].OperationReservationSetExact)
	require.Equal(t, string(privacyreservation.StatusReserved), contract.InitialStatePreconditions.ReservationStatus)
	require.Equal(t, string(privacyreservation.OperationStatusPlanned), contract.InitialStatePreconditions.OperationStatus)
	require.Equal(t, []string{"lease", "payload_hash", "broadcast", "relay_handoff", "manual_review"}, contract.InitialStatePreconditions.ForbiddenReservationEvidence)
	require.Equal(t, []string{"payload_hash", "tx_identity"}, contract.InitialStatePreconditions.ForbiddenOperationEvidence)
	require.True(t, contract.InitialStatePreconditions.Positive["reservation_clean"])
	require.True(t, contract.InitialStatePreconditions.Positive["operation_clean"])
	require.False(t, contract.InitialStatePreconditions.Negative["reservation_clean"])
	require.False(t, contract.InitialStatePreconditions.Negative["operation_clean"])
	require.True(t, contract.FailClosedRuntimePolicy.NullifierSpentEvidence.SpentValue)
	require.False(t, contract.FailClosedRuntimePolicy.NullifierSpentEvidence.UnspentValue)
	require.Equal(t, "unknown_excluded_from_spending", contract.FailClosedRuntimePolicy.NullifierSpentEvidence.OtherValues)
	require.Equal(t, "latest_chain_block_time", contract.FailClosedRuntimePolicy.RelaySubmission.ChainTimeSource)
	require.True(t, contract.FailClosedRuntimePolicy.RelaySubmission.ChainTimeRequired)
	require.True(t, contract.FailClosedRuntimePolicy.RelaySubmission.RecheckImmediatelyBeforeBroadcast)
	require.Equal(t, "reject_submit", contract.FailClosedRuntimePolicy.RelaySubmission.OnUnavailable)
	require.Equal(t, []string{"proof_generation", "transaction_or_sign_doc_build", "proof_ready_transition"}, contract.FailClosedRuntimePolicy.Heartbeat.Coverage)
	require.True(t, contract.FailClosedRuntimePolicy.Heartbeat.AwaitInFlightBeforeStop)
	require.True(t, contract.FailClosedRuntimePolicy.BroadcastBoundary.DurableAttemptBeforeExternalCall)
	require.True(t, contract.FailClosedRuntimePolicy.BroadcastBoundary.RetryBlockedUntilReconciled)
	expectedWriteOnceFields := []string{
		"payload_hash",
		"submitted_tx_hash",
		"tx_bytes_hash",
		"sign_doc_hash",
		"expected_output_commitment",
		"expected_disclosure_digest",
		"expected_recipient_hash",
		"expected_amount",
		"expected_amount_hash",
		"expected_denom",
		"batch_item_index",
		"batch_item_index_known",
		"operation_success_evidence_required",
	}
	require.Equal(t, expectedWriteOnceFields, contract.EvidenceImmutability.WriteOnceFields)
	require.Equal(t, []string{"broadcast_attempt_count"}, contract.EvidenceImmutability.MonotonicFields)
	require.Equal(t, "", contract.EvidenceImmutability.Negative["submitted_tx_hash"])
	require.Len(t, contract.EvidenceImmutability.MutationRejectionVectors, len(expectedWriteOnceFields))
	for index, vector := range contract.EvidenceImmutability.MutationRejectionVectors {
		require.Equal(t, expectedWriteOnceFields[index], vector.Field)
		require.NotEqual(t, vector.Original, vector.Mutation, vector.Field)
	}
	require.Equal(t, []string{"owner_key_id", "nullifier_lookup_key"}, contract.SpentSiblingQuarantine.MatchFields)
	require.Equal(t, string(privacyreservation.StatusConfirmedSpent), contract.SpentSiblingQuarantine.TargetStatus)
	require.Equal(t, contract.SpentSiblingQuarantine.Positive["matching_siblings"], contract.SpentSiblingQuarantine.Positive["confirmed_spent"])
	require.Less(t, contract.SpentSiblingQuarantine.Negative["confirmed_spent"], contract.SpentSiblingQuarantine.Negative["matching_siblings"])
	require.Equal(t, "matching_persisted_tx_identity", contract.OperationIdentityEvidence.Required)
	for _, vector := range contract.OperationIdentityEvidence.Vectors {
		ctx := context.Background()
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		store := privacyreservation.NewMemoryStore()
		service := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
		created, err := service.Reserve(ctx, privacyreservation.ReserveInput{
			Reservation: privacyreservation.NoteReservation{
				ReservationID:      "reservation-a",
				NoteID:             "note-a",
				OwnerKeyID:         "owner-a",
				NullifierLookupKey: "lookup-a",
				OperationID:        "operation-a",
				Status:             privacyreservation.StatusReserved,
			},
			Operation: &privacyreservation.PayrollOperation{
				OperationID:              "operation-a",
				Status:                   privacyreservation.OperationStatusPlanned,
				ExpectedOutputCommitment: "output-a",
				ExpectedDisclosureDigest: "disclosure-a",
				ExpectedRecipientHash:    "recipient-a",
				ExpectedAmountHash:       "amount-a",
				ExpectedDenom:            "uclair",
				BatchItemIndex:           0,
				BatchItemIndexKnown:      true,
			},
		})
		require.NoError(t, err, vector.Name)
		lease, err := service.AcquireLease(ctx, created.ReservationID, "worker-a", time.Minute)
		require.NoError(t, err, vector.Name)
		_, err = service.TransitionWithLease(ctx, created.ReservationID, lease.Owner, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err, vector.Name)
		ref := privacyreservation.SubmittedReservationRef{ReservationID: created.ReservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token}
		_, _, err = service.MarkProofReadyBatch(ctx, []privacyreservation.SubmittedReservationRef{ref}, privacyreservation.ProofReadyOperationUpdate{
			OperationID: "operation-a",
			PayloadHash: "payload-a",
		})
		require.NoError(t, err, vector.Name)
		_, _, err = service.MarkBroadcastAttempting(ctx, []privacyreservation.SubmittedReservationRef{ref}, []string{"operation-a"}, privacyreservation.BroadcastAttemptStart{Reason: "fixture"})
		require.NoError(t, err, vector.Name)
		_, _, err = service.MarkSubmittedBatch(ctx, []privacyreservation.SubmittedReservationRef{ref}, []string{"operation-a"}, privacyreservation.SubmittedReservationUpdate{
			TxHash:      vector.StoredTxHash,
			TxBytesHash: vector.StoredTxBytesHash,
			SignDocHash: vector.StoredSignDocHash,
		})
		require.NoError(t, err, vector.Name)
		result, err := service.Reconcile(ctx, created.ReservationID, privacyreservation.OperationEvidence{
			NullifierSpent:      true,
			TxKnown:             vector.TxResult.TxHash != "" || vector.TxResult.TxBytesHash != "",
			TxSucceeded:         vector.TxResult.Code == 0,
			TxHash:              vector.TxResult.TxHash,
			TxBytesHash:         vector.TxResult.TxBytesHash,
			SignDocHash:         vector.TxResult.SignDocHash,
			OutputCommitment:    "output-a",
			DisclosureDigest:    "disclosure-a",
			RecipientHash:       "recipient-a",
			AmountHash:          "amount-a",
			Denom:               "uclair",
			BatchItemIndex:      0,
			BatchItemIndexKnown: true,
		})
		require.NoError(t, err, vector.Name)
		require.Equal(t, vector.OperationStatus, string(result.OperationStatus), vector.Name)
	}
	for _, vector := range contract.NullifierLookupKey.TestVectors {
		got, err := privacyreservation.NullifierLookupKey([]byte(vector.IndexKeyUTF8), []byte(vector.NullifierUTF8))
		require.NoError(t, err)
		require.Equal(t, vector.LookupKeyHex, got)
	}
	require.NotEmpty(t, contract.OperationSuccessExamples)
	for _, example := range contract.OperationSuccessExamples {
		require.NotEmpty(t, example.Name)
		require.True(t, example.NullifierSpent)
		require.NotEmpty(t, example.NoteStatus)
		require.NotEmpty(t, example.OperationStatus)
	}
}

func normalizedContractTxIdentity(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
}

func readNoteReservationContractFixture(t *testing.T) []byte {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	fixturePath := filepath.Join(filepath.Dir(filename), "testdata", "privacy_note_reservation_contract.json")
	data, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	return data
}

func allReservationStatuses() []privacyreservation.ReservationStatus {
	return []privacyreservation.ReservationStatus{
		privacyreservation.StatusDiscovered,
		privacyreservation.StatusAvailable,
		privacyreservation.StatusReserved,
		privacyreservation.StatusProving,
		privacyreservation.StatusProofReady,
		privacyreservation.StatusSubmitted,
		privacyreservation.StatusUnknown,
		privacyreservation.StatusManualReview,
		privacyreservation.StatusFailed,
		privacyreservation.StatusReleased,
		privacyreservation.StatusReplanRequired,
		privacyreservation.StatusConfirmedSpent,
	}
}

func transitionKey(from string, to string) string {
	return from + "\x00" + to
}
