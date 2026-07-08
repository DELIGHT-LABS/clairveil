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
	ReservationID         string
	CompanyID             string
	PayrollID             string
	BatchID               string
	ChunkID               string
	ItemID                string
	NoteID                string
	OwnerKeyID            string
	EncryptedNullifier    []byte
	NullifierLookupKey    string
	NullifierLookupKeyID  string
	Status                ReservationStatus
	ExpiresAt             time.Time
	LeaseOwner            string
	LeaseToken            string
	LeaseUntil            time.Time
	LastHeartbeatAt       time.Time
	OperationID           string
	SignDocHash           string
	TxBytesHash           string
	TxHash                string
	AccountSequence       uint64
	BroadcastAttemptCount int
	LastBroadcastAt       time.Time
	LastBroadcastError    string
	CreatedAt             time.Time
	UpdatedAt             time.Time
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
}

type BroadcastAttemptUpdate struct {
	TxHash             string
	TxBytesHash        string
	SignDocHash        string
	AccountSequence    uint64
	LastBroadcastError string
}

type SubmittedReservationRef struct {
	ReservationID string
	LeaseToken    string
}

type ProofReadyOperationUpdate struct {
	OperationID                      string
	ExpectedOutputCommitment         string
	ExpectedDisclosureDigest         string
	ExpectedUserDisclosureDigest     string
	ExpectedAuditDisclosureDigest    string
	ExpectedSelfViewDisclosureDigest string
}

type ReservationFilter struct {
	Statuses []ReservationStatus
	Limit    int
}

func (r NoteReservation) ActiveKey() string {
	return r.OwnerKeyID + "\x00" + r.NullifierLookupKey
}
