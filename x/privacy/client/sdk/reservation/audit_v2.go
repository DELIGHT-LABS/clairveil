package reservation

import (
	"encoding/hex"
	"fmt"
	"time"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
)

// AuditBinding is the public namespace attached to a note reservation that is
// about to feed one v2 audit-field transaction. It deliberately has no JSON
// route for r, encryption witness data, note plaintext, or a prover response.
type AuditBinding struct {
	NetworkID     string `json:"network_id"`
	KeyID         string `json:"key_id"`
	Epoch         uint64 `json:"epoch"`
	ArtifactHash  string `json:"artifact_hash"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

func NewAuditBinding(snapshot privacyaudit.Snapshot, expiresAt time.Time) (*AuditBinding, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	if expiresAt.IsZero() || expiresAt.Unix() <= 0 {
		return nil, fmt.Errorf("audit reservation expiry is required")
	}
	keyID := snapshot.Key.ID()
	binding := &AuditBinding{
		NetworkID:     hex.EncodeToString(snapshot.Network[:]),
		KeyID:         hex.EncodeToString(keyID[:]),
		Epoch:         snapshot.Epoch,
		ArtifactHash:  hex.EncodeToString(snapshot.ArtifactHash[:]),
		ExpiresAtUnix: expiresAt.UTC().Unix(),
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	return binding, nil
}

func (b AuditBinding) Validate() error {
	if b.Epoch == 0 || b.ExpiresAtUnix <= 0 {
		return fmt.Errorf("epoch and expiry are required")
	}
	for name, value := range map[string]string{
		"network id": b.NetworkID, "key id": b.KeyID, "artifact hash": b.ArtifactHash,
	} {
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != value {
			return fmt.Errorf("invalid audit %s", name)
		}
		zero := true
		for _, part := range decoded {
			zero = zero && part == 0
		}
		if zero {
			return fmt.Errorf("zero audit %s", name)
		}
	}
	return nil
}

// ValidFor verifies a saved reservation against the currently authenticated
// snapshot. The strict deadline check means a reused or delayed r/nonce is
// never retained across a preparation expiry.
func (b AuditBinding) ValidFor(snapshot privacyaudit.Snapshot, now time.Time) bool {
	if b.Validate() != nil || snapshot.Validate() != nil || (!now.IsZero() && now.Unix() >= b.ExpiresAtUnix) {
		return false
	}
	keyID := snapshot.Key.ID()
	return b.NetworkID == hex.EncodeToString(snapshot.Network[:]) &&
		b.KeyID == hex.EncodeToString(keyID[:]) &&
		b.Epoch == snapshot.Epoch &&
		b.ArtifactHash == hex.EncodeToString(snapshot.ArtifactHash[:])
}
