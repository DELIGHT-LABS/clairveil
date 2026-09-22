// Package auditfield owns the audit-field wire DTOs and their canonical
// framing, curve/key validation and native audit cipher. It deliberately does
// not import the parent crypto package, types or SDK; chain provenance and
// generated message adaptation belong to higher layers.
package auditfield

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
)

const (
	FieldSize          = 32
	PointSize          = 64
	NonceSize          = 16
	EnvelopeHeaderSize = 32
	AuditVersion       = uint16(2)
	AuditSuite         = uint16(2)
	AuditPlainSchema   = uint16(1)

	CircuitSetID = "privacy-note-v1-u128-audit-field-v1"

	AuditEnvelopeDomain     = "clairveil.audit.envelope.v1"
	AuditKDFDomain          = "clairveil.audit.field.kdf.v1"
	AuditAEDomain           = "clairveil.audit.field.ae.v1"
	AuditCipherRootDomain   = "clairveil.audit.field.root.v1"
	AuditOwnerIntentDomain  = "clairveil.audit.field.owner.v1"
	AuditContextDomain      = "clairveil.audit.context.v2"
	AuditAuxDomain          = "clairveil.audit.aux.v1"
	AuditKeyDomain          = "clairveil.audit.key.v1"
	AuditNetworkDomain      = "clairveil.audit.network.v1"
	AuditPublicTargetDomain = "clairveil.audit.public-target.v1"
)

var (
	ErrInvalidField    = errors.New("invalid canonical BN254 field encoding")
	ErrInvalidKind     = errors.New("invalid audit kind")
	ErrInvalidEnvelope = errors.New("invalid audit envelope")
	ErrInvalidPoint    = errors.New("invalid audit Point64")
	ErrInvalidAuditKey = errors.New("invalid audit key identity")
)

// fieldModulusBE is the BN254 Fr modulus. It is intentionally local to the
// wire boundary so decoding does not depend on secret arithmetic packages.
var fieldModulusBE = [FieldSize]byte{
	0x30, 0x64, 0x4e, 0x72, 0xe1, 0x31, 0xa0, 0x29,
	0xb8, 0x50, 0x45, 0xb6, 0x81, 0x81, 0x58, 0x5d,
	0x28, 0x33, 0xe8, 0x48, 0x79, 0xb9, 0x70, 0x91,
	0x43, 0xe1, 0xf5, 0x93, 0xf0, 0x00, 0x00, 0x01,
}

// Field32 is the fixed-width big-endian encoding of an Fr element. Since Go
// permits callers to construct an array literal directly, every public wire
// boundary rechecks canonicality before using a Field32 value.
type Field32 [FieldSize]byte

func ParseField32(raw []byte) (Field32, error) {
	var value Field32
	if len(raw) != FieldSize {
		return value, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidField, FieldSize, len(raw))
	}
	copy(value[:], raw)
	if err := validateField32(value); err != nil {
		return Field32{}, err
	}
	return value, nil
}

func Field32FromUint64(value uint64) Field32 {
	var field Field32
	binary.BigEndian.PutUint64(field[FieldSize-8:], value)
	return field
}

func (f Field32) Bytes() []byte { return append([]byte(nil), f[:]...) }
func (f Field32) IsZero() bool  { return f == Field32{} }

func validateField32(value Field32) error {
	if _, ok := frct.FromCanonicalBE(value); !ok {
		return fmt.Errorf("%w: value is not smaller than Fr modulus", ErrInvalidField)
	}
	return nil
}

// Point64 is a validated canonical public point. It can only be constructed
// by the strict decoder, which checks Fr coordinates, curve membership, prime
// subgroup membership, and nonidentity.
type Point64 struct {
	encoded [PointSize]byte
	point   edwardsct.Point
	valid   bool
}

func ParsePoint64Frame(raw []byte) (Point64, error) {
	if len(raw) != PointSize {
		return Point64{}, fmt.Errorf("%w: point frame must be exactly %d bytes, got %d", ErrInvalidPoint, PointSize, len(raw))
	}
	point, err := edwardsct.DecodePoint64(raw)
	if err != nil {
		return Point64{}, fmt.Errorf("%w: %v", ErrInvalidPoint, err)
	}
	encoded, err := point.EncodePoint64()
	if err != nil {
		return Point64{}, fmt.Errorf("%w: %v", ErrInvalidPoint, err)
	}
	if !bytes.Equal(encoded[:], raw) {
		return Point64{}, fmt.Errorf("%w: noncanonical point encoding", ErrInvalidPoint)
	}
	return Point64{encoded: encoded, point: point, valid: true}, nil
}

