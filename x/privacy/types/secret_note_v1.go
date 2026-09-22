package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/amount"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

// SecretNoteV1 is the wallet-side representation of a decrypted NoteV1.
// It deliberately keeps every private field in the fixed-field crypto type.
// Convert it to a circuit witness only at the explicitly named prover boundary.
type SecretNoteV1 struct {
	ReceiverSpendPubKeyX privacycrypto.FieldValue
	ReceiverSpendPubKeyY privacycrypto.FieldValue
	ReceiverViewPubKeyX  privacycrypto.FieldValue
	ReceiverViewPubKeyY  privacycrypto.FieldValue
	Amount               amount.Amount128
	AssetID              privacycrypto.FieldValue
	Randomness           privacycrypto.FieldValue
	Memo                 string
}

// Automatic formatting/serialization must not disclose amount, memo, or
// private preimages. Wallet and prover exports use explicit transport codecs.
func (SecretNoteV1) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("types.SecretNoteV1(<redacted>)"))
}
func (SecretNoteV1) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("secret note serialization is disabled")
}
func (SecretNoteV1) MarshalText() ([]byte, error) {
	return nil, fmt.Errorf("secret note serialization is disabled")
}
func (SecretDisclosurePlaintextV1) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("types.SecretDisclosurePlaintextV1(<redacted>)"))
}
func (SecretDisclosurePlaintextV1) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("secret disclosure serialization is disabled")
}
func (SecretDisclosurePlaintextV1) MarshalText() ([]byte, error) {
	return nil, fmt.Errorf("secret disclosure serialization is disabled")
}

func (note SecretNoteV1) CommitmentV1() (privacycrypto.FieldValue, error) {
	return SecretNoteCommitmentV1(note)
}

func (note SecretNoteV1) NullifierV1() (privacycrypto.FieldValue, error) {
	commitment, err := SecretNoteCommitmentV1(note)
	if err != nil {
		return privacycrypto.FieldValue{}, err
	}
	return SecretNoteNullifierV1(note, commitment)
}

func (note SecretNoteV1) AssetIDHex() string {
	assetID := note.AssetID.Bytes()
	return hex.EncodeToString(assetID[:])
}

// ComputeSecretAssetIDV1 derives the NoteV1 asset field directly in the
// fixed backend for wallet-side denom selection.
func ComputeSecretAssetIDV1(canonicalDenom string) privacycrypto.FieldValue {
	h := sha256.New()
	_, _ = h.Write([]byte(AssetIDV1ByteDomain))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(canonicalDenom)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(canonicalDenom))
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return privacycrypto.ReduceFieldBytes32(digest)
}

// NewSecretNoteV1 constructs a wallet note from already-typed private
// material. Randomness must come from the fixed-field wallet RNG; this
// constructor intentionally does not accept legacy-integer compatibility values.
func NewSecretNoteV1(
	spendX, spendY, viewX, viewY privacycrypto.FieldValue,
	amount amount.Amount128,
	assetID, randomness privacycrypto.FieldValue,
	memo string,
) (*SecretNoteV1, error) {
	note := SecretNoteV1{
		ReceiverSpendPubKeyX: spendX, ReceiverSpendPubKeyY: spendY,
		ReceiverViewPubKeyX: viewX, ReceiverViewPubKeyY: viewY,
		Amount: amount, AssetID: assetID, Randomness: randomness, Memo: memo,
	}
	if err := note.ValidateV1(); err != nil {
		return nil, err
	}
	return &note, nil
}

// ValidateV1 checks the fixed wallet representation and its active C/N
// values. It is safe for callers that receive typed notes from private
// storage: every check remains on FieldValue and no prover conversion occurs.
func (note SecretNoteV1) ValidateV1() error {
	memo := note.Memo
	if !utf8.ValidString(memo) {
		return fmt.Errorf("note memo must be valid UTF-8")
	}
	if len([]byte(memo)) > NoteMemoCapacityV1 {
		return fmt.Errorf("note memo exceeds fixed capacity %d", NoteMemoCapacityV1)
	}
	for _, pointFields := range [][2]privacycrypto.FieldValue{{note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY}, {note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY}} {
		var encoded [64]byte
		x, y := pointFields[0].Bytes(), pointFields[1].Bytes()
		copy(encoded[:32], x[:])
		copy(encoded[32:], y[:])
		if _, err := privacycrypto.ParseSecretPoint64(encoded[:]); err != nil {
			return fmt.Errorf("invalid receiver public key: %w", err)
		}
	}
	commitment, err := SecretNoteCommitmentV1(note)
	if err != nil {
		return err
	}
	nullifier, err := SecretNoteNullifierV1(note, commitment)
	if err != nil {
		return err
	}
	commitmentBytes, nullifierBytes := commitment.Bytes(), nullifier.Bytes()
	if allZeroBytes(commitmentBytes[:]) || allZeroBytes(nullifierBytes[:]) {
		return fmt.Errorf("secret note commitment/nullifier must be nonzero")
	}
	return nil
}

