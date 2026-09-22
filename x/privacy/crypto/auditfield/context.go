package auditfield

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

const AuditContextPublicFieldCount = 21

// AuditContext owns only Kind and PI[0..20]. It intentionally has no nonce;
// callers obtain T with the envelope's actual nonce.
type AuditContext struct {
	kind Kind
	pi   [AuditContextPublicFieldCount]Field32
	key  AuditKey
}

func NewAuditContext(kind Kind, publicInputs []Field32) (AuditContext, error) {
	if _, err := kind.shape(); err != nil {
		return AuditContext{}, err
	}
	if len(publicInputs) != AuditContextPublicFieldCount {
		return AuditContext{}, fmt.Errorf("audit context requires exactly %d public inputs, got %d", AuditContextPublicFieldCount, len(publicInputs))
	}
	var context AuditContext
	context.kind = kind
	copy(context.pi[:], publicInputs)
	if err := context.validate(); err != nil {
		return AuditContext{}, err
	}
	key, err := auditKeyFromPublicInputs(context.pi)
	if err != nil {
		return AuditContext{}, err
	}
	context.key = key
	return context, nil
}

func (c AuditContext) Kind() Kind              { return c.kind }
func (c AuditContext) PublicInputs() []Field32 { return append([]Field32(nil), c.pi[:]...) }
func (c AuditContext) AuditKey() AuditKey      { return c.key }

// T returns [2, 2, kind] || PI[0..20] || [nonce128].
func (c AuditContext) T(nonce Nonce128) ([25]Field32, error) {
	if err := c.validate(); err != nil {
		return [25]Field32{}, err
	}
	var values [25]Field32
	values[0], values[1], values[2] = Field32FromUint64(2), Field32FromUint64(2), Field32FromUint64(uint64(c.kind))
	copy(values[3:24], c.pi[:])
	copy(values[24][FieldSize-NonceSize:], nonce[:])
	return values, nil
}

// ContextHash implements Hctx32 for archive cross-checking. It derives T from
// the nonce argument, keeping nonce out of AuditContext's stored state.
func (c AuditContext) ContextHash(nonce Nonce128) ([sha256.Size]byte, error) {
	values, err := c.T(nonce)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(AuditContextDomain))
	for _, value := range values {
		_, _ = hash.Write(value[:])
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func (c AuditContext) validate() error {
	if _, err := c.kind.shape(); err != nil {
		return err
	}
	for index, value := range c.pi {
		if err := validateField32(value); err != nil {
			return fmt.Errorf("audit context PI[%d]: %w", index, err)
		}
	}
	for _, index := range [...]int{0, 1, 2, 3, 15, 17, 18, 19, 20} {
		if err := validateUnsignedField(c.pi[index], 128); err != nil {
			return fmt.Errorf("audit context PI[%d] must be uint128: %w", index, err)
		}
	}
	for _, index := range [...]int{4, 7} {
		if err := validateUnsignedField(c.pi[index], 64); err != nil {
			return fmt.Errorf("audit context PI[%d] must be uint64: %w", index, err)
		}
	}
	if c.pi[4].IsZero() {
		return fmt.Errorf("audit context KeyEpoch must be positive")
	}
	if c.pi[7].IsZero() {
		return fmt.Errorf("audit context ExpiresAtUnix must be positive")
	}
	inputCount, err := fieldUint8(c.pi[9], 5)
	if err != nil {
		return fmt.Errorf("audit context InputCount: %w", err)
	}
	outputCount, err := fieldUint8(c.pi[10], 6)
	if err != nil {
		return fmt.Errorf("audit context OutputCount: %w", err)
	}
	if err := c.kind.ValidateActiveCounts(inputCount, outputCount); err != nil {
		return fmt.Errorf("audit context counts: %w", err)
	}
	if _, err := auditKeyFromPublicInputs(c.pi); err != nil {
		return err
	}
	return nil
}

func auditKeyFromPublicInputs(pi [AuditContextPublicFieldCount]Field32) (AuditKey, error) {
	point, err := NewPoint64Frame(pi[5], pi[6])
	if err != nil {
		return AuditKey{}, fmt.Errorf("audit context PK: %w", err)
	}
	key, err := NewAuditKey(point)
	if err != nil {
		return AuditKey{}, err
	}
	wantHi, wantLo := DigestFields(key.ID())
	if pi[2] != wantHi || pi[3] != wantLo {
		return AuditKey{}, fmt.Errorf("audit context KeyID does not match PK: %w", ErrInvalidAuditKey)
	}
	return key, nil
}

func validateUnsignedField(value Field32, bits uint) error {
	if bits > 256 {
		return fmt.Errorf("unsupported unsigned width %d", bits)
	}
	wholeBytes := FieldSize - int((bits+7)/8)
	for _, byteValue := range value[:wholeBytes] {
		if byteValue != 0 {
			return fmt.Errorf("non-zero high byte")
		}
	}
	if remainder := bits % 8; remainder != 0 && value[wholeBytes]&^byte((1<<remainder)-1) != 0 {
		return fmt.Errorf("value exceeds %d-bit range", bits)
	}
	return nil
}

func fieldUint8(value Field32, bits uint) (uint8, error) {
	if err := validateUnsignedField(value, bits); err != nil {
		return 0, err
	}
	return value[FieldSize-1], nil
}

// AuditPlain is the private field plaintext shape. The protocol relation,
// rather than this DTO, establishes that these fields come from a note
// witness. This leaf only protects its fixed length and canonical encodings.
type AuditPlain struct {
	kind   Kind
	fields []Field32
}

func NewAuditPlain(kind Kind, fields []Field32) (out AuditPlain, err error) {
	if err = secretprofile.Check(); err != nil {
		return AuditPlain{}, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = newAuditPlain(kind, fields) })
	return out, err
}

func newAuditPlain(kind Kind, fields []Field32) (AuditPlain, error) {
	want, err := kind.PlaintextFieldCount()
	if err != nil {
		return AuditPlain{}, err
	}
	if len(fields) != want {
		return AuditPlain{}, fmt.Errorf("kind %d plaintext requires %d fields, got %d", kind, want, len(fields))
	}
	plain := AuditPlain{kind: kind, fields: append([]Field32(nil), fields...)}
	if err := plain.validate(); err != nil {
		return AuditPlain{}, err
	}
	return plain, nil
}

func (p AuditPlain) Kind() Kind        { return p.kind }
func (p AuditPlain) Fields() []Field32 { return append([]Field32(nil), p.fields...) }

func (p AuditPlain) validate() error {
	want, err := p.kind.PlaintextFieldCount()
	if err != nil {
		return err
	}
	if len(p.fields) != want {
		return fmt.Errorf("kind %d plaintext requires %d fields, got %d", p.kind, want, len(p.fields))
	}
	for index, field := range p.fields {
		if err := validateField32(field); err != nil {
			return fmt.Errorf("audit plaintext field %d: %w", index, err)
		}
	}
	return nil
}

type CipherRoot struct{ Left, Right Field32 }

func (r CipherRoot) Validate() error {
	if err := validateField32(r.Left); err != nil {
		return fmt.Errorf("cipher root left: %w", err)
	}
	if err := validateField32(r.Right); err != nil {
		return fmt.Errorf("cipher root right: %w", err)
	}
	return nil
}
