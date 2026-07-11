package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const (
	BatchJoinSplitV1MaxInputs  uint32 = 16
	BatchJoinSplitV1MaxOutputs uint32 = 32
	AuditKeyIDV1MaxBytes              = 64

	BatchTransferIntentV1DomainLabel     = "clairveil.batch-transfer-intent.v1"
	BatchEffectV1ByteDomain              = "clairveil.batch-effect.v1"
	BatchUserDisclosureV2DomainLabel     = "clairveil.user-disclosure.v2"
	BatchUserDisclosureLeafV1DomainLabel = "clairveil.user-disclosure-leaf.v1"
	BatchFullDisclosureV2DomainLabel     = "clairveil.full-disclosure.v2"
)

// ValidateAuditKeyIDV1 freezes the bounded identifier carried by the future
// batch message and duplicated into its typed scan records. IDs are canonical
// lowercase ASCII: the first byte is [a-z0-9], and remaining bytes may also
// contain '.', '_' or '-'.
func ValidateAuditKeyIDV1(value string) error {
	if len(value) == 0 || len(value) > AuditKeyIDV1MaxBytes {
		return fmt.Errorf("audit key id must contain 1..%d bytes", AuditKeyIDV1MaxBytes)
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		alphaNumeric := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		if alphaNumeric || (i > 0 && (ch == '.' || ch == '_' || ch == '-')) {
			continue
		}
		return fmt.Errorf("audit key id must use canonical lowercase ASCII [a-z0-9][a-z0-9._-]*")
	}
	return nil
}

// BatchPublicInputOrderV1 is the consensus-visible order reserved for the
// future production BatchJoinSplit16x32 circuit. Session 2 freezes the schema;
// it does not register a production batch circuit or verifier artifact.
var BatchPublicInputOrderV1 = [...]string{
	"MerkleRoot",
	"ChainDomainHi",
	"ChainDomainLo",
	"ExpiresAtUnix",
	"InputCount",
	"OutputCount",
	"NullifierRoot",
	"CommitmentRoot",
	"UserDisclosureRoot",
	"FullDisclosureRoot",
	"PayloadDigestHi",
	"PayloadDigestLo",
}

type BatchVectorKindV1 string

const (
	BatchVectorNullifierV1      BatchVectorKindV1 = "nullifier"
	BatchVectorCommitmentV1     BatchVectorKindV1 = "commitment"
	BatchVectorUserDisclosureV1 BatchVectorKindV1 = "user_disclosure"
	BatchVectorFullDisclosureV1 BatchVectorKindV1 = "full_disclosure"
)

func (k BatchVectorKindV1) Capacity() (uint32, error) {
	switch k {
	case BatchVectorNullifierV1:
		return BatchJoinSplitV1MaxInputs, nil
	case BatchVectorCommitmentV1, BatchVectorUserDisclosureV1, BatchVectorFullDisclosureV1:
		return BatchJoinSplitV1MaxOutputs, nil
	default:
		return 0, fmt.Errorf("unsupported batch vector kind %q", k)
	}
}

func (k BatchVectorKindV1) domainLabel(part string) (string, error) {
	if _, err := k.Capacity(); err != nil {
		return "", err
	}
	switch part {
	case "leaf", "node", "root":
		return fmt.Sprintf("clairveil.batch-vector.%s.%s.v1", k, part), nil
	default:
		return "", fmt.Errorf("unsupported batch vector domain part %q", part)
	}
}