// NewRandomSecretNoteV1 samples canonical Fr randomness directly into the
// fixed transport. Rejection only depends on a fresh independent draw. It
// additionally rejects the negligible C/N zero cases required for an active
// note without exposing a legacy-integer fallback.
func NewRandomSecretNoteV1(
	reader io.Reader,
	spendX, spendY, viewX, viewY privacycrypto.FieldValue,
	amount amount.Amount128,
	assetID privacycrypto.FieldValue,
	memo string,
) (*SecretNoteV1, error) {
	if reader == nil {
		return nil, fmt.Errorf("note randomness reader is required")
	}
	for {
		var raw [32]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return nil, fmt.Errorf("sample note randomness: %w", err)
		}
		raw[0] &= 0x3f // Match the legacy 254-bit Fr rejection sampler.
		randomness, err := privacycrypto.ParseFieldValueBE32(raw[:])
		clear(raw[:])
		if err != nil {
			continue
		}
		note := &SecretNoteV1{ReceiverSpendPubKeyX: spendX, ReceiverSpendPubKeyY: spendY, ReceiverViewPubKeyX: viewX, ReceiverViewPubKeyY: viewY, Amount: amount, AssetID: assetID, Randomness: randomness, Memo: memo}
		commitment, err := SecretNoteCommitmentV1(*note)
		if err != nil {
			return nil, err
		}
		nullifier, err := SecretNoteNullifierV1(*note, commitment)
		if err != nil {
			return nil, err
		}
		commitmentBytes, nullifierBytes := commitment.Bytes(), nullifier.Bytes()
		if !allZeroBytes(commitmentBytes[:]) && !allZeroBytes(nullifierBytes[:]) {
			if err := note.ValidateV1(); err != nil {
				return nil, err
			}
			return note, nil
		}
	}
}

// SecretNoteCommitmentV1 computes the legacy NoteV1 commitment without
// materialising any private hash input as legacy-integer package or a gnark field element.
func SecretNoteCommitmentV1(note SecretNoteV1) (privacycrypto.FieldValue, error) {
	return privacycrypto.LegacyMiMCHash(
		DomainFieldValueV1(NoteCommitmentV1FieldDomain),
		note.ReceiverSpendPubKeyX,
		note.ReceiverSpendPubKeyY,
		note.ReceiverViewPubKeyX,
		note.ReceiverViewPubKeyY,
		Amount128FieldValue(note.Amount),
		note.AssetID,
		note.Randomness,
	)
}

// SecretNoteNullifierV1 computes the legacy NoteV1 nullifier in the fixed
// backend. commitment is an already-public digest but remains FieldValue so
// the secret hash call does not cross a legacy-integer compatibility shim.
func SecretNoteNullifierV1(note SecretNoteV1, commitment privacycrypto.FieldValue) (privacycrypto.FieldValue, error) {
	return privacycrypto.LegacyMiMCHash(
		DomainFieldValueV1(NoteNullifierV1FieldDomain),
		commitment,
		note.Randomness,
		note.ReceiverSpendPubKeyX,
		note.ReceiverSpendPubKeyY,
	)
}

// SecretTransferDisclosureV1Input is the fixed-field preimage for the 2x2
// user disclosure digest. Selected fields that the policy hides are supplied
// by the caller but are masked here before hashing.
type SecretTransferDisclosureV1Input struct {
	Policy      uint32
	OutputIndex uint32
	Commitment  privacycrypto.FieldValue
	Amount      amount.Amount128
	AssetID     privacycrypto.FieldValue

	FromSpendPubKeyX privacycrypto.FieldValue
	FromSpendPubKeyY privacycrypto.FieldValue
	FromViewPubKeyX  privacycrypto.FieldValue
	FromViewPubKeyY  privacycrypto.FieldValue
	ToSpendPubKeyX   privacycrypto.FieldValue
	ToSpendPubKeyY   privacycrypto.FieldValue
	ToViewPubKeyX    privacycrypto.FieldValue
	ToViewPubKeyY    privacycrypto.FieldValue
	Blinding         privacycrypto.FieldValue
}

