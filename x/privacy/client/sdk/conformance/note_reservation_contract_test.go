package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

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
		TokenRequiredFor [][]string `json:"token_required_for"`
	} `json:"lease_transition_preconditions"`
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
	require.Equal(t, 1, contract.Version)

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
	for _, transition := range contract.RejectedTransitions {
		require.Len(t, transition, 2)
		require.False(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected transition %s -> %s to be rejected", transition[0], transition[1])
	}
	require.True(t, contract.BatchReserve.Atomic)
	for _, transition := range contract.LeaseTransitionPreconditions.TokenRequiredFor {
		require.Len(t, transition, 2)
		require.True(t, privacyreservation.CanTransitionReservation(
			privacyreservation.ReservationStatus(transition[0]),
			privacyreservation.ReservationStatus(transition[1]),
		), "expected lease-guarded transition %s -> %s to be allowed", transition[0], transition[1])
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

func readNoteReservationContractFixture(t *testing.T) []byte {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	fixturePath := filepath.Join(filepath.Dir(filename), "testdata", "privacy_note_reservation_contract.json")
	data, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	return data
}