// ComputeBatchVectorRootV1 commits to an exact-capacity vector. Active slots
// are the prefix [0,count); every disabled value must be the canonical zero
// sentinel. This makes count, order, vector type, and enabled state explicit.
func ComputeBatchVectorRootV1(kind BatchVectorKindV1, count uint32, values []*big.Int) (*big.Int, error) {
	capacity, err := kind.Capacity()
	if err != nil {
		return nil, err
	}
	if count > capacity {
		return nil, fmt.Errorf("%s vector count %d exceeds capacity %d", kind, count, capacity)
	}
	if count == 0 {
		return nil, fmt.Errorf("%s vector count must be in [1,%d]", kind, capacity)
	}
	if len(values) != int(capacity) {
		return nil, fmt.Errorf("%s vector must contain exactly %d values, got %d", kind, capacity, len(values))
	}

	leafLabel, _ := kind.domainLabel("leaf")
	nodeLabel, _ := kind.domainLabel("node")
	rootLabel, _ := kind.domainLabel("root")
	leafDomain := DomainFieldV1(leafLabel)
	nodeDomain := DomainFieldV1(nodeLabel)
	rootDomain := DomainFieldV1(rootLabel)

	layer := make([]*big.Int, capacity)
	for i := uint32(0); i < capacity; i++ {
		value := values[i]
		if err := validateIntentFieldElement(fmt.Sprintf("%s vector value %d", kind, i), value); err != nil {
			return nil, err
		}
		enabled := i < count
		if !enabled && value.Sign() != 0 {
			return nil, fmt.Errorf("%s vector disabled value %d must be zero", kind, i)
		}
		if enabled && value.Sign() == 0 {
			return nil, fmt.Errorf("%s vector active value %d must be non-zero", kind, i)
		}
		enabledField := big.NewInt(0)
		if enabled {
			enabledField = big.NewInt(1)
		}
		layer[i] = privacycrypto.MimcHash(leafDomain, new(big.Int).SetUint64(uint64(i)), enabledField, value)
	}

	for level := uint32(0); len(layer) > 1; level++ {
		next := make([]*big.Int, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = privacycrypto.MimcHash(nodeDomain, new(big.Int).SetUint64(uint64(level)), layer[i], layer[i+1])
		}
		layer = next
	}

	return privacycrypto.MimcHash(
		rootDomain,
		new(big.Int).SetUint64(uint64(capacity)),
		new(big.Int).SetUint64(uint64(count)),
		layer[0],
	), nil
}

// ComputeBatchUserDisclosureVectorRootV1 applies the explicit per-output
// user-leaf contract before the generic fixed-capacity vector tree:
//
//	user_value_i = MiMC(USER_DISCLOSURE_LEAF_V1_DOMAIN,
//	  i, enabled_i, policy_i, raw_user_digest_i)
//
// Disabled slots require policy=0 and raw digest=0 and use the canonical zero
// value for the outer vector leaf. Active all-private outputs use policy=0 and
// raw digest=0 but still produce a non-zero, enabled user value.
func ComputeBatchUserDisclosureVectorRootV1(count uint32, policies []uint32, rawDigests []*big.Int) (*big.Int, error) {
	capacity := BatchJoinSplitV1MaxOutputs
	if count == 0 || count > capacity {
		return nil, fmt.Errorf("user disclosure vector count must be in [1,%d]", capacity)
	}
	if len(policies) != int(capacity) || len(rawDigests) != int(capacity) {
		return nil, fmt.Errorf("user disclosure vector requires exactly %d policies and raw digests", capacity)
	}
	values := make([]*big.Int, capacity)
	domain := DomainFieldV1(BatchUserDisclosureLeafV1DomainLabel)
	for i := uint32(0); i < capacity; i++ {
		raw := rawDigests[i]
		if err := validateIntentFieldElement(fmt.Sprintf("user disclosure raw digest %d", i), raw); err != nil {
			return nil, err
		}
		policy := policies[i]
		if policy > TransferPrivacyPolicyDiscloseAmountToFrom {
			return nil, fmt.Errorf("user disclosure policy %d at output %d exceeds 3-bit policy", policy, i)
		}
		if i >= count {
			if policy != TransferPrivacyPolicyAllPrivate || raw.Sign() != 0 {
				return nil, fmt.Errorf("disabled user disclosure output %d must use zero policy and raw digest", i)
			}
			values[i] = new(big.Int)
			continue
		}
		if policy == TransferPrivacyPolicyAllPrivate && raw.Sign() != 0 {
			return nil, fmt.Errorf("all-private output %d must use zero raw user disclosure digest", i)
		}
		if policy != TransferPrivacyPolicyAllPrivate && raw.Sign() == 0 {
			return nil, fmt.Errorf("disclosed output %d must use a non-zero raw user disclosure digest", i)
		}
		values[i] = privacycrypto.MimcHash(
			domain,
			new(big.Int).SetUint64(uint64(i)),
			big.NewInt(1),
			new(big.Int).SetUint64(uint64(policy)),
			raw,
		)
		if values[i].Sign() == 0 {
			return nil, fmt.Errorf("active user disclosure value %d must be non-zero", i)
		}
	}
	return ComputeBatchVectorRootV1(BatchVectorUserDisclosureV1, count, values)
}

