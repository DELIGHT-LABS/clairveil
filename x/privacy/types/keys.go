package types

import (
	"encoding/binary"
	"strconv"
)

const (
	// ModuleName defines the module name
	ModuleName = "privacy"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_privacy"
)

const (
	// Store key prefixes.
	KeyPrefixNullifier       = 0x01
	KeyPrefixMerkleNode      = 0x02
	KeyPrefixHistoricalRoot  = 0x03
	KeyPrefixCommitmentIdx   = 0x04
	KeyPrefixAuditConfig     = 0x05
	KeyPrefixPrivacyEvent    = 0x06
	KeyPrefixPrivacyEventSeq = 0x07
	KeyPrefixReserveDeposit  = 0x08
	KeyPrefixReserveWithdraw = 0x09
	KeyPrefixCircuitIdentity = 0x0a
	KeyPrefixAssetByDenom    = 0x0b
	KeyPrefixAssetByID       = 0x0c
	KeyPrefixScanSummary     = 0x0d
	KeyPrefixScanOutput      = 0x0e
	KeyPrefixRootSnapshot    = 0x0f
	KeyPrefixScanSequence    = 0x10
	KeyPrefixAuditKeyID      = 0x11
	KeyPrefixAuditKeyEpoch   = 0x12
)

// Event types and attribute keys emitted by the module.
const (
	EventTypeDeposit          = "deposit"
	EventTypeWithdraw         = "withdraw"
	EventTypeShieldedTransfer = "shielded_transfer"
	// EventTypeBatchTransferV1 identifies typed batch-transfer state and events.
	EventTypeBatchTransferV1 = "batch_transfer"

	AttributeKeyCreator                              = "creator"
	AttributeKeyCommitment                           = "commitment"
	AttributeKeyNullifier                            = "nullifier"
	AttributeKeyEncryptedNote                        = "encrypted_note"
	AttributeKeyNullifier1                           = "nullifier_1"
	AttributeKeyNullifier2                           = "nullifier_2"
	AttributeKeyCommitment1                          = "commitment_1"
	AttributeKeyCommitment2                          = "commitment_2"
	AttributeKeyCipherText1                          = "cipher_text_1"
	AttributeKeyCipherText2                          = "cipher_text_2"
	AttributeKeyViewTag1                             = "view_tag_1"
	AttributeKeyViewTag2                             = "view_tag_2"
	AttributeKeyRelayer                              = "relayer"
	AttributeKeyUserPrivacyPolicy                    = "user_privacy_policy"
	AttributeKeyUserDisclosureDigest                 = "user_disclosure_digest"
	AttributeKeyUserDisclosureMode                   = "user_disclosure_mode"
	AttributeKeyUserDisclosureTargetPubKey           = "user_disclosure_target_pubkey"
	AttributeKeyUserDisclosurePayload                = "user_disclosure_payload"
	AttributeKeyAuditDisclosureDigest                = "audit_disclosure_digest"
	AttributeKeyAuditDisclosureTargetPubKey          = "audit_disclosure_target_pubkey"
	AttributeKeyAuditDisclosurePayload               = "audit_disclosure_payload"
	AttributeKeySelfViewDisclosureDigest             = "self_view_disclosure_digest"
	AttributeKeySelfViewDisclosurePayload            = "self_view_disclosure_payload"
	AttributeKeyDisclosureEnvelopeCount              = "disclosure_envelope_count"
	AttributeKeyDisclosureEnvelopeKindPrefix         = "disclosure_envelope_kind_"
	AttributeKeyDisclosureEnvelopeTargetPubKeyPrefix = "disclosure_envelope_target_pubkey_"
	AttributeKeyDisclosureEnvelopePayloadPrefix      = "disclosure_envelope_payload_"
	AttributeKeyDisclosureEnvelopeLabelPrefix        = "disclosure_envelope_label_"
)

const (
	ScanFormatVersion = uint32(1)
	ViewTagVersion    = uint32(1)
	ViewTagLength     = 2
)

var auditConfigStoreKey = []byte{KeyPrefixAuditConfig}
var auditKeyIDStoreKey = []byte{KeyPrefixAuditKeyID}
var auditKeyEpochStoreKey = []byte{KeyPrefixAuditKeyEpoch}
var privacyEventSequenceStoreKey = []byte{KeyPrefixPrivacyEventSeq}
var circuitIdentityStoreKey = []byte{KeyPrefixCircuitIdentity}

func DisclosureEnvelopeKindAttributeKey(index int) string {
	return AttributeKeyDisclosureEnvelopeKindPrefix + strconv.Itoa(index)
}

func DisclosureEnvelopeTargetPubKeyAttributeKey(index int) string {
	return AttributeKeyDisclosureEnvelopeTargetPubKeyPrefix + strconv.Itoa(index)
}

func DisclosureEnvelopePayloadAttributeKey(index int) string {
	return AttributeKeyDisclosureEnvelopePayloadPrefix + strconv.Itoa(index)
}

