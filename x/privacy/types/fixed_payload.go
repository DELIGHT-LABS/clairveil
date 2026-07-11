package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"unicode/utf8"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const (
	FixedPayloadVersionV1 = "privacy-fixed-v1"

	fixedBinaryVersionV1 uint16 = 1
	NoteMemoCapacityV1          = 128

	NotePlaintextV1Size           = 350
	DisclosurePlaintextV1Size     = 392
	EncryptedEnvelopeV1HeaderSize = 20

	symmetricEnvelopeOverheadV1 = 12 + 16
	eciesEnvelopeOverheadV1     = privacycrypto.CanonicalPointSize + 12 + 16
)

var (
	notePlaintextV1DomainTag       = fixedDomainTagV1("clairveil.note-plaintext.v1")
	disclosurePlaintextV1DomainTag = fixedDomainTagV1("clairveil.disclosure-plaintext.v1")
	encryptedEnvelopeV1DomainTag   = fixedDomainTagV1("clairveil.encrypted-envelope.v1")
)

type DisclosurePlaneV1 uint8

const (
	DisclosurePlaneUserV1  DisclosurePlaneV1 = 1
	DisclosurePlaneFullV1  DisclosurePlaneV1 = 2
	DisclosureFullMarkerV1 uint32            = math.MaxUint32
)

// DisclosurePlaintextV1 stores either selected user fields or the complete
// full-disclosure fields. Fields omitted by the user bitmap are exact zeros.
type DisclosurePlaintextV1 struct {
	Plane                DisclosurePlaneV1
	OutputIndex          uint32
	Policy               uint32
	DisclosedFieldBitmap uint32
	Commitment           *big.Int
	Amount               *big.Int
	AssetID              *big.Int
	SenderSpendKeyX      *big.Int
	SenderSpendKeyY      *big.Int
	SenderViewKeyX       *big.Int
	SenderViewKeyY       *big.Int
	RecipientSpendKeyX   *big.Int
	RecipientSpendKeyY   *big.Int
	RecipientViewKeyX    *big.Int
	RecipientViewKeyY    *big.Int
	DisclosureBlinding   *big.Int
}

type EncryptedEnvelopeKindV1 uint8

const (
	EnvelopeDepositNoteV1        EncryptedEnvelopeKindV1 = 1
	EnvelopeTransferNoteV1       EncryptedEnvelopeKindV1 = 2
	EnvelopeUserDisclosureV1     EncryptedEnvelopeKindV1 = 3
	EnvelopeAuditDisclosureV1    EncryptedEnvelopeKindV1 = 4
	EnvelopeSelfViewDisclosureV1 EncryptedEnvelopeKindV1 = 5
)

func MarshalNotePlaintextV1(note *Note) ([]byte, error) {
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
	// flags/reserved are already zero.
	offset += 2
	for _, field := range []*big.Int{
		note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY,
		note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY,
	} {
		offset = putFixedField(result, offset, field)
	}
	binary.BigEndian.PutUint64(result[offset:offset+8], note.Amount.Uint64())
	offset += 8
	offset = putFixedField(result, offset, note.AssetID)
	offset = putFixedField(result, offset, note.Randomness)
	binary.BigEndian.PutUint16(result[offset:offset+2], uint16(len(memo)))
	offset += 2
	copy(result[offset:offset+NoteMemoCapacityV1], memo)
	offset += NoteMemoCapacityV1
	if offset != len(result) {
		return nil, fmt.Errorf("internal NotePlaintextV1 size mismatch: wrote %d, expected %d", offset, len(result))
	}
	return result, nil
}

func UnmarshalNotePlaintextV1(encoded []byte) (*Note, error) {
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
	fields := make([]*big.Int, 0, 6)
	for i := 0; i < 4; i++ {
		value, next, err := readFixedField(encoded, offset, fmt.Sprintf("note key coordinate %d", i))
		if err != nil {
			return nil, err
		}
		fields = append(fields, value)
		offset = next
	}
	amount := new(big.Int).SetUint64(binary.BigEndian.Uint64(encoded[offset : offset+8]))
	offset += 8
	assetID, next, err := readFixedField(encoded, offset, "note asset id")
	if err != nil {
		return nil, err
	}
	offset = next
	randomness, next, err := readFixedField(encoded, offset, "note randomness")
	if err != nil {
		return nil, err
	}
	offset = next
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
	note := &Note{
		ReceiverSpendPubKeyX: fields[0], ReceiverSpendPubKeyY: fields[1],
		ReceiverViewPubKeyX: fields[2], ReceiverViewPubKeyY: fields[3],
		Amount: amount, AssetID: assetID, Randomness: randomness,
		Memo: string(memoRegion[:memoLength]),
	}
	if err := note.ValidateV1(); err != nil {
		return nil, fmt.Errorf("invalid NotePlaintextV1: %w", err)
	}
	return note, nil
}