func SecretTransferDisclosureDigestV1(input SecretTransferDisclosureV1Input) (privacycrypto.FieldValue, error) {
	if err := validateTransferDisclosurePolicy(input.Policy); err != nil {
		return privacycrypto.FieldValue{}, err
	}
	zero := privacycrypto.FieldValueFromUint64(0)
	if input.Policy == TransferPrivacyPolicyAllPrivate {
		return zero, nil
	}
	if input.AssetID.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("user disclosure asset id must be non-zero")
	}
	if input.Blinding.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("user disclosure blinding must be non-zero")
	}

	amount := zero
	if input.Policy&TransferPrivacyPolicyDiscloseAmount != 0 {
		amount = Amount128FieldValue(input.Amount)
	}
	fromSpendX, fromSpendY, fromViewX, fromViewY := zero, zero, zero, zero
	if input.Policy&TransferPrivacyPolicyDiscloseFrom != 0 {
		fromSpendX, fromSpendY = input.FromSpendPubKeyX, input.FromSpendPubKeyY
		fromViewX, fromViewY = input.FromViewPubKeyX, input.FromViewPubKeyY
	}
	toSpendX, toSpendY, toViewX, toViewY := zero, zero, zero, zero
	if input.Policy&TransferPrivacyPolicyDiscloseTo != 0 {
		toSpendX, toSpendY = input.ToSpendPubKeyX, input.ToSpendPubKeyY
		toViewX, toViewY = input.ToViewPubKeyX, input.ToViewPubKeyY
	}

	return privacycrypto.LegacyMiMCHash(
		privacycrypto.HashStringFieldValue(TransferUserDisclosureV2FieldDomain),
		privacycrypto.FieldValueFromUint64(uint64(input.Policy)),
		privacycrypto.FieldValueFromUint64(uint64(input.OutputIndex)),
		input.Commitment, amount, input.AssetID,
		fromSpendX, fromSpendY, fromViewX, fromViewY,
		toSpendX, toSpendY, toViewX, toViewY, input.Blinding,
	)
}

// SecretFullTransferDisclosureV1Input is shared by audit and self-view
// disclosure. They intentionally use the same full digest.
type SecretFullTransferDisclosureV1Input struct {
	OutputIndex uint32
	Commitment  privacycrypto.FieldValue
	Amount      amount.Amount128
	AssetID     privacycrypto.FieldValue

	FromSpendPubKeyX privacycrypto.FieldValue
	FromSpendPubKeyY privacycrypto.FieldValue
	FromViewPubKeyX  privacycrypto.FieldValue
	FromViewPubKeyY  privacycrypto.FieldValue
	ToSpendPubKeyX   privacycrypto.FieldValue
	ToSpendPubKeyY   privacycrypto.FieldValue
	ToViewPubKeyX    privacycrypto.FieldValue
	ToViewPubKeyY    privacycrypto.FieldValue
	Blinding         privacycrypto.FieldValue
}

func SecretFullTransferDisclosureDigestV1(input SecretFullTransferDisclosureV1Input) (privacycrypto.FieldValue, error) {
	if input.Blinding.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("full disclosure blinding must be non-zero")
	}
	return privacycrypto.LegacyMiMCHash(
		privacycrypto.HashStringFieldValue(TransferFullDisclosureV2FieldDomain),
		privacycrypto.FieldValueFromUint64(uint64(TransferAuditDisclosureDomain)),
		privacycrypto.FieldValueFromUint64(uint64(input.OutputIndex)),
		input.Commitment,
		Amount128FieldValue(input.Amount), input.AssetID,
		input.FromSpendPubKeyX, input.FromSpendPubKeyY, input.FromViewPubKeyX, input.FromViewPubKeyY,
		input.ToSpendPubKeyX, input.ToSpendPubKeyY, input.ToViewPubKeyX, input.ToViewPubKeyY,
		input.Blinding,
	)
}

type SecretBatchUserDisclosureV1Input struct {
	OutputIndex          uint32
	Commitment           privacycrypto.FieldValue
	Policy               uint32
	DisclosedFieldBitmap uint32
	SelectedAmount       amount.Amount128
	SelectedFromSpendX   privacycrypto.FieldValue
	SelectedFromSpendY   privacycrypto.FieldValue
	SelectedFromViewX    privacycrypto.FieldValue
	SelectedFromViewY    privacycrypto.FieldValue
	SelectedToSpendX     privacycrypto.FieldValue
	SelectedToSpendY     privacycrypto.FieldValue
	SelectedToViewX      privacycrypto.FieldValue
	SelectedToViewY      privacycrypto.FieldValue
	AssetID              privacycrypto.FieldValue
	Blinding             privacycrypto.FieldValue
}