func DisclosureEnvelopeLabelAttributeKey(index int) string {
	return AttributeKeyDisclosureEnvelopeLabelPrefix + strconv.Itoa(index)
}

// GetMerkleNodeKey returns the store key for a Merkle node.
func GetMerkleNodeKey(level uint8, index uint64) []byte {
	key := make([]byte, 10)

	key[0] = KeyPrefixMerkleNode
	key[1] = level
	binary.BigEndian.PutUint64(key[2:], index)

	return key
}

// GetHistoricalRootKey returns the store key for a historical root.
func GetHistoricalRootKey(root []byte) []byte {
	return append([]byte{KeyPrefixHistoricalRoot}, root...)
}

// GetNullifierKey returns the store key for a nullifier.
func GetNullifierKey(nullifier []byte) []byte {
	return append([]byte{KeyPrefixNullifier}, nullifier...)
}

func GetCommitmentIndexKey(commitment []byte) []byte {
	return append([]byte{KeyPrefixCommitmentIdx}, commitment...)
}

func GetAuditConfigKey() []byte {
	return append([]byte(nil), auditConfigStoreKey...)
}

func GetAuditKeyIDKey() []byte {
	return append([]byte(nil), auditKeyIDStoreKey...)
}

func GetAuditKeyEpochKey() []byte {
	return append([]byte(nil), auditKeyEpochStoreKey...)
}

func GetCircuitIdentityKey() []byte {
	return append([]byte(nil), circuitIdentityStoreKey...)
}

func GetPrivacyEventKey(height int64, sequence uint64) []byte {
	key := make([]byte, 17)
	key[0] = KeyPrefixPrivacyEvent
	binary.BigEndian.PutUint64(key[1:9], uint64(height))
	binary.BigEndian.PutUint64(key[9:17], sequence)
	return key
}

func GetPrivacyEventStartKey(height int64) []byte {
	if height < 0 {
		height = 0
	}
	return GetPrivacyEventKey(height, 0)
}

func GetPrivacyEventPrefix() []byte {
	return []byte{KeyPrefixPrivacyEvent}
}

func GetPrivacyEventSequenceKey() []byte {
	return append([]byte(nil), privacyEventSequenceStoreKey...)
}

// GetPrivacyGlobalSequenceKey is the consensus sequence shared by every
// privacy operation. GetPrivacyEventSequenceKey remains as a compatibility
// alias for callers compiled against the legacy event-sequence name.
func GetPrivacyGlobalSequenceKey() []byte {
	return append([]byte(nil), privacyEventSequenceStoreKey...)
}

func GetAssetByDenomKey(canonicalDenom string) []byte {
	return append([]byte{KeyPrefixAssetByDenom}, []byte(canonicalDenom)...)
}

func GetAssetByDenomPrefix() []byte {
	return []byte{KeyPrefixAssetByDenom}
}

func GetAssetByIDKey(assetID []byte) []byte {
	return append([]byte{KeyPrefixAssetByID}, assetID...)
}

func GetAssetByIDPrefix() []byte {
	return []byte{KeyPrefixAssetByID}
}

func GetPrivacyScanSummaryKey(height int64, globalSequence uint64) []byte {
	key := make([]byte, 17)
	key[0] = KeyPrefixScanSummary
	binary.BigEndian.PutUint64(key[1:9], uint64(height))
	binary.BigEndian.PutUint64(key[9:17], globalSequence)
	return key
}

func GetPrivacyScanSummaryPrefix() []byte {
	return []byte{KeyPrefixScanSummary}
}

func GetPrivacyScanOutputKey(height int64, globalSequence uint64, outputIndex uint32) []byte {
	key := make([]byte, 21)
	key[0] = KeyPrefixScanOutput
	binary.BigEndian.PutUint64(key[1:9], uint64(height))
	binary.BigEndian.PutUint64(key[9:17], globalSequence)
	binary.BigEndian.PutUint32(key[17:21], outputIndex)
	return key
}

func GetPrivacyScanOutputEventPrefix(height int64, globalSequence uint64) []byte {
	key := GetPrivacyScanOutputKey(height, globalSequence, 0)
	return key[:17]
}

func GetPrivacyScanOutputPrefix() []byte {
	return []byte{KeyPrefixScanOutput}
}

func GetPrivacyScanSequenceKey(globalSequence uint64) []byte {
	key := make([]byte, 9)
	key[0] = KeyPrefixScanSequence
	binary.BigEndian.PutUint64(key[1:], globalSequence)
	return key
}

func GetMerkleRootSnapshotKey(root []byte) []byte {
	return append([]byte{KeyPrefixRootSnapshot}, root...)
}

func GetMerkleRootSnapshotPrefix() []byte {
	return []byte{KeyPrefixRootSnapshot}
}

func GetReserveDepositKey(denom string) []byte {
	return append([]byte{KeyPrefixReserveDeposit}, []byte(denom)...)
}

func GetReserveWithdrawKey(denom string) []byte {
	return append([]byte{KeyPrefixReserveWithdraw}, []byte(denom)...)
}
