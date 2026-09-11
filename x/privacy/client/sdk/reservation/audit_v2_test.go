package reservation

import (
	"context"
	"testing"
	"time"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/stretchr/testify/require"
)

func TestReleaseStaleAuditReservationsOnArtifactChange(t *testing.T) {
	snapshot := reservationAuditSnapshot(t)
	now := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	binding, err := NewAuditBinding(snapshot, now.Add(time.Minute))
	require.NoError(t, err)

	svc := Service{Store: NewMemoryStore(), Now: func() time.Time { return now }}
	created, err := svc.Reserve(context.Background(), ReserveInput{Reservation: NoteReservation{
		ReservationID: "audit-bound", OwnerKeyID: "owner", NullifierLookupKey: "lookup", Audit: binding,
	}})
	require.NoError(t, err)

	changed := snapshot
	changed.ArtifactHash[0] ^= 1
	released, err := svc.ReleaseStaleAuditReservations(context.Background(), changed)
	require.NoError(t, err)
	require.Len(t, released, 1)
	require.Equal(t, created.ReservationID, released[0].ReservationID)
	require.Equal(t, StatusReleased, released[0].Status)
}

func TestReserveRejectsMalformedAuditBinding(t *testing.T) {
	svc := Service{Store: NewMemoryStore(), Now: func() time.Time { return time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC) }}
	_, err := svc.Reserve(context.Background(), ReserveInput{Reservation: NoteReservation{
		ReservationID: "invalid-audit-bound", OwnerKeyID: "owner", NullifierLookupKey: "lookup",
		Audit: &AuditBinding{NetworkID: "bad", KeyID: "bad", Epoch: 1, ArtifactHash: "bad", ExpiresAtUnix: time.Now().Add(time.Hour).Unix()},
	}})
	require.Error(t, err)
}

func reservationAuditSnapshot(t *testing.T) privacyaudit.Snapshot {
	t.Helper()
	secretBytes := make([]byte, 32)
	secretBytes[31] = 19
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(secretBytes)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 5, ArtifactHash: [32]byte{2}}
}
