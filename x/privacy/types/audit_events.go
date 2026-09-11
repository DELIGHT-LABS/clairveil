package types

// Lifecycle events invalidate prepared proofs without deleting wallet notes.
const (
	EventTypeAuditKeyScheduled  = "audit_key_scheduled"
	EventTypeAuditKeyCancelled  = "audit_key_cancelled"
	EventTypeAuditKeyActivated  = "audit_key_activated"
	EventTypePrivacyHaltChanged = "privacy_halt_changed"
)