func SecretBatchUserDisclosureDigestV1(input SecretBatchUserDisclosureV1Input) (privacycrypto.FieldValue, error) {
	if input.Commitment.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("commitment must be non-zero")
	}
	if input.Policy > TransferPrivacyPolicyDiscloseAmountToFrom {
		return privacycrypto.FieldValue{}, fmt.Errorf("unsupported batch user disclosure policy %d", input.Policy)
	}
	if input.Policy == TransferPrivacyPolicyAllPrivate {
		if input.DisclosedFieldBitmap != 0 || !allZeroSecretBatchUserSelectedFields(input) {
			return privacycrypto.FieldValue{}, fmt.Errorf("all-private disclosure must use a zero bitmap and zero selected fields")
		}
		if !input.Blinding.IsZero() {
			return privacycrypto.FieldValue{}, fmt.Errorf("all-private disclosure must use the zero blinding sentinel")
		}
		return privacycrypto.FieldValueFromUint64(0), nil
	}
	if input.DisclosedFieldBitmap != input.Policy {
		return privacycrypto.FieldValue{}, fmt.Errorf("disclosed field bitmap %d must equal policy %d in v1", input.DisclosedFieldBitmap, input.Policy)
	}
	if input.AssetID.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("user disclosure asset ID must be non-zero")
	}
	if input.Policy&TransferPrivacyPolicyDiscloseAmount == 0 && !input.SelectedAmount.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("undisclosed amount must use zero sentinel")
	}
	if err := validateSecretBatchDisclosureBundle(
		"sender", "selected sender", input.Policy&TransferPrivacyPolicyDiscloseFrom != 0,
		input.SelectedFromSpendX, input.SelectedFromSpendY, input.SelectedFromViewX, input.SelectedFromViewY,
	); err != nil {
		return privacycrypto.FieldValue{}, err
	}
	if err := validateSecretBatchDisclosureBundle(
		"recipient", "selected recipient", input.Policy&TransferPrivacyPolicyDiscloseTo != 0,
		input.SelectedToSpendX, input.SelectedToSpendY, input.SelectedToViewX, input.SelectedToViewY,
	); err != nil {
		return privacycrypto.FieldValue{}, err
	}
	if input.Blinding.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("user disclosure blinding must be non-zero")
	}
	return privacycrypto.LegacyMiMCHash(
		DomainFieldValueV1(BatchUserDisclosureV2DomainLabel),
		privacycrypto.FieldValueFromUint64(uint64(input.OutputIndex)), input.Commitment,
		privacycrypto.FieldValueFromUint64(uint64(input.Policy)), privacycrypto.FieldValueFromUint64(uint64(input.DisclosedFieldBitmap)),
		Amount128FieldValue(input.SelectedAmount),
		input.SelectedFromSpendX, input.SelectedFromSpendY, input.SelectedFromViewX, input.SelectedFromViewY,
		input.SelectedToSpendX, input.SelectedToSpendY, input.SelectedToViewX, input.SelectedToViewY,
		input.AssetID, input.Blinding,
	)
}

type SecretBatchFullDisclosureV1Input struct {
	OutputIndex     uint32
	Commitment      privacycrypto.FieldValue
	Amount          amount.Amount128
	AssetID         privacycrypto.FieldValue
	SenderSpendX    privacycrypto.FieldValue
	SenderSpendY    privacycrypto.FieldValue
	SenderViewX     privacycrypto.FieldValue
	SenderViewY     privacycrypto.FieldValue
	RecipientSpendX privacycrypto.FieldValue
	RecipientSpendY privacycrypto.FieldValue
	RecipientViewX  privacycrypto.FieldValue
	RecipientViewY  privacycrypto.FieldValue
	Blinding        privacycrypto.FieldValue
}

// SecretDisclosurePlaintextV1 is the fixed-field representation of a
// decrypted user, audit or self-view disclosure payload. It keeps the legacy
// bytes unchanged while avoiding a transient legacy-integer preimage in scan code.
type SecretDisclosurePlaintextV1 struct {
	Plane                DisclosurePlaneV1
	OutputIndex          uint32
	Policy               uint32
	DisclosedFieldBitmap uint32
	Commitment           privacycrypto.FieldValue
	Amount               amount.Amount128
	AssetID              privacycrypto.FieldValue
	SenderSpendKeyX      privacycrypto.FieldValue
	SenderSpendKeyY      privacycrypto.FieldValue
	SenderViewKeyX       privacycrypto.FieldValue
	SenderViewKeyY       privacycrypto.FieldValue
	RecipientSpendKeyX   privacycrypto.FieldValue
	RecipientSpendKeyY   privacycrypto.FieldValue
	RecipientViewKeyX    privacycrypto.FieldValue
	RecipientViewKeyY    privacycrypto.FieldValue
	DisclosureBlinding   privacycrypto.FieldValue
}