// ParsePoint64 is the preferred name for the strict Point64 decoder.
func ParsePoint64(raw []byte) (Point64, error) { return ParsePoint64Frame(raw) }

func NewPoint64Frame(x, y Field32) (Point64, error) {
	if err := validateField32(x); err != nil {
		return Point64{}, err
	}
	if err := validateField32(y); err != nil {
		return Point64{}, err
	}
	var raw [PointSize]byte
	copy(raw[:FieldSize], x[:])
	copy(raw[FieldSize:], y[:])
	return ParsePoint64(raw[:])
}

func (p Point64) Bytes() []byte { return append([]byte(nil), p.encoded[:]...) }

func (p Point64) Coordinates() (Field32, Field32, error) {
	if err := validatePoint64Frame(p); err != nil {
		return Field32{}, Field32{}, err
	}
	var x, y Field32
	copy(x[:], p.encoded[:FieldSize])
	copy(y[:], p.encoded[FieldSize:])
	return x, y, nil
}

func (p Point64) validPoint() (edwardsct.Point, error) {
	if !p.valid {
		return edwardsct.Point{}, ErrInvalidPoint
	}
	// Re-decode the stored canonical bytes so zero/mutated manufactured values
	// cannot smuggle an invalid extended point through this package boundary.
	point, err := edwardsct.DecodePoint64(p.encoded[:])
	if err != nil {
		return edwardsct.Point{}, fmt.Errorf("%w: %v", ErrInvalidPoint, err)
	}
	return point, nil
}

func validatePoint64Frame(point Point64) error {
	if _, err := point.validPoint(); err != nil {
		return err
	}
	return nil
}

// AuditKey binds a validated Point64 to the only permitted deterministic key
// identifier. Callers cannot provide an unrelated KeyID.
type AuditKey struct {
	point Point64
	id    [sha256.Size]byte
}

func NewAuditKey(point Point64) (AuditKey, error) {
	if err := validatePoint64Frame(point); err != nil {
		return AuditKey{}, err
	}
	key := AuditKey{point: point}
	h := sha256.New()
	_, _ = h.Write([]byte(AuditKeyDomain))
	_, _ = h.Write([]byte{0, byte(AuditSuite)})
	_, _ = h.Write(point.encoded[:])
	copy(key.id[:], h.Sum(nil))
	return key, nil
}

// NewAuditKeyFromPoint64 is the registration/import boundary: it validates
// the public key first, then derives the only allowed KeyID.
func NewAuditKeyFromPoint64(raw []byte) (AuditKey, error) {
	point, err := ParsePoint64(raw)
	if err != nil {
		return AuditKey{}, err
	}
	return NewAuditKey(point)
}

func ParseAuditKey(pointRaw, keyID []byte) (AuditKey, error) {
	point, err := ParsePoint64(pointRaw)
	if err != nil {
		return AuditKey{}, err
	}
	key, err := NewAuditKey(point)
	if err != nil {
		return AuditKey{}, err
	}
	if len(keyID) != len(key.id) || !bytes.Equal(keyID, key.id[:]) {
		return AuditKey{}, ErrInvalidAuditKey
	}
	return key, nil
}

func (k AuditKey) Point() Point64        { return k.point }
func (k AuditKey) ID() [sha256.Size]byte { return k.id }
func (k AuditKey) IDBytes() []byte       { return append([]byte(nil), k.id[:]...) }

func (k AuditKey) validate() error {
	if err := validatePoint64Frame(k.point); err != nil {
		return err
	}
	expected, err := NewAuditKey(k.point)
	if err != nil || !bytes.Equal(k.id[:], expected.id[:]) {
		return ErrInvalidAuditKey
	}
	return nil
}

type Nonce128 [NonceSize]byte

func ParseNonce128(raw []byte) (Nonce128, error) {
	var nonce Nonce128
	if len(raw) != NonceSize {
		return nonce, fmt.Errorf("%w: nonce must be exactly %d bytes, got %d", ErrInvalidEnvelope, NonceSize, len(raw))
	}
	copy(nonce[:], raw)
	return nonce, nil
}

