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