// MarshalSecretDisclosurePlaintextV1 preserves the established disclosure
// wire using fixed field bytes. Caller-side policy validation belongs to the
// typed user/full digest builders, which consume this same structure.
func MarshalSecretDisclosurePlaintextV1(payload *SecretDisclosurePlaintextV1) ([]byte, error) {
	if err := validateSecretDisclosurePlaintextV1(payload); err != nil {
		return nil, err
	}
	result := make([]byte, DisclosurePlaintextV1Size)
	offset := 0
	offset = putFixedBytes(result, offset, disclosurePlaintextV1DomainTag[:])
	binary.BigEndian.PutUint16(result[offset:offset+2], fixedBinaryVersionV1)
	offset += 2
	result[offset] = byte(payload.Plane)
	offset += 2 // plane plus reserved byte
	binary.BigEndian.PutUint32(result[offset:offset+4], payload.OutputIndex)
	offset += 4
	binary.BigEndian.PutUint32(result[offset:offset+4], payload.Policy)
	offset += 4
	binary.BigEndian.PutUint32(result[offset:offset+4], payload.DisclosedFieldBitmap)
	offset += 4
	commitment := payload.Commitment.Bytes()
	offset = putFixedBytes(result, offset, commitment[:])
	amountBytes := payload.Amount.Bytes16()
	offset = putFixedBytes(result, offset, amountBytes[:])
	for _, field := range []privacycrypto.FieldValue{
		payload.AssetID,
		payload.SenderSpendKeyX, payload.SenderSpendKeyY,
		payload.SenderViewKeyX, payload.SenderViewKeyY,
		payload.RecipientSpendKeyX, payload.RecipientSpendKeyY,
		payload.RecipientViewKeyX, payload.RecipientViewKeyY,
		payload.DisclosureBlinding,
	} {
		encoded := field.Bytes()
		offset = putFixedBytes(result, offset, encoded[:])
	}
	if offset != len(result) {
		return nil, fmt.Errorf("internal DisclosurePlaintextV1 size mismatch: wrote %d, expected %d", offset, len(result))
	}
	return result, nil
}