// BatchUserDisclosureV1Input fixes the exact order of selected fields. A
// bitmap bit that is not enabled must have corresponding selected values set
// to zero by the caller/circuit.
type BatchUserDisclosureV1Input struct {
	OutputIndex            uint32
	Commitment             *big.Int
	Policy                 uint32
	DisclosedFieldBitmap   uint32
	SelectedAmount         *big.Int
	SelectedFromSpendKeyX  *big.Int
	SelectedFromSpendKeyY  *big.Int
	SelectedFromViewKeyX   *big.Int
	SelectedFromViewKeyY   *big.Int
	SelectedToSpendKeyX    *big.Int
	SelectedToSpendKeyY    *big.Int
	SelectedToViewKeyX     *big.Int
	SelectedToViewKeyY     *big.Int
	AssetID                *big.Int
	UserDisclosureBlinding *big.Int
}

func ComputeBatchUserDisclosureDigestV1(input BatchUserDisclosureV1Input) (*big.Int, error) {
	fields := []struct {
		name  string
		value *big.Int
	}{
		{"commitment", input.Commitment},
		{"selected amount", input.SelectedAmount},
		{"selected from spend key x", input.SelectedFromSpendKeyX},
		{"selected from spend key y", input.SelectedFromSpendKeyY},
		{"selected from view key x", input.SelectedFromViewKeyX},
		{"selected from view key y", input.SelectedFromViewKeyY},
		{"selected to spend key x", input.SelectedToSpendKeyX},
		{"selected to spend key y", input.SelectedToSpendKeyY},
		{"selected to view key x", input.SelectedToViewKeyX},
		{"selected to view key y", input.SelectedToViewKeyY},
		{"asset id", input.AssetID},
	}
	for _, field := range fields {
		if err := validateIntentFieldElement(field.name, field.value); err != nil {
			return nil, err
		}
	}
	if input.Commitment.Sign() == 0 {
		return nil, fmt.Errorf("commitment must be non-zero")
	}

	if input.Policy == TransferPrivacyPolicyAllPrivate {
		if input.DisclosedFieldBitmap != 0 || !allZeroBatchUserSelectedFields(input) {
			return nil, fmt.Errorf("all-private disclosure must use a zero bitmap and zero selected fields")
		}
		if input.UserDisclosureBlinding != nil && input.UserDisclosureBlinding.Sign() != 0 {
			return nil, fmt.Errorf("all-private disclosure must use the zero blinding sentinel")
		}
		return big.NewInt(0), nil
	}
	if input.Policy > TransferPrivacyPolicyDiscloseAmountToFrom {
		return nil, fmt.Errorf("unsupported batch user disclosure policy %d", input.Policy)
	}
	if input.DisclosedFieldBitmap != input.Policy {
		return nil, fmt.Errorf("disclosed field bitmap %d must equal policy %d in v1", input.DisclosedFieldBitmap, input.Policy)
	}
	if input.AssetID.Sign() == 0 {
		return nil, fmt.Errorf("user disclosure asset ID must be non-zero")
	}
	if input.Policy&TransferPrivacyPolicyDiscloseAmount == 0 {
		if input.SelectedAmount.Sign() != 0 {
			return nil, fmt.Errorf("undisclosed amount must use zero sentinel")
		}
	}
	if input.Policy&TransferPrivacyPolicyDiscloseFrom == 0 {
		if !allZeroBigInts(
			input.SelectedFromSpendKeyX, input.SelectedFromSpendKeyY,
			input.SelectedFromViewKeyX, input.SelectedFromViewKeyY,
		) {
			return nil, fmt.Errorf("undisclosed sender keys must use zero sentinels")
		}
	} else if err := validateDisclosureKeyBundleV1(
		"selected sender",
		input.SelectedFromSpendKeyX, input.SelectedFromSpendKeyY,
		input.SelectedFromViewKeyX, input.SelectedFromViewKeyY,
	); err != nil {
		return nil, err
	}
	if input.Policy&TransferPrivacyPolicyDiscloseTo == 0 {
		if !allZeroBigInts(
			input.SelectedToSpendKeyX, input.SelectedToSpendKeyY,
			input.SelectedToViewKeyX, input.SelectedToViewKeyY,
		) {
			return nil, fmt.Errorf("undisclosed recipient keys must use zero sentinels")
		}
	} else if err := validateDisclosureKeyBundleV1(
		"selected recipient",
		input.SelectedToSpendKeyX, input.SelectedToSpendKeyY,
		input.SelectedToViewKeyX, input.SelectedToViewKeyY,
	); err != nil {
		return nil, err
	}
	if err := validateNonZeroBatchBlinding("user disclosure blinding", input.UserDisclosureBlinding); err != nil {
		return nil, err
	}

	return privacycrypto.MimcHash(
		DomainFieldV1(BatchUserDisclosureV2DomainLabel),
		new(big.Int).SetUint64(uint64(input.OutputIndex)),
		input.Commitment,
		new(big.Int).SetUint64(uint64(input.Policy)),
		new(big.Int).SetUint64(uint64(input.DisclosedFieldBitmap)),
		input.SelectedAmount,
		input.SelectedFromSpendKeyX,
		input.SelectedFromSpendKeyY,
		input.SelectedFromViewKeyX,
		input.SelectedFromViewKeyY,
		input.SelectedToSpendKeyX,
		input.SelectedToSpendKeyY,
		input.SelectedToViewKeyX,
		input.SelectedToViewKeyY,
		input.AssetID,
		input.UserDisclosureBlinding,
	), nil
}

