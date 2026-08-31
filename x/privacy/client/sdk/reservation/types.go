package reservation

import "time"

type NoteInventory struct {
	NoteID               string
	Commitment           string
	EncryptedNullifier   []byte
	NullifierLookupKey   string
	NullifierLookupKeyID string
	AssetID              string
	EncryptedAmount      []byte
	OwnerKeyID           string
	MerklePosition       uint64
	DiscoveredHeight     int64
	SpendStatus          ReservationStatus
	ReservationID        string
	UpdatedAt            time.Time
}

type NoteReservation struct {
	ReservationID                 string
	CompanyID                     string
	PayrollID                     string
	BatchID                       string
	ChunkID                       string
	ItemID                        string
	NoteID                        string
	OwnerKeyID                    string
	EncryptedNullifier            []byte
	NullifierLookupKey            string
	NullifierLookupKeyID          string
	Status                        ReservationStatus
	ExpiresAt                     time.Time
	LeaseOwner                    string
	LeaseToken                    string
	LeaseUntil                    time.Time
	LastHeartbeatAt               time.Time
	OperationID                   string
	PayloadHash                   string
	SignDocHash                   string
	TxBytesHash                   string
	TxHash                        string
	AccountSequence               uint64
	BroadcastAttemptCount         int
	BroadcastInFlight             bool
	ProofDiscardInFlight          bool
	ProofDiscardStartedAt         time.Time
	LastBroadcastAt               time.Time
	LastBroadcastError            string
	RelayHandedOff                bool
	RelayHandedOffAt              time.Time
	ReconciliationReviewReason    string
	LastReconciledAt              time.Time
	ManualReviewResolvedBy        string
	ManualReviewApprovalReference string
	ManualReviewResolutionReason  string
	CreatedAt                     time.Time
	UpdatedAt                     time.Time
}

type PayrollOperation struct {
	OperationID                      string
	CompanyID                        string
	PayrollID                        string
	BatchID                          string
	ChunkID                          string
	ItemID                           string
	ReservationID                    string
	ExpectedOutputCommitment         string
	ExpectedDisclosureDigest         string
	ExpectedUserDisclosureDigest     string
	ExpectedAuditDisclosureDigest    string
	ExpectedSelfViewDisclosureDigest string
	ExpectedRecipientHash            string
	EncryptedExpectedRecipient       []byte
	EncryptedExpectedAmount          []byte
	ExpectedAmountHash               string
	ExpectedDenom                    string
	BatchItemIndex                   int
	BatchItemIndexKnown              bool
	SignDocHash                      string
	TxBytesHash                      string
	TxHash                           string
	Status                           OperationStatus
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	PayloadHash                      string
}

type Lease struct {
	Owner string
	Token string
	Until time.Time
}

type SubmittedReservationUpdate struct {
	TxHash          string
	TxBytesHash     string
	SignDocHash     string
	AccountSequence uint64
	// LastBroadcastError preserves a non-fatal post-boundary heartbeat failure
	// for reconciliation without changing a successfully submitted status.
	LastBroadcastError string
}

type BroadcastAttemptUpdate struct {
	TxHash             string
	TxBytesHash        string
	SignDocHash        string
	AccountSequence    uint64
	LastBroadcastError string
}

type BroadcastAttemptStart struct {
	Reason          string
	TxHash          string
	TxBytesHash     string
	SignDocHash     string
	AccountSequence uint64
}

// BroadcastAmbiguityUpdate records an RPC failure after crossing an external
// submission boundary when no durable transaction identity is available.
// The reservation must remain active for manual reconciliation, never retry.
type BroadcastAmbiguityUpdate struct {
	LastBroadcastError string
}

// ManualReviewResolution records the operator evidence that authorizes an
// otherwise unsafe transition out of ManualReview.
type ManualReviewResolution struct {
	Target            ReservationStatus
	OperatorID        string
	ApprovalReference string
	Reason            string
}