func UnmarshalSecretDisclosurePlaintextV1(encoded []byte) (*SecretDisclosurePlaintextV1, error) {
	if len(encoded) != DisclosurePlaintextV1Size {
		return nil, fmt.Errorf("DisclosurePlaintextV1 must be exactly %d bytes, got %d", DisclosurePlaintextV1Size, len(encoded))
	}
	offset := 0
	if !bytes.Equal(encoded[offset:offset+16], disclosurePlaintextV1DomainTag[:]) {
		return nil, fmt.Errorf("invalid DisclosurePlaintextV1 domain tag")
	}
	offset += 16
	if version := binary.BigEndian.Uint16(encoded[offset : offset+2]); version != fixedBinaryVersionV1 {
		return nil, fmt.Errorf("unsupported DisclosurePlaintextV1 version %d", version)
	}
	offset += 2
	plane := DisclosurePlaneV1(encoded[offset])
	offset++
	if encoded[offset] != 0 {
		return nil, fmt.Errorf("DisclosurePlaintextV1 reserved byte must be zero")
	}
	offset++
	outputIndex := binary.BigEndian.Uint32(encoded[offset : offset+4])
	offset += 4
	policy := binary.BigEndian.Uint32(encoded[offset : offset+4])
	offset += 4
	bitmap := binary.BigEndian.Uint32(encoded[offset : offset+4])
	offset += 4
	commitment, err := privacycrypto.ParseFieldValueBE32(encoded[offset : offset+32])
	if err != nil {
		return nil, fmt.Errorf("disclosure commitment: %w", err)
	}
	offset += 32
	var amountBytes [16]byte
	copy(amountBytes[:], encoded[offset:offset+16])
	value := amount.FromBytes16(amountBytes)
	offset += 16
	fields := make([]privacycrypto.FieldValue, 0, 10)
	for i := 0; i < 10; i++ {
		field, err := privacycrypto.ParseFieldValueBE32(encoded[offset : offset+32])
		if err != nil {
			return nil, fmt.Errorf("disclosure field %d: %w", i, err)
		}
		fields = append(fields, field)
		offset += 32
	}
	if offset != len(encoded) {
		return nil, fmt.Errorf("DisclosurePlaintextV1 trailing bytes are not allowed")
	}
	payload := &SecretDisclosurePlaintextV1{
		Plane: plane, OutputIndex: outputIndex, Policy: policy, DisclosedFieldBitmap: bitmap, Commitment: commitment, Amount: value,
		AssetID: fields[0], SenderSpendKeyX: fields[1], SenderSpendKeyY: fields[2], SenderViewKeyX: fields[3], SenderViewKeyY: fields[4],
		RecipientSpendKeyX: fields[5], RecipientSpendKeyY: fields[6], RecipientViewKeyX: fields[7], RecipientViewKeyY: fields[8], DisclosureBlinding: fields[9],
	}
	if err := validateSecretDisclosurePlaintextV1(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (payload SecretDisclosurePlaintextV1) UserDigestInputV1() SecretBatchUserDisclosureV1Input {
	return SecretBatchUserDisclosureV1Input{
		OutputIndex: payload.OutputIndex, Commitment: payload.Commitment, Policy: payload.Policy, DisclosedFieldBitmap: payload.DisclosedFieldBitmap,
		SelectedAmount: payload.Amount, SelectedFromSpendX: payload.SenderSpendKeyX, SelectedFromSpendY: payload.SenderSpendKeyY,
		SelectedFromViewX: payload.SenderViewKeyX, SelectedFromViewY: payload.SenderViewKeyY,
		SelectedToSpendX: payload.RecipientSpendKeyX, SelectedToSpendY: payload.RecipientSpendKeyY,
		SelectedToViewX: payload.RecipientViewKeyX, SelectedToViewY: payload.RecipientViewKeyY,
		AssetID: payload.AssetID, Blinding: payload.DisclosureBlinding,
	}
}

func (payload SecretDisclosurePlaintextV1) FullDigestInputV1() SecretBatchFullDisclosureV1Input {
	return SecretBatchFullDisclosureV1Input{
		OutputIndex: payload.OutputIndex, Commitment: payload.Commitment, Amount: payload.Amount, AssetID: payload.AssetID,
		SenderSpendX: payload.SenderSpendKeyX, SenderSpendY: payload.SenderSpendKeyY, SenderViewX: payload.SenderViewKeyX, SenderViewY: payload.SenderViewKeyY,
		RecipientSpendX: payload.RecipientSpendKeyX, RecipientSpendY: payload.RecipientSpendKeyY, RecipientViewX: payload.RecipientViewKeyX, RecipientViewY: payload.RecipientViewKeyY,
		Blinding: payload.DisclosureBlinding,
	}
}

func SecretBatchFullDisclosureDigestV1(input SecretBatchFullDisclosureV1Input) (privacycrypto.FieldValue, error) {
	if input.Commitment.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("commitment must be non-zero")
	}
	if input.AssetID.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("full disclosure asset ID must be non-zero")
	}
	if err := validateSecretBatchDisclosureBundle(
		"full disclosure sender", "full disclosure sender", true,
		input.SenderSpendX, input.SenderSpendY, input.SenderViewX, input.SenderViewY,
	); err != nil {
		return privacycrypto.FieldValue{}, err
	}
	if err := validateSecretBatchDisclosureBundle(
		"full disclosure recipient", "full disclosure recipient", true,
		input.RecipientSpendX, input.RecipientSpendY, input.RecipientViewX, input.RecipientViewY,
	); err != nil {
		return privacycrypto.FieldValue{}, err
	}
	if input.Blinding.IsZero() {
		return privacycrypto.FieldValue{}, fmt.Errorf("full disclosure blinding must be non-zero")
	}
	return privacycrypto.LegacyMiMCHash(
		DomainFieldValueV1(BatchFullDisclosureV2DomainLabel),
		privacycrypto.FieldValueFromUint64(uint64(input.OutputIndex)), input.Commitment,
		Amount128FieldValue(input.Amount), input.AssetID,
		input.SenderSpendX, input.SenderSpendY, input.SenderViewX, input.SenderViewY,
		input.RecipientSpendX, input.RecipientSpendY, input.RecipientViewX, input.RecipientViewY,
		input.Blinding,
	)
}

func allZeroSecretBatchUserSelectedFields(input SecretBatchUserDisclosureV1Input) bool {
	if !input.SelectedAmount.IsZero() {
		return false
	}
	return allZeroSecretFields(
		input.SelectedFromSpendX, input.SelectedFromSpendY, input.SelectedFromViewX, input.SelectedFromViewY,
		input.SelectedToSpendX, input.SelectedToSpendY, input.SelectedToViewX, input.SelectedToViewY,
		input.AssetID,
	)
}

func validateSecretBatchDisclosureBundle(hiddenLabel, disclosedLabel string, disclosed bool, fields ...privacycrypto.FieldValue) error {
	if !disclosed {
		if !allZeroSecretFields(fields...) {
			return fmt.Errorf("undisclosed %s keys must use zero sentinels", hiddenLabel)
		}
		return nil
	}
	for i, keyKind := 0, "spend"; i < len(fields); i, keyKind = i+2, "view" {
		x, y := fields[i].Bytes(), fields[i+1].Bytes()
		var raw [64]byte
		copy(raw[:32], x[:])
		copy(raw[32:], y[:])
		if _, err := privacycrypto.ParseSecretPoint64(raw[:]); err != nil {
			return fmt.Errorf("invalid %s %s public key: %w", disclosedLabel, keyKind, err)
		}
	}
	return nil
}

