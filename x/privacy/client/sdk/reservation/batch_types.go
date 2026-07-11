package reservation

import (
	"time"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const BatchOperationSchemaVersionV1 = "privacy-payroll-batch-v1"

type BatchOutputRole string

const (
	BatchOutputRolePayment BatchOutputRole = "payment"
	BatchOutputRoleChange  BatchOutputRole = "change"
	BatchOutputRolePadding BatchOutputRole = "padding"
)

type BatchItemEvidenceStatus string

const (
	BatchItemEvidencePending      BatchItemEvidenceStatus = "Pending"
	BatchItemEvidenceSucceeded    BatchItemEvidenceStatus = "Succeeded"
	BatchItemEvidenceManualReview BatchItemEvidenceStatus = "ManualReview"
	BatchItemEvidenceFailed       BatchItemEvidenceStatus = "Failed"
)

// BatchOperation is the durable one-proof unit. Input reservations and output
// items are stored as separate relations so note-spend state never implies an
// individual payroll item succeeded.
type BatchOperation struct {
	SchemaVersion             string
	OperationID               string
	CompanyID                 string
	PayrollID                 string
	BatchID                   string
	OwnerKeyID                string
	AssetID                   string
	Denom                     string
	InputCount                int
	OutputCount               int
	Status                    OperationStatus
	LeaseOwner                string
	LeaseToken                string
	LeaseUntil                time.Time
	LastHeartbeatAt           time.Time
	PreparedPayloadCiphertext []byte
	PreparedPayloadHash       string
	ProofCiphertext           []byte
	ProofHash                 string
	SignedTxBytesCiphertext   []byte
	TxBytesHash               string
	SignDocHash               string
	TxHash                    string
	AccountSequence           uint64
	BroadcastAttemptCount     int
	LastBroadcastAt           time.Time
	LastBroadcastError        string
	BroadcastHistory          []BatchBroadcastAttempt
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

type BatchBroadcastAttempt struct {
	SignedTxBytesCiphertext []byte
	TxBytesHash             string
	SignDocHash             string
	TxHash                  string
	AccountSequence         uint64
	BroadcastAt             time.Time
	BroadcastError          string
	Unknown                 bool
}

type OperationInputReservation struct {
	SchemaVersion   string
	OperationID     string
	ReservationID   string
	InputIndex      int
	Commitment      string
	EncryptedAmount []byte
	CreatedAt       time.Time
}

type PayrollItemOutput struct {
	SchemaVersion      string
	OperationID        string
	ItemID             string
	EmployeeID         string
	OutputIndex        int
	Role               BatchOutputRole
	EvidenceStatus     BatchItemEvidenceStatus
	ManualReviewReason string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ExpectedOutputEvidence struct {
	SchemaVersion          string
	OperationID            string
	OutputIndex            int
	Commitment             string
	UserPrivacyPolicy      uint32
	UserDisclosureMode     privacytypes.UserDisclosureMode
	UserDisclosureDigest   string
	FullDisclosureDigest   string
	RecipientHash          string
	EncryptedRecipient     []byte
	EncryptedAmount        []byte
	AmountHash             string
	Denom                  string
	AssetID                string
	Role                   BatchOutputRole
	AuditKeyID             string
	AuditKeyEpoch          uint64
	ObservedCommitment     string
	ObservedUserDigest     string
	ObservedFullDigest     string
	ObservedRecipientHash  string
	AuditDeliveryFailed    bool
	SelfViewDeliveryFailed bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type BatchOperationGraph struct {
	Operation BatchOperation
	Inputs    []OperationInputReservation
	Items     []PayrollItemOutput
	Evidence  []ExpectedOutputEvidence
}

type BatchProofArtifactUpdate struct {
	PreparedPayloadCiphertext []byte
	PreparedPayloadHash       string
	ProofCiphertext           []byte
	ProofHash                 string
}

// BatchSignedTxUpdate stages immutable signed bytes durably before the first
// network submission. This closes the crash window between signing and an
// ambiguous broadcast response.
type BatchSignedTxUpdate struct {
	SignedTxBytesCiphertext []byte
	TxBytesHash             string
	SignDocHash             string
	TxHash                  string
	AccountSequence         uint64
}

type BatchBroadcastUpdate struct {
	SignedTxBytesCiphertext []byte
	TxBytesHash             string
	SignDocHash             string
	TxHash                  string
	AccountSequence         uint64
	LastBroadcastError      string
	Unknown                 bool
}

type ObservedOutputEvidence struct {
	OutputIndex            int
	Commitment             string
	UserDisclosureDigest   string
	FullDisclosureDigest   string
	RecipientHash          string
	AuditDeliveryFailed    bool
	SelfViewDeliveryFailed bool
}

type BatchReconcileUpdate struct {
	TxHash              string
	TxSucceeded         bool
	TxFailed            bool
	SpentReservationIDs []string
	ObservedOutputs     []ObservedOutputEvidence
	FailureReason       string
}