func allZeroBatchUserSelectedFields(input BatchUserDisclosureV1Input) bool {
	values := []*big.Int{
		input.SelectedAmount,
		input.SelectedFromSpendKeyX, input.SelectedFromSpendKeyY,
		input.SelectedFromViewKeyX, input.SelectedFromViewKeyY,
		input.SelectedToSpendKeyX, input.SelectedToSpendKeyY,
		input.SelectedToViewKeyX, input.SelectedToViewKeyY,
		input.AssetID,
	}
	for _, value := range values {
		if value == nil || value.Sign() != 0 {
			return false
		}
	}
	return true
}

type BatchFullDisclosureV1Input struct {
	OutputIndex            uint32
	Commitment             *big.Int
	Amount                 *big.Int
	AssetID                *big.Int
	SenderSpendKeyX        *big.Int
	SenderSpendKeyY        *big.Int
	SenderViewKeyX         *big.Int
	SenderViewKeyY         *big.Int
	RecipientSpendKeyX     *big.Int
	RecipientSpendKeyY     *big.Int
	RecipientViewKeyX      *big.Int
	RecipientViewKeyY      *big.Int
	FullDisclosureBlinding *big.Int
}

func ComputeBatchFullDisclosureDigestV1(input BatchFullDisclosureV1Input) (*big.Int, error) {
	fields := []struct {
		name  string
		value *big.Int
	}{
		{"commitment", input.Commitment},
		{"amount", input.Amount},
		{"asset id", input.AssetID},
		{"sender spend key x", input.SenderSpendKeyX},
		{"sender spend key y", input.SenderSpendKeyY},
		{"sender view key x", input.SenderViewKeyX},
		{"sender view key y", input.SenderViewKeyY},
		{"recipient spend key x", input.RecipientSpendKeyX},
		{"recipient spend key y", input.RecipientSpendKeyY},
		{"recipient view key x", input.RecipientViewKeyX},
		{"recipient view key y", input.RecipientViewKeyY},
	}
	for _, field := range fields {
		if err := validateIntentFieldElement(field.name, field.value); err != nil {
			return nil, err
		}
	}
	if input.Commitment.Sign() == 0 {
		return nil, fmt.Errorf("commitment must be non-zero")
	}
	if input.AssetID.Sign() == 0 {
		return nil, fmt.Errorf("full disclosure asset ID must be non-zero")
	}
	if err := ValidateShieldedAmount("full disclosure amount", input.Amount); err != nil {
		return nil, err
	}
	if err := validateDisclosureKeyBundleV1(
		"full disclosure sender",
		input.SenderSpendKeyX, input.SenderSpendKeyY, input.SenderViewKeyX, input.SenderViewKeyY,
	); err != nil {
		return nil, err
	}
	if err := validateDisclosureKeyBundleV1(
		"full disclosure recipient",
		input.RecipientSpendKeyX, input.RecipientSpendKeyY, input.RecipientViewKeyX, input.RecipientViewKeyY,
	); err != nil {
		return nil, err
	}
	if err := validateNonZeroBatchBlinding("full disclosure blinding", input.FullDisclosureBlinding); err != nil {
		return nil, err
	}

	return privacycrypto.MimcHash(
		DomainFieldV1(BatchFullDisclosureV2DomainLabel),
		new(big.Int).SetUint64(uint64(input.OutputIndex)),
		input.Commitment,
		input.Amount,
		input.AssetID,
		input.SenderSpendKeyX,
		input.SenderSpendKeyY,
		input.SenderViewKeyX,
		input.SenderViewKeyY,
		input.RecipientSpendKeyX,
		input.RecipientSpendKeyY,
		input.RecipientViewKeyX,
		input.RecipientViewKeyY,
		input.FullDisclosureBlinding,
	), nil
}