func allZeroSecretFields(fields ...privacycrypto.FieldValue) bool {
	for _, field := range fields {
		if !field.IsZero() {
			return false
		}
	}
	return true
}

// DomainFieldValueV1 is the fixed-field form of DomainFieldV1. The SHA-256
// output is reduced in the native field backend; no secret input is routed
// through legacy-integer package.
func DomainFieldValueV1(label string) privacycrypto.FieldValue {
	h := sha256.New()
	_, _ = h.Write([]byte(FieldDomainV1ByteDomain))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(label)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(label))
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return privacycrypto.ReduceFieldBytes32(digest)
}

// MarshalSecretNotePlaintextV1 is the inverse wallet transport operation. It
// preserves the established recovery wire while keeping private fields out of
// legacy-integer package before encryption.
func MarshalSecretNotePlaintextV1(note *SecretNoteV1) ([]byte, error) {
	if note == nil {
		return nil, fmt.Errorf("note is required")
	}
	if err := note.ValidateV1(); err != nil {
		return nil, err
	}
	memo := []byte(note.Memo)
	result := make([]byte, NotePlaintextV1Size)
	offset := 0
	offset = putFixedBytes(result, offset, notePlaintextV1DomainTag[:])
	binary.BigEndian.PutUint16(result[offset:offset+2], fixedBinaryVersionV1)
	offset += 2
	offset += 2 // reserved flags
	for _, field := range []privacycrypto.FieldValue{
		note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY,
		note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY,
	} {
		encoded := field.Bytes()
		offset = putFixedBytes(result, offset, encoded[:])
	}
	amountBytes := note.Amount.Bytes16()
	offset = putFixedBytes(result, offset, amountBytes[:])
	for _, field := range []privacycrypto.FieldValue{note.AssetID, note.Randomness} {
		encoded := field.Bytes()
		offset = putFixedBytes(result, offset, encoded[:])
	}
	binary.BigEndian.PutUint16(result[offset:offset+2], uint16(len(memo)))
	offset += 2
	copy(result[offset:offset+NoteMemoCapacityV1], memo)
	offset += NoteMemoCapacityV1
	if offset != len(result) {
		return nil, fmt.Errorf("internal NotePlaintextV1 size mismatch: wrote %d, expected %d", offset, len(result))
	}
	return result, nil
}

// UnmarshalSecretNotePlaintextV1 decodes a recovery plaintext directly into
// fixed field values. It is the wallet transport entry point; callers that
// need a gnark witness must use the separately named prover adapter.
func UnmarshalSecretNotePlaintextV1(encoded []byte) (*SecretNoteV1, error) {
	if len(encoded) != NotePlaintextV1Size {
		return nil, fmt.Errorf("NotePlaintextV1 must be exactly %d bytes, got %d", NotePlaintextV1Size, len(encoded))
	}
	offset := 0
	if !bytes.Equal(encoded[offset:offset+16], notePlaintextV1DomainTag[:]) {
		return nil, fmt.Errorf("invalid NotePlaintextV1 domain tag")
	}
	offset += 16
	if version := binary.BigEndian.Uint16(encoded[offset : offset+2]); version != fixedBinaryVersionV1 {
		return nil, fmt.Errorf("unsupported NotePlaintextV1 version %d", version)
	}
	offset += 2
	if encoded[offset] != 0 || encoded[offset+1] != 0 {
		return nil, fmt.Errorf("NotePlaintextV1 reserved flags must be zero")
	}
	offset += 2
	fields := make([]privacycrypto.FieldValue, 0, 6)
	for i := 0; i < 4; i++ {
		field, err := privacycrypto.ParseFieldValueBE32(encoded[offset : offset+32])
		if err != nil {
			return nil, fmt.Errorf("note key coordinate %d: %w", i, err)
		}
		fields = append(fields, field)
		offset += 32
	}
	var amountBytes [16]byte
	copy(amountBytes[:], encoded[offset:offset+16])
	value := amount.FromBytes16(amountBytes)
	offset += 16
	assetID, err := privacycrypto.ParseFieldValueBE32(encoded[offset : offset+32])
	if err != nil {
		return nil, fmt.Errorf("note asset id: %w", err)
	}
	offset += 32
	randomness, err := privacycrypto.ParseFieldValueBE32(encoded[offset : offset+32])
	if err != nil {
		return nil, fmt.Errorf("note randomness: %w", err)
	}
	offset += 32
	memoLength := int(binary.BigEndian.Uint16(encoded[offset : offset+2]))
	offset += 2
	if memoLength > NoteMemoCapacityV1 {
		return nil, fmt.Errorf("NotePlaintextV1 memo length %d exceeds capacity", memoLength)
	}
	memoRegion := encoded[offset : offset+NoteMemoCapacityV1]
	if !allZeroBytes(memoRegion[memoLength:]) {
		return nil, fmt.Errorf("NotePlaintextV1 memo padding must be zero")
	}
	if !utf8.Valid(memoRegion[:memoLength]) {
		return nil, fmt.Errorf("NotePlaintextV1 memo must be valid UTF-8")
	}
	offset += NoteMemoCapacityV1
	if offset != len(encoded) {
		return nil, fmt.Errorf("NotePlaintextV1 trailing bytes are not allowed")
	}
	note := &SecretNoteV1{
		ReceiverSpendPubKeyX: fields[0], ReceiverSpendPubKeyY: fields[1],
		ReceiverViewPubKeyX: fields[2], ReceiverViewPubKeyY: fields[3],
		Amount: value, AssetID: assetID, Randomness: randomness,
		Memo: string(memoRegion[:memoLength]),
	}
	if err := note.ValidateV1(); err != nil {
		return nil, err
	}
	return note, nil
}