func MarshalDisclosurePlaintextV1(payload *DisclosurePlaintextV1) ([]byte, error) {
	if err := validateDisclosurePlaintextV1(payload); err != nil {
		return nil, err
	}
	result := make([]byte, DisclosurePlaintextV1Size)
	offset := 0
	offset = putFixedBytes(result, offset, disclosurePlaintextV1DomainTag[:])
	binary.BigEndian.PutUint16(result[offset:offset+2], fixedBinaryVersionV1)
	offset += 2
	result[offset] = byte(payload.Plane)
	offset++
	// reserved byte is already zero.
	offset++
	binary.BigEndian.PutUint32(result[offset:offset+4], payload.OutputIndex)
	offset += 4
	binary.BigEndian.PutUint32(result[offset:offset+4], payload.Policy)
	offset += 4
	binary.BigEndian.PutUint32(result[offset:offset+4], payload.DisclosedFieldBitmap)
	offset += 4
	offset = putFixedField(result, offset, payload.Commitment)
	binary.BigEndian.PutUint64(result[offset:offset+8], payload.Amount.Uint64())
	offset += 8
	for _, field := range []*big.Int{
		payload.AssetID,
		payload.SenderSpendKeyX, payload.SenderSpendKeyY,
		payload.SenderViewKeyX, payload.SenderViewKeyY,
		payload.RecipientSpendKeyX, payload.RecipientSpendKeyY,
		payload.RecipientViewKeyX, payload.RecipientViewKeyY,
		payload.DisclosureBlinding,
	} {
		offset = putFixedField(result, offset, field)
	}
	if offset != len(result) {
		return nil, fmt.Errorf("internal DisclosurePlaintextV1 size mismatch: wrote %d, expected %d", offset, len(result))
	}
	return result, nil
}