func validateNonZeroBatchBlinding(name string, value *big.Int) error {
	if err := validateIntentFieldElement(name, value); err != nil {
		return err
	}
	if value.Sign() == 0 {
		return fmt.Errorf("%s must be non-zero", name)
	}
	return nil
}

type BatchTransferIntentV1Input struct {
	ChainDomainHi      *big.Int
	ChainDomainLo      *big.Int
	MerkleRoot         *big.Int
	InputCount         uint32
	OutputCount        uint32
	AssetID            *big.Int
	NullifierRoot      *big.Int
	CommitmentRoot     *big.Int
	UserDisclosureRoot *big.Int
	FullDisclosureRoot *big.Int
	PayloadDigestHi    *big.Int
	PayloadDigestLo    *big.Int
	ExpiresAtUnix      int64
}

func ComputeBatchTransferIntentV1(input BatchTransferIntentV1Input) (*big.Int, error) {
	if err := ValidateBatchJoinSplitCountsV1(input.InputCount, input.OutputCount); err != nil {
		return nil, err
	}
	fields := []struct {
		name  string
		value *big.Int
	}{
		{"chain domain hi", input.ChainDomainHi}, {"chain domain lo", input.ChainDomainLo},
		{"merkle root", input.MerkleRoot}, {"asset id", input.AssetID},
		{"nullifier root", input.NullifierRoot}, {"commitment root", input.CommitmentRoot},
		{"user disclosure root", input.UserDisclosureRoot}, {"full disclosure root", input.FullDisclosureRoot},
		{"payload digest hi", input.PayloadDigestHi}, {"payload digest lo", input.PayloadDigestLo},
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
	return privacycrypto.MimcHash(
		DomainFieldV1(BatchTransferIntentV1DomainLabel),
		input.ChainDomainHi, input.ChainDomainLo,
		DomainFieldV1("clairveil.batch-joinsplit-16x32.v1"),
		input.MerkleRoot,
		new(big.Int).SetUint64(uint64(input.InputCount)),
		new(big.Int).SetUint64(uint64(input.OutputCount)),
		input.AssetID,
		input.NullifierRoot, input.CommitmentRoot,
		input.UserDisclosureRoot, input.FullDisclosureRoot,
		input.PayloadDigestHi, input.PayloadDigestLo,
		big.NewInt(input.ExpiresAtUnix),
	), nil
}

func ValidateBatchJoinSplitCountsV1(inputCount, outputCount uint32) error {
	if inputCount == 0 || inputCount > BatchJoinSplitV1MaxInputs {
		return fmt.Errorf("input count must be in 1..%d", BatchJoinSplitV1MaxInputs)
	}
	if outputCount == 0 || outputCount > BatchJoinSplitV1MaxOutputs {
		return fmt.Errorf("output count must be in 1..%d", BatchJoinSplitV1MaxOutputs)
	}
	return nil
}

type BatchEffectIDV1Input struct {
	ChainDomainHi      *big.Int
	ChainDomainLo      *big.Int
	MerkleRoot         *big.Int
	InputCount         uint32
	OutputCount        uint32
	NullifierRoot      *big.Int
	CommitmentRoot     *big.Int
	UserDisclosureRoot *big.Int
	FullDisclosureRoot *big.Int
	PayloadDigestHi    *big.Int
	PayloadDigestLo    *big.Int
	ExpiresAtUnix      int64
}

// ComputeBatchEffectIDV1 uses fixed 32-byte canonical field encodings, u32be
// counts, and u64be expiry. Proof, creator, and tx hash are deliberately absent.
func ComputeBatchEffectIDV1(input BatchEffectIDV1Input) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if err := ValidateBatchJoinSplitCountsV1(input.InputCount, input.OutputCount); err != nil {
		return zero, err
	}
	fields := []struct {
		name  string
		value *big.Int
	}{
		{"chain domain hi", input.ChainDomainHi}, {"chain domain lo", input.ChainDomainLo},
		{"merkle root", input.MerkleRoot}, {"nullifier root", input.NullifierRoot},
		{"commitment root", input.CommitmentRoot}, {"user disclosure root", input.UserDisclosureRoot},
		{"full disclosure root", input.FullDisclosureRoot}, {"payload digest hi", input.PayloadDigestHi},
		{"payload digest lo", input.PayloadDigestLo},
	}
	for _, field := range fields {
		if err := validateIntentFieldElement(field.name, field.value); err != nil {
			return zero, err
		}
	}
	if !fitsUnsignedBits(input.ChainDomainHi, 128) || !fitsUnsignedBits(input.ChainDomainLo, 128) ||
		!fitsUnsignedBits(input.PayloadDigestHi, 128) || !fitsUnsignedBits(input.PayloadDigestLo, 128) {
		return zero, fmt.Errorf("chain and payload digest limbs must be unsigned 128-bit integers")
	}
	if input.ExpiresAtUnix <= 0 {
		return zero, fmt.Errorf("expires_at_unix must be positive")
	}

	var encoded bytes.Buffer
	encoded.WriteString(BatchEffectV1ByteDomain)
	for _, value := range []*big.Int{input.ChainDomainHi, input.ChainDomainLo, input.MerkleRoot} {
		writeBatchField32(&encoded, value)
	}
	writeBatchUint32(&encoded, input.InputCount)
	writeBatchUint32(&encoded, input.OutputCount)
	for _, value := range []*big.Int{
		input.NullifierRoot, input.CommitmentRoot, input.UserDisclosureRoot,
		input.FullDisclosureRoot, input.PayloadDigestHi, input.PayloadDigestLo,
	} {
		writeBatchField32(&encoded, value)
	}
	var expiry [8]byte
	binary.BigEndian.PutUint64(expiry[:], uint64(input.ExpiresAtUnix))
	encoded.Write(expiry[:])
	return sha256.Sum256(encoded.Bytes()), nil
}

func writeBatchField32(dst *bytes.Buffer, value *big.Int) {
	dst.Write(value.FillBytes(make([]byte, 32)))
}

func writeBatchUint32(dst *bytes.Buffer, value uint32) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	dst.Write(encoded[:])
}