// validateSecretDisclosurePlaintextV1 mirrors the frozen V1 semantic checks
// without converting any decrypted field to a legacy integer.
func validateSecretDisclosurePlaintextV1(p *SecretDisclosurePlaintextV1) error {
	if p == nil {
		return fmt.Errorf("disclosure plaintext is required")
	}
	if p.Commitment.IsZero() {
		return fmt.Errorf("disclosure commitment must be non-zero")
	}
	if p.DisclosureBlinding.IsZero() {
		return fmt.Errorf("disclosure blinding must be non-zero")
	}
	if p.AssetID.IsZero() {
		return fmt.Errorf("disclosure asset id must be non-zero")
	}
	policy := p.Policy
	switch p.Plane {
	case DisclosurePlaneUserV1:
		if policy == 0 || policy > TransferPrivacyPolicyDiscloseAmountToFrom {
			return fmt.Errorf("user disclosure plaintext requires policy 1..7")
		}
		if p.DisclosedFieldBitmap != policy {
			return fmt.Errorf("user disclosure bitmap must equal policy in v1")
		}
		if policy&TransferPrivacyPolicyDiscloseAmount == 0 && !p.Amount.IsZero() {
			return fmt.Errorf("undisclosed amount must use zero sentinel")
		}
	case DisclosurePlaneFullV1:
		if policy != DisclosureFullMarkerV1 || p.DisclosedFieldBitmap != TransferPrivacyPolicyDiscloseAmountToFrom {
			return fmt.Errorf("full disclosure must use the full marker and bitmap")
		}
		policy = TransferPrivacyPolicyDiscloseAmountToFrom
	default:
		return fmt.Errorf("unsupported disclosure plane %d", p.Plane)
	}
	for _, bundle := range []struct {
		flag   uint32
		fields [4]privacycrypto.FieldValue
	}{
		{TransferPrivacyPolicyDiscloseFrom, [4]privacycrypto.FieldValue{p.SenderSpendKeyX, p.SenderSpendKeyY, p.SenderViewKeyX, p.SenderViewKeyY}},
		{TransferPrivacyPolicyDiscloseTo, [4]privacycrypto.FieldValue{p.RecipientSpendKeyX, p.RecipientSpendKeyY, p.RecipientViewKeyX, p.RecipientViewKeyY}},
	} {
		if policy&bundle.flag == 0 {
			valid := true
			for _, v := range bundle.fields {
				zero := v.IsZero()
				valid = zero && valid
			}
			if !valid {
				return fmt.Errorf("undisclosed keys must use zero sentinel")
			}
		} else {
			for i := 0; i < 4; i += 2 {
				x, y := bundle.fields[i].Bytes(), bundle.fields[i+1].Bytes()
				var raw [64]byte
				copy(raw[:32], x[:])
				copy(raw[32:], y[:])
				if _, err := privacycrypto.ParseSecretPoint64(raw[:]); err != nil {
					return fmt.Errorf("invalid disclosed key: %w", err)
				}
			}
		}
	}
	return nil
}

// Amount128FieldValue embeds the full amount without reduction or truncation.
func Amount128FieldValue(value amount.Amount128) privacycrypto.FieldValue {
	raw := value.Bytes32()
	field, err := privacycrypto.ParseFieldValueBE32(raw[:])
	if err != nil {
		panic("uint128 amount is outside the field")
	}
	return field
}