func (n Nonce128) Bytes() []byte { return append([]byte(nil), n[:]...) }

type Kind uint8

const (
	KindDeposit     Kind = 1
	KindWithdraw    Kind = 2
	KindTransfer2x2 Kind = 3
	KindBatch16x32  Kind = 4
)

type kindShape struct {
	inputCap, outputCap, plainFields int
	envelopeBytes                    int
}

func (k Kind) shape() (kindShape, error) {
	switch k {
	case KindDeposit:
		return kindShape{0, 1, 6, 336}, nil
	case KindWithdraw:
		return kindShape{1, 0, 2, 208}, nil
	case KindTransfer2x2:
		return kindShape{2, 2, 13, 560}, nil
	case KindBatch16x32:
		return kindShape{16, 32, 177, 5808}, nil
	default:
		return kindShape{}, fmt.Errorf("%w: %d", ErrInvalidKind, k)
	}
}

func (k Kind) PlaintextFieldCount() (int, error) {
	shape, err := k.shape()
	return shape.plainFields, err
}
func (k Kind) EnvelopeSize() (int, error) { shape, err := k.shape(); return shape.envelopeBytes, err }

func (k Kind) ValidateActiveCounts(inputs, outputs uint8) error {
	shape, err := k.shape()
	if err != nil {
		return err
	}
	switch k {
	case KindDeposit:
		if inputs != 0 || outputs != 1 {
			return fmt.Errorf("%w: deposit requires 0 inputs and 1 output", ErrInvalidEnvelope)
		}
	case KindWithdraw:
		if inputs != 1 || outputs != 0 {
			return fmt.Errorf("%w: withdraw requires 1 input and 0 outputs", ErrInvalidEnvelope)
		}
	case KindTransfer2x2:
		if inputs != 2 || outputs != 2 {
			return fmt.Errorf("%w: transfer requires 2 inputs and 2 outputs", ErrInvalidEnvelope)
		}
	case KindBatch16x32:
		if inputs == 0 || int(inputs) > shape.inputCap || outputs == 0 || int(outputs) > shape.outputCap {
			return fmt.Errorf("%w: batch counts must be 1..%d and 1..%d", ErrInvalidEnvelope, shape.inputCap, shape.outputCap)
		}
	}
	return nil
}

// PublicInputField describes one consensus-visible field. Callers receive a
// fresh slice from PublicInputSchema and may not alter the package table.
type PublicInputField struct{ Name, Encoding string }

var publicInputSchema = [...]PublicInputField{
	{"NetworkHi", "uint128"}, {"NetworkLo", "uint128"},
	{"KeyIDHi", "uint128"}, {"KeyIDLo", "uint128"},
	{"KeyEpoch", "uint64"}, {"PKx", "bn254-fr"}, {"PKy", "bn254-fr"},
	{"ExpiresAtUnix", "uint64"}, {"MerkleRoot", "bn254-fr"},
	{"InputCount", "uint5"}, {"OutputCount", "uint6"}, {"NullifierRoot", "bn254-fr"},
	{"CommitmentRoot", "bn254-fr"}, {"UserDisclosureRoot", "bn254-fr"}, {"SelfViewRoot", "bn254-fr"},
	{"PublicAmount", "uint128"}, {"PublicAsset", "bn254-fr"},
	{"PublicTargetHi", "uint128"}, {"PublicTargetLo", "uint128"},
	{"AuxHi", "uint128"}, {"AuxLo", "uint128"},
	{"CipherRoot0", "bn254-fr"}, {"CipherRoot1", "bn254-fr"},
}

func PublicInputSchema() []PublicInputField {
	result := make([]PublicInputField, len(publicInputSchema))
	copy(result, publicInputSchema[:])
	return result
}

func envelopeHeader(kind Kind, inputs, outputs uint8) ([EnvelopeHeaderSize]byte, error) {
	if err := kind.ValidateActiveCounts(inputs, outputs); err != nil {
		return [EnvelopeHeaderSize]byte{}, err
	}
	var header [EnvelopeHeaderSize]byte
	digest := sha256.Sum256([]byte(AuditEnvelopeDomain))
	copy(header[:16], digest[:16])
	binary.BigEndian.PutUint16(header[16:18], AuditVersion)
	binary.BigEndian.PutUint16(header[18:20], AuditSuite)
	header[20], header[21], header[22] = byte(kind), inputs, outputs
	return header, nil
}