// ProofDiscardEvidence proves that a local ProofReady artifact was discarded
// before any broadcast attempt. It authorizes the worker-owned replan path.
type ProofDiscardEvidence struct {
	NoBroadcastAttempt bool
	ProofDiscarded     bool
}

type ReconciliationCommandKind string

const (
	ReconciliationCommandStandard                ReconciliationCommandKind = "standard"
	ReconciliationCommandQuarantineMatchingSpent ReconciliationCommandKind = "quarantine_matching_spent"
)

// ReconciliationTransition is an atomic reservation and operation update
// prepared by Service after it validates chain or operator evidence. Store
// implementations must reject commands that are not service-authorized.
//
// The authorization bit is deliberately private: callers outside this package
// can implement Store, but cannot manufacture an evidence-bypassing command.
type ReconciliationTransition struct {
	ReservationID          string
	From                   ReservationStatus
	To                     ReservationStatus
	Operation              *PayrollOperation
	ManualReviewResolution *ManualReviewResolution
	ProofDiscardEvidence   *ProofDiscardEvidence
	// SiblingOperationStatus applies to non-terminal operations attached to
	// reservations quarantined through the same spent-nullifier transaction.
	SiblingOperationStatus OperationStatus
	// AuditReason records newly observed conflicting chain evidence without
	// changing an already terminal reservation's outcome.
	AuditReason string
	// Transaction identity is accepted only on a Service-authorized
	// reconciliation command. It lets a crash-recovery worker move a durable
	// ProofReady broadcast attempt to Unknown without recreating the old lease.
	TxHash      string
	TxBytesHash string
	SignDocHash string
	// OperationReservationIDs is populated only by Service.ReconcileOperation.
	// The Store verifies this is the complete linked input set before it records
	// a multi-input operation success.
	OperationReservationIDs []string
	// OperationReservationFromStatuses lets a Service-authorized reconciliation
	// move a mixed, exact linked set to one fail-closed state atomically.
	OperationReservationFromStatuses map[string]ReservationStatus
	// OperationReservationRefs carries the owner/token capability for every
	// input when an operation-wide lifecycle command requires a live lease.
	OperationReservationRefs []SubmittedReservationRef
	LeaseOwner               string
	LeaseToken               string
	Now                      time.Time

	serviceAuthorized                 bool
	quarantineMatchingSpent           bool
	requireSingleReservationOperation bool
}

func (transition ReconciliationTransition) ServiceAuthorized() bool {
	return transition.serviceAuthorized
}

// CommandKind lets external Store implementations apply the same atomic
// reconciliation semantics without access to the service-only authorization bit.
func (transition ReconciliationTransition) CommandKind() ReconciliationCommandKind {
	if transition.quarantineMatchingSpent {
		return ReconciliationCommandQuarantineMatchingSpent
	}
	return ReconciliationCommandStandard
}

func (transition ReconciliationTransition) QuarantinesMatchingSpent() bool {
	return transition.CommandKind() == ReconciliationCommandQuarantineMatchingSpent
}

// RequiresSingleReservationOperation tells Store implementations to reject
// this command if another reservation is linked to the same operation. The
// check must be performed atomically with the transition.
func (transition ReconciliationTransition) RequiresSingleReservationOperation() bool {
	return transition.requireSingleReservationOperation
}

type SubmittedReservationRef struct {
	ReservationID string
	LeaseOwner    string
	LeaseToken    string
}

type ProofReadyOperationUpdate struct {
	OperationID                      string
	PayloadHash                      string
	SignDocHash                      string
	TxBytesHash                      string
	ExpectedOutputCommitment         string
	ExpectedDisclosureDigest         string
	ExpectedUserDisclosureDigest     string
	ExpectedAuditDisclosureDigest    string
	ExpectedSelfViewDisclosureDigest string
}

type ReservationFilter struct {
	Statuses    []ReservationStatus
	OperationID string
	Limit       int
}

func (r NoteReservation) ActiveKey() string {
	return r.OwnerKeyID + "\x00" + r.NullifierLookupKey
}
