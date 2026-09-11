package types

import (
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/internal/strictjson"
)

type InitialAuditKeyV4 struct {
	Epoch     uint64 `json:"epoch"`
	KeyID     []byte `json:"key_id"`
	Suite     uint16 `json:"suite"`
	PublicKey []byte `json:"public_key"`
	PoP       []byte `json:"pop"`
}

// AuditOriginV4 retains provenance for governance-managed audit-key state.
// It is deliberately separate from privacy transaction provenance, which is
// represented only by the privacy scan and event indexes.
type AuditOriginV4 struct {
	Kind   uint8  `json:"kind"`
	Anchor []byte `json:"anchor"`
	Height uint64 `json:"height"`
}

type AuditKeyHistoryV4 struct {
	Epoch            uint64         `json:"epoch"`
	KeyID            []byte         `json:"key_id"`
	Suite            uint16         `json:"suite"`
	PublicKey        []byte         `json:"public_key"`
	PoP              []byte         `json:"pop"`
	ActivationHeight uint64         `json:"activation_height"`
	RegisteredHeight uint64         `json:"registered_height"`
	Origin           *AuditOriginV4 `json:"origin"`
}

type AuditKeyActivationV4 struct {
	Epoch            uint64 `json:"epoch"`
	ActivationHeight uint64 `json:"activation_height"`
}

type AuditKeyCancellationV4 struct {
	Epoch           uint64         `json:"epoch"`
	Origin          *AuditOriginV4 `json:"origin"`
	CancelledHeight uint64         `json:"cancelled_height"`
}

type GenesisStateV4 struct {
	FormatVersion      uint32                    `json:"format_version"`
	Mode               string                    `json:"mode"`
	NetworkNonce       []byte                    `json:"network_nonce32"`
	InitialHeight      uint64                    `json:"initial_height"`
	RestartHeight      uint64                    `json:"restart_height,omitempty"`
	InitialAuditKey    InitialAuditKeyV4         `json:"initial_audit_key"`
	CircuitSetIdentity *CircuitSetIdentity       `json:"circuit_set_identity"`
	State              *GenesisState             `json:"state,omitempty"`
	AssetRegistry      []*AssetRegistryEntryV1   `json:"asset_registry"`
	AuditKeyHistory    []*AuditKeyHistoryV4      `json:"audit_key_history,omitempty"`
	ActiveAuditEpoch   uint64                    `json:"active_audit_epoch,omitempty"`
	PendingAuditKey    *AuditKeyActivationV4     `json:"pending_audit_key,omitempty"`
	AuditCancellations []*AuditKeyCancellationV4 `json:"audit_cancellations,omitempty"`
	PrivacyHalted      bool                      `json:"privacy_halted"`
	TreeCapacity       uint64                    `json:"tree_capacity"`
}

func ParseFreshGenesisV4(data []byte) (GenesisStateV4, error) {
	var g GenesisStateV4
	if err := strictjson.Decode(data, &g); err != nil {
		return g, err
	}
	return g, g.Validate()
}

func (g GenesisStateV4) Validate() error {
	if g.FormatVersion != 4 || g.Mode != "FRESH" || len(g.NetworkNonce) != 32 || g.InitialHeight == 0 || g.InitialHeight > 1<<63-1 || g.TreeCapacity != 1<<32 {
		return fmt.Errorf("invalid FRESH genesis identity/mode/capacity")
	}
	if err := ValidateAuditCircuitSetIdentity(g.CircuitSetIdentity); err != nil {
		return fmt.Errorf("invalid FRESH circuit identity: %w", err)
	}
	if g.State != nil {
		if g.RestartHeight == 0 {
			return fmt.Errorf("FRESH exported restart height is required")
		}
		if err := g.State.Validate(); err != nil {
			return fmt.Errorf("invalid FRESH privacy state: %w", err)
		}
		encoded, _ := g.CircuitSetIdentity.Marshal()
		stateIdentity, _ := g.State.CircuitSetIdentity.Marshal()
		if string(encoded) != string(stateIdentity) {
			return fmt.Errorf("FRESH privacy state circuit identity differs from audit identity")
		}
		if err := validateGenesisAssetRegistryV1(g.AssetRegistry); err != nil {
			return fmt.Errorf("FRESH audit asset registry: %w", err)
		}
		if err := validateGenesisAssetRegistryV1(g.State.AssetRegistry); err != nil {
			return fmt.Errorf("FRESH exported asset registry: %w", err)
		}
		if !sameGenesisAssetRegistry(g.AssetRegistry, g.State.AssetRegistry) {
			return fmt.Errorf("FRESH exported asset registry differs from audit asset registry")
		}
		if err := g.validateAuditManagementState(); err != nil {
			return err
		}
	}
	if g.State == nil && g.RestartHeight != 0 {
		return fmt.Errorf("FRESH initial genesis must not set restart height")
	}
	key := g.InitialAuditKey
	if key.Epoch == 0 || key.Suite != 2 || len(key.KeyID) != 32 || len(key.PublicKey) != 64 || len(key.PoP) != 96 {
		return fmt.Errorf("invalid initial audit key")
	}
	return validateGenesisAssetRegistryV1(g.AssetRegistry)
}

func (g GenesisStateV4) validateAuditManagementState() error {
	if len(g.AuditKeyHistory) == 0 || g.ActiveAuditEpoch == 0 {
		return fmt.Errorf("FRESH exported audit key history and active epoch are required")
	}
	seen := make(map[uint64]struct{}, len(g.AuditKeyHistory))
	for _, key := range g.AuditKeyHistory {
		if key == nil || key.Epoch == 0 || key.Suite != 2 || len(key.KeyID) != 32 || len(key.PublicKey) != 64 || len(key.PoP) != 96 || key.Origin == nil || key.Origin.Kind < 1 || key.Origin.Kind > 3 || len(key.Origin.Anchor) != 32 || key.Origin.Height != key.RegisteredHeight {
			return fmt.Errorf("invalid exported audit key history")
		}
		if _, exists := seen[key.Epoch]; exists {
			return fmt.Errorf("duplicate exported audit key epoch")
		}
		seen[key.Epoch] = struct{}{}
	}
	if _, ok := seen[g.ActiveAuditEpoch]; !ok {
		return fmt.Errorf("exported active audit epoch is absent from history")
	}
	if g.PendingAuditKey != nil {
		if g.PendingAuditKey.Epoch == 0 || g.PendingAuditKey.ActivationHeight == 0 {
			return fmt.Errorf("invalid exported pending audit key")
		}
		if _, ok := seen[g.PendingAuditKey.Epoch]; !ok {
			return fmt.Errorf("exported pending audit key is absent from history")
		}
	}
	for _, cancellation := range g.AuditCancellations {
		if cancellation == nil || cancellation.Epoch == 0 || cancellation.Origin == nil || cancellation.Origin.Kind < 1 || cancellation.Origin.Kind > 2 || len(cancellation.Origin.Anchor) != 32 || cancellation.Origin.Height != cancellation.CancelledHeight {
			return fmt.Errorf("invalid exported audit cancellation")
		}
		if _, ok := seen[cancellation.Epoch]; !ok {
			return fmt.Errorf("exported audit cancellation is absent from history")
		}
	}
	return nil
}

func sameGenesisAssetRegistry(left, right []*AssetRegistryEntryV1) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] == nil || right[i] == nil || left[i].CanonicalDenom != right[i].CanonicalDenom || string(left[i].AssetId) != string(right[i].AssetId) {
			return false
		}
	}
	return true
}
