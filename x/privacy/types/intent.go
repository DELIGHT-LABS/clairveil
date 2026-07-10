package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const (
	ActiveCircuitSetID = "privacy-intent-v2"

	ChainDomainV1ByteDomain       = "clairveil.chain-domain.v1"
	TransferPayloadV1ByteDomain   = "clairveil.transfer-payload.v1"
	WithdrawRecipientV1ByteDomain = "clairveil.withdraw-recipient.v1"

	TransferIntentV2FieldDomain = "CLAIRVEIL_TRANSFER_INTENT_V2"
	SpendIntentV2FieldDomain    = "CLAIRVEIL_SPEND_INTENT_V2"
	NullifierSetV1FieldDomain   = "CLAIRVEIL_NULLIFIER_SET_V1"
	CommitmentSetV1FieldDomain  = "CLAIRVEIL_COMMITMENT_SET_V1"

	JoinSplit2x2V2CircuitKindFieldDomain = "CLAIRVEIL_JOINSPLIT_2X2_V2"
	SpendV2CircuitKindFieldDomain        = "CLAIRVEIL_SPEND_V2"

	CanonicalTransferPayloadFormatVersion uint32 = 1
)

// DigestLimbs is a SHA-256 digest split into two non-reduced, big-endian
// 128-bit integers that fit canonically in the BN254 scalar field.
type DigestLimbs struct {
	Hi *big.Int
	Lo *big.Int
}

type TransferIntentV2Input struct {
	ChainDomainHi        *big.Int
	ChainDomainLo        *big.Int
	MerkleRoot           *big.Int
	AssetID              *big.Int
	Nullifiers           [2]*big.Int
	Commitments          [2]*big.Int
	UserDisclosureDigest *big.Int
	FullDisclosureDigest *big.Int
	PayloadDigestHi      *big.Int
	PayloadDigestLo      *big.Int
	ExpiresAtUnix        int64
}

func SplitDigestToLimbs(digest [sha256.Size]byte) DigestLimbs {
	return DigestLimbs{
		Hi: new(big.Int).SetBytes(digest[:sha256.Size/2]),
		Lo: new(big.Int).SetBytes(digest[sha256.Size/2:]),
	}
}

func ComputeChainDomainV1(chainID, circuitSetID string) (DigestLimbs, error) {
	if chainID == "" {
		return DigestLimbs{}, fmt.Errorf("chain id is required")
	}
	if circuitSetID == "" {
		return DigestLimbs{}, fmt.Errorf("circuit set id is required")
	}

	var encoded bytes.Buffer
	encoded.WriteString(ChainDomainV1ByteDomain)
	if err := writeLengthPrefixedBytes(&encoded, []byte(chainID)); err != nil {
		return DigestLimbs{}, err
	}
	if err := writeLengthPrefixedBytes(&encoded, []byte(circuitSetID)); err != nil {
		return DigestLimbs{}, err
	}
	digest := sha256.Sum256(encoded.Bytes())
	return SplitDigestToLimbs(digest), nil
}

// CanonicalTransferPayloadBytesV1 encodes only transfer effects. It excludes
// creator, proof, transaction metadata, and the digest itself.
func CanonicalTransferPayloadBytesV1(msg *MsgTransfer) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("transfer message is required")
	}
	if msg.ExpiresAtUnix <= 0 {
		return nil, fmt.Errorf("expires_at_unix must be positive for transfer")
	}
	if msg.UserDisclosureMode < 0 {
		return nil, fmt.Errorf("user disclosure mode must not be negative")
	}

	var encoded bytes.Buffer
	writeUint32(&encoded, CanonicalTransferPayloadFormatVersion)
	if err := writeLengthPrefixedBytes(&encoded, msg.Root); err != nil {
		return nil, err
	}
	if err := writeByteSlice(&encoded, msg.Nullifiers); err != nil {
		return nil, err
	}
	if err := writeByteSlice(&encoded, msg.NewCommitments); err != nil {
		return nil, err
	}
	if err := writeByteSlice(&encoded, msg.CipherTexts); err != nil {
		return nil, err
	}
	if err := writeByteSlice(&encoded, msg.ViewTags); err != nil {
		return nil, err
	}
	writeUint32(&encoded, msg.UserPrivacyPolicy)
	writeUint32(&encoded, uint32(msg.UserDisclosureMode))
	for _, value := range [][]byte{
		msg.UserDisclosureDigest,
		msg.UserDisclosureTargetPubkey,
		msg.UserDisclosurePayload,
		msg.AuditDisclosureDigest,
		msg.AuditDisclosureTargetPubkey,
		msg.AuditDisclosurePayload,
		msg.SelfViewDisclosureDigest,
		msg.SelfViewDisclosurePayload,
	} {
		if err := writeLengthPrefixedBytes(&encoded, value); err != nil {
			return nil, err
		}
	}
	writeUint64(&encoded, uint64(msg.ExpiresAtUnix))
	return encoded.Bytes(), nil
}

