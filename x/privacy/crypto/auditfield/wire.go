package auditfield

import (
	"bytes"
	"fmt"
)

// EnvelopeFrame owns a canonical frame with a fully validated subgroup E.
// Frame validity does not establish chain provenance or AE authentication.
type EnvelopeFrame struct {
	kind, inputs, outputs uint8
	nonce                 Nonce128
	ephemeral             Point64
	ciphertext            []Field32
	tag                   Field32
}

func ParseEnvelopeFrame(kind Kind, inputs, outputs uint8, raw []byte) (EnvelopeFrame, error) {
	if err := kind.ValidateActiveCounts(inputs, outputs); err != nil {
		return EnvelopeFrame{}, err
	}
	want, err := kind.EnvelopeSize()
	if err != nil {
		return EnvelopeFrame{}, err
	}
	if len(raw) != want {
		return EnvelopeFrame{}, fmt.Errorf("%w: kind %d envelope must be exactly %d bytes, got %d", ErrInvalidEnvelope, kind, want, len(raw))
	}
	header, err := envelopeHeader(kind, inputs, outputs)
	if err != nil {
		return EnvelopeFrame{}, err
	}
	if !bytes.Equal(raw[:EnvelopeHeaderSize], header[:]) {
		return EnvelopeFrame{}, fmt.Errorf("%w: header/version/suite/kind/count/reserved bytes mismatch", ErrInvalidEnvelope)
	}
	nonce, err := ParseNonce128(raw[EnvelopeHeaderSize : EnvelopeHeaderSize+NonceSize])
	if err != nil {
		return EnvelopeFrame{}, err
	}
	offset := EnvelopeHeaderSize + NonceSize
	ephemeral, err := ParsePoint64Frame(raw[offset : offset+PointSize])
	if err != nil {
		return EnvelopeFrame{}, fmt.Errorf("%w: ephemeral point: %v", ErrInvalidEnvelope, err)
	}
	offset += PointSize
	fieldCount, _ := kind.PlaintextFieldCount()
	ciphertext := make([]Field32, fieldCount)
	for index := range ciphertext {
		ciphertext[index], err = ParseField32(raw[offset : offset+FieldSize])
		if err != nil {
			return EnvelopeFrame{}, fmt.Errorf("%w: ciphertext field %d: %v", ErrInvalidEnvelope, index, err)
		}
		offset += FieldSize
	}
	tag, err := ParseField32(raw[offset : offset+FieldSize])
	if err != nil {
		return EnvelopeFrame{}, fmt.Errorf("%w: tag: %v", ErrInvalidEnvelope, err)
	}
	offset += FieldSize
	if offset != len(raw) {
		return EnvelopeFrame{}, fmt.Errorf("%w: trailing bytes", ErrInvalidEnvelope)
	}
	return EnvelopeFrame{kind: uint8(kind), inputs: inputs, outputs: outputs, nonce: nonce, ephemeral: ephemeral, ciphertext: ciphertext, tag: tag}, nil
}

func NewEnvelopeFrame(kind Kind, inputs, outputs uint8, nonce Nonce128, ephemeral Point64, ciphertext []Field32, tag Field32) (EnvelopeFrame, error) {
	if err := kind.ValidateActiveCounts(inputs, outputs); err != nil {
		return EnvelopeFrame{}, err
	}
	want, _ := kind.PlaintextFieldCount()
	if len(ciphertext) != want {
		return EnvelopeFrame{}, fmt.Errorf("%w: kind %d ciphertext requires %d fields, got %d", ErrInvalidEnvelope, kind, want, len(ciphertext))
	}
	if err := validatePoint64Frame(ephemeral); err != nil {
		return EnvelopeFrame{}, err
	}
	for index, field := range ciphertext {
		if err := validateField32(field); err != nil {
			return EnvelopeFrame{}, fmt.Errorf("ciphertext field %d: %w", index, err)
		}
	}
	if err := validateField32(tag); err != nil {
		return EnvelopeFrame{}, fmt.Errorf("tag: %w", err)
	}
	return EnvelopeFrame{kind: uint8(kind), inputs: inputs, outputs: outputs, nonce: nonce, ephemeral: ephemeral, ciphertext: append([]Field32(nil), ciphertext...), tag: tag}, nil
}

func (e EnvelopeFrame) Kind() Kind                   { return Kind(e.kind) }
func (e EnvelopeFrame) ActiveCounts() (uint8, uint8) { return e.inputs, e.outputs }
func (e EnvelopeFrame) Nonce() Nonce128              { return e.nonce }
func (e EnvelopeFrame) EphemeralFrame() Point64      { return e.ephemeral }
func (e EnvelopeFrame) Ciphertext() []Field32        { return append([]Field32(nil), e.ciphertext...) }
func (e EnvelopeFrame) Tag() Field32                 { return e.tag }

func (e EnvelopeFrame) Bytes() ([]byte, error) {
	kind := Kind(e.kind)
	if err := kind.ValidateActiveCounts(e.inputs, e.outputs); err != nil {
		return nil, err
	}
	want, _ := kind.PlaintextFieldCount()
	if len(e.ciphertext) != want {
		return nil, fmt.Errorf("%w: ciphertext field count changed", ErrInvalidEnvelope)
	}
	if err := validatePoint64Frame(e.ephemeral); err != nil {
		return nil, err
	}
	for index, field := range e.ciphertext {
		if err := validateField32(field); err != nil {
			return nil, fmt.Errorf("ciphertext field %d: %w", index, err)
		}
	}
	if err := validateField32(e.tag); err != nil {
		return nil, fmt.Errorf("tag: %w", err)
	}
	header, err := envelopeHeader(kind, e.inputs, e.outputs)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, 0, EnvelopeHeaderSize+NonceSize+PointSize+(len(e.ciphertext)+1)*FieldSize)
	raw = append(raw, header[:]...)
	raw = append(raw, e.nonce[:]...)
	raw = append(raw, e.ephemeral.encoded[:]...)
	for _, field := range e.ciphertext {
		raw = append(raw, field[:]...)
	}
	raw = append(raw, e.tag[:]...)
	return raw, nil
}