func UnmarshalDisclosurePlaintextV1(encoded []byte) (*DisclosurePlaintextV1, error) {
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
	payload := &DisclosurePlaintextV1{Plane: plane}
	payload.OutputIndex = binary.BigEndian.Uint32(encoded[offset : offset+4])
	offset += 4
	payload.Policy = binary.BigEndian.Uint32(encoded[offset : offset+4])
	offset += 4
	payload.DisclosedFieldBitmap = binary.BigEndian.Uint32(encoded[offset : offset+4])
	offset += 4
	var err error
	payload.Commitment, offset, err = readFixedField(encoded, offset, "disclosure commitment")
	if err != nil {
		return nil, err
	}
	payload.Amount = new(big.Int).SetUint64(binary.BigEndian.Uint64(encoded[offset : offset+8]))
	offset += 8
	fields := []*(*big.Int){
		&payload.AssetID,
		&payload.SenderSpendKeyX, &payload.SenderSpendKeyY,
		&payload.SenderViewKeyX, &payload.SenderViewKeyY,
		&payload.RecipientSpendKeyX, &payload.RecipientSpendKeyY,
		&payload.RecipientViewKeyX, &payload.RecipientViewKeyY,
		&payload.DisclosureBlinding,
	}
	for i, target := range fields {
		*target, offset, err = readFixedField(encoded, offset, fmt.Sprintf("disclosure field %d", i))
		if err != nil {
			return nil, err
		}
	}
	if offset != len(encoded) {
		return nil, fmt.Errorf("DisclosurePlaintextV1 trailing bytes are not allowed")
	}
	if err := validateDisclosurePlaintextV1(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func WrapEncryptedEnvelopeV1(kind EncryptedEnvelopeKindV1, cipherText []byte) ([]byte, error) {
	expected, err := encryptedCiphertextSizeV1(kind)
	if err != nil {
		return nil, err
	}
	if len(cipherText) != expected {
		return nil, fmt.Errorf("encrypted envelope kind %d ciphertext must be exactly %d bytes, got %d", kind, expected, len(cipherText))
	}
	result := make([]byte, EncryptedEnvelopeV1HeaderSize+expected)
	copy(result[:16], encryptedEnvelopeV1DomainTag[:])
	binary.BigEndian.PutUint16(result[16:18], fixedBinaryVersionV1)
	result[18] = byte(kind)
	// result[19] is reserved zero.
	copy(result[EncryptedEnvelopeV1HeaderSize:], cipherText)
	return result, nil
}

func UnwrapEncryptedEnvelopeV1(encoded []byte, expectedKind EncryptedEnvelopeKindV1) ([]byte, error) {
	kind, cipherText, err := DecodeEncryptedEnvelopeV1(encoded)
	if err != nil {
		return nil, err
	}
	if kind != expectedKind {
		return nil, fmt.Errorf("encrypted envelope kind mismatch: got %d, expected %d", kind, expectedKind)
	}
	return cipherText, nil
}

// DecodeEncryptedEnvelopeV1 validates the domain/version/kind/reserved byte
// and the exact ciphertext length before returning a detached copy.
func DecodeEncryptedEnvelopeV1(encoded []byte) (EncryptedEnvelopeKindV1, []byte, error) {
	if len(encoded) < EncryptedEnvelopeV1HeaderSize {
		return 0, nil, fmt.Errorf("encrypted envelope is shorter than the %d-byte header", EncryptedEnvelopeV1HeaderSize)
	}
	if !bytes.Equal(encoded[:16], encryptedEnvelopeV1DomainTag[:]) {
		return 0, nil, fmt.Errorf("invalid encrypted envelope domain tag")
	}
	if version := binary.BigEndian.Uint16(encoded[16:18]); version != fixedBinaryVersionV1 {
		return 0, nil, fmt.Errorf("unsupported encrypted envelope version %d", version)
	}
	kind := EncryptedEnvelopeKindV1(encoded[18])
	expectedCiphertext, err := encryptedCiphertextSizeV1(kind)
	if err != nil {
		return 0, nil, err
	}
	expectedLength := EncryptedEnvelopeV1HeaderSize + expectedCiphertext
	if len(encoded) != expectedLength {
		return 0, nil, fmt.Errorf("encrypted envelope kind %d must be exactly %d bytes, got %d", kind, expectedLength, len(encoded))
	}
	if encoded[19] != 0 {
		return 0, nil, fmt.Errorf("encrypted envelope reserved byte must be zero")
	}
	return kind, append([]byte(nil), encoded[EncryptedEnvelopeV1HeaderSize:]...), nil
}

func EncryptedEnvelopeV1Size(kind EncryptedEnvelopeKindV1) (int, error) {
	cipherTextSize, err := encryptedCiphertextSizeV1(kind)
	if err != nil {
		return 0, err
	}
	return EncryptedEnvelopeV1HeaderSize + cipherTextSize, nil
}

func encryptedCiphertextSizeV1(kind EncryptedEnvelopeKindV1) (int, error) {
	switch kind {
	case EnvelopeDepositNoteV1:
		return NotePlaintextV1Size + symmetricEnvelopeOverheadV1, nil
	case EnvelopeTransferNoteV1:
		return NotePlaintextV1Size + eciesEnvelopeOverheadV1, nil
	case EnvelopeUserDisclosureV1, EnvelopeAuditDisclosureV1, EnvelopeSelfViewDisclosureV1:
		return DisclosurePlaintextV1Size + eciesEnvelopeOverheadV1, nil
	default:
		return 0, fmt.Errorf("unsupported encrypted envelope kind %d", kind)
	}
}

func validateDisclosurePlaintextV1(payload *DisclosurePlaintextV1) error {
	if payload == nil {
		return fmt.Errorf("disclosure plaintext is required")
	}
	fields := []struct {
		name  string
		value *big.Int
	}{
		{"commitment", payload.Commitment}, {"amount", payload.Amount}, {"asset id", payload.AssetID},
		{"sender spend key x", payload.SenderSpendKeyX}, {"sender spend key y", payload.SenderSpendKeyY},
		{"sender view key x", payload.SenderViewKeyX}, {"sender view key y", payload.SenderViewKeyY},
		{"recipient spend key x", payload.RecipientSpendKeyX}, {"recipient spend key y", payload.RecipientSpendKeyY},
		{"recipient view key x", payload.RecipientViewKeyX}, {"recipient view key y", payload.RecipientViewKeyY},
		{"disclosure blinding", payload.DisclosureBlinding},
	}
	for _, field := range fields {
		if err := validateCanonicalNoteField(field.name, field.value); err != nil {
			return err
		}
	}
	if payload.Commitment.Sign() == 0 {
		return fmt.Errorf("disclosure commitment must be non-zero")
	}
	if err := ValidateShieldedAmount("disclosure amount", payload.Amount); err != nil {
		return err
	}
	if payload.DisclosureBlinding.Sign() == 0 {
		return fmt.Errorf("disclosure blinding must be non-zero")
	}
	if payload.AssetID.Sign() == 0 {
		return fmt.Errorf("disclosure asset id must be non-zero")
	}

	switch payload.Plane {
	case DisclosurePlaneUserV1:
		if payload.Policy == TransferPrivacyPolicyAllPrivate || payload.Policy > TransferPrivacyPolicyDiscloseAmountToFrom {
			return fmt.Errorf("user disclosure plaintext requires policy 1..7")
		}
		if payload.DisclosedFieldBitmap != payload.Policy {
			return fmt.Errorf("user disclosure bitmap must equal policy in v1")
		}
		if payload.Policy&TransferPrivacyPolicyDiscloseAmount == 0 && payload.Amount.Sign() != 0 {
			return fmt.Errorf("undisclosed amount must use zero sentinel")
		}
		if payload.Policy&TransferPrivacyPolicyDiscloseFrom == 0 && !allZeroBigInts(
			payload.SenderSpendKeyX, payload.SenderSpendKeyY, payload.SenderViewKeyX, payload.SenderViewKeyY,
		) {
			return fmt.Errorf("undisclosed sender keys must use zero sentinel")
		}
		if payload.Policy&TransferPrivacyPolicyDiscloseFrom != 0 {
			if err := validateDisclosureKeyBundleV1(
				"sender", payload.SenderSpendKeyX, payload.SenderSpendKeyY, payload.SenderViewKeyX, payload.SenderViewKeyY,
			); err != nil {
				return err
			}
		}
		if payload.Policy&TransferPrivacyPolicyDiscloseTo == 0 && !allZeroBigInts(
			payload.RecipientSpendKeyX, payload.RecipientSpendKeyY, payload.RecipientViewKeyX, payload.RecipientViewKeyY,
		) {
			return fmt.Errorf("undisclosed recipient keys must use zero sentinel")
		}
		if payload.Policy&TransferPrivacyPolicyDiscloseTo != 0 {
			if err := validateDisclosureKeyBundleV1(
				"recipient", payload.RecipientSpendKeyX, payload.RecipientSpendKeyY, payload.RecipientViewKeyX, payload.RecipientViewKeyY,
			); err != nil {
				return err
			}
		}
	case DisclosurePlaneFullV1:
		if payload.Policy != DisclosureFullMarkerV1 || payload.DisclosedFieldBitmap != TransferPrivacyPolicyDiscloseAmountToFrom {
			return fmt.Errorf("full disclosure must use the full marker and bitmap")
		}
		if err := validateDisclosureKeyBundleV1(
			"sender", payload.SenderSpendKeyX, payload.SenderSpendKeyY, payload.SenderViewKeyX, payload.SenderViewKeyY,
		); err != nil {
			return err
		}
		if err := validateDisclosureKeyBundleV1(
			"recipient", payload.RecipientSpendKeyX, payload.RecipientSpendKeyY, payload.RecipientViewKeyX, payload.RecipientViewKeyY,
		); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported disclosure plane %d", payload.Plane)
	}
	return nil
}

func validateDisclosureKeyBundleV1(name string, spendX, spendY, viewX, viewY *big.Int) error {
	if _, err := pointFromBigInts(spendX, spendY); err != nil {
		return fmt.Errorf("invalid %s spend public key: %w", name, err)
	}
	if _, err := pointFromBigInts(viewX, viewY); err != nil {
		return fmt.Errorf("invalid %s view public key: %w", name, err)
	}
	return nil
}

func fixedDomainTagV1(label string) [16]byte {
	sum := sha256.Sum256([]byte(label))
	var result [16]byte
	copy(result[:], sum[:16])
	return result
}

func putFixedBytes(dst []byte, offset int, value []byte) int {
	copy(dst[offset:offset+len(value)], value)
	return offset + len(value)
}

func putFixedField(dst []byte, offset int, value *big.Int) int {
	return putFixedBytes(dst, offset, value.FillBytes(make([]byte, 32)))
}

func readFixedField(encoded []byte, offset int, name string) (*big.Int, int, error) {
	if offset+32 > len(encoded) {
		return nil, offset, fmt.Errorf("%s is truncated", name)
	}
	value := new(big.Int).SetBytes(encoded[offset : offset+32])
	if err := validateCanonicalNoteField(name, value); err != nil {
		return nil, offset, err
	}
	return value, offset + 32, nil
}

func allZeroBytes(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

func allZeroBigInts(values ...*big.Int) bool {
	for _, value := range values {
		if value == nil || value.Sign() != 0 {
			return false
		}
	}
	return true
}