func ComputeTransferPayloadDigestV1(msg *MsgTransfer) (DigestLimbs, error) {
	payload, err := CanonicalTransferPayloadBytesV1(msg)
	if err != nil {
		return DigestLimbs{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte(TransferPayloadV1ByteDomain))
	_, _ = h.Write(payload)
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return SplitDigestToLimbs(digest), nil
}

func ComputeOrderedSetDigestV1(domain string, values []*big.Int) (*big.Int, error) {
	if domain != NullifierSetV1FieldDomain && domain != CommitmentSetV1FieldDomain {
		return nil, fmt.Errorf("unsupported ordered set domain %q", domain)
	}
	if len(values) > math.MaxUint32 {
		return nil, fmt.Errorf("ordered set contains too many values")
	}
	inputs := make([]*big.Int, 0, len(values)+2)
	inputs = append(inputs, privacycrypto.HashString(domain), new(big.Int).SetUint64(uint64(len(values))))
	for i, value := range values {
		if err := validateIntentFieldElement(fmt.Sprintf("ordered set value %d", i), value); err != nil {
			return nil, err
		}
		inputs = append(inputs, value)
	}
	return privacycrypto.MimcHash(inputs...), nil
}

func ComputeTransferIntentV2(input TransferIntentV2Input) (*big.Int, error) {
	fields := []struct {
		name  string
		value *big.Int
	}{
		{"chain domain hi", input.ChainDomainHi},
		{"chain domain lo", input.ChainDomainLo},
		{"merkle root", input.MerkleRoot},
		{"asset id", input.AssetID},
		{"nullifier 0", input.Nullifiers[0]},
		{"nullifier 1", input.Nullifiers[1]},
		{"commitment 0", input.Commitments[0]},
		{"commitment 1", input.Commitments[1]},
		{"user disclosure digest", input.UserDisclosureDigest},
		{"full disclosure digest", input.FullDisclosureDigest},
		{"payload digest hi", input.PayloadDigestHi},
		{"payload digest lo", input.PayloadDigestLo},
	}
	for _, field := range fields {
		if err := validateIntentFieldElement(field.name, field.value); err != nil {
			return nil, err
		}
	}
	if !fitsUnsignedBits(input.ChainDomainHi, 128) || !fitsUnsignedBits(input.ChainDomainLo, 128) ||
		!fitsUnsignedBits(input.PayloadDigestHi, 128) || !fitsUnsignedBits(input.PayloadDigestLo, 128) {
		return nil, fmt.Errorf("chain and payload digest limbs must be unsigned 128-bit integers")
	}
	if input.ExpiresAtUnix <= 0 {
		return nil, fmt.Errorf("expires_at_unix must be positive")
	}

	nullifierDigest, err := ComputeOrderedSetDigestV1(NullifierSetV1FieldDomain, input.Nullifiers[:])
	if err != nil {
		return nil, err
	}
	commitmentDigest, err := ComputeOrderedSetDigestV1(CommitmentSetV1FieldDomain, input.Commitments[:])
	if err != nil {
		return nil, err
	}

	return privacycrypto.MimcHash(
		privacycrypto.HashString(TransferIntentV2FieldDomain),
		input.ChainDomainHi,
		input.ChainDomainLo,
		privacycrypto.HashString(JoinSplit2x2V2CircuitKindFieldDomain),
		input.MerkleRoot,
		big.NewInt(2),
		big.NewInt(2),
		input.AssetID,
		nullifierDigest,
		commitmentDigest,
		input.UserDisclosureDigest,
		input.FullDisclosureDigest,
		input.PayloadDigestHi,
		input.PayloadDigestLo,
		big.NewInt(input.ExpiresAtUnix),
	), nil
}

func validateIntentFieldElement(name string, value *big.Int) error {
	if value == nil {
		return fmt.Errorf("%s is required", name)
	}
	if value.Sign() < 0 || value.Cmp(fr.Modulus()) >= 0 {
		return fmt.Errorf("%s must be a canonical BN254 field element", name)
	}
	return nil
}

func fitsUnsignedBits(value *big.Int, bits int) bool {
	return value != nil && value.Sign() >= 0 && value.BitLen() <= bits
}

func writeByteSlice(dst *bytes.Buffer, values [][]byte) error {
	if len(values) > math.MaxUint32 {
		return fmt.Errorf("byte slice contains too many values")
	}
	writeUint32(dst, uint32(len(values)))
	for _, value := range values {
		if err := writeLengthPrefixedBytes(dst, value); err != nil {
			return err
		}
	}
	return nil
}

func writeLengthPrefixedBytes(dst *bytes.Buffer, value []byte) error {
	if len(value) > math.MaxUint32 {
		return fmt.Errorf("byte value is too large")
	}
	writeUint32(dst, uint32(len(value)))
	_, _ = dst.Write(value)
	return nil
}

func writeUint32(dst *bytes.Buffer, value uint32) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	_, _ = dst.Write(encoded[:])
}

func writeUint64(dst *bytes.Buffer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = dst.Write(encoded[:])
}
