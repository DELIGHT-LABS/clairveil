package auditfield

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
	frct "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/frct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

// ErrAuditDecrypt is intentionally indistinguishable across malformed
// envelope, wrong key, wrong root, and failed authentication tag cases.
var ErrAuditDecrypt = errors.New("audit decryption failed")

// EncryptAudit samples independent r in q* and a 128-bit nonce. It exposes
// only typed fixed context and plaintext; raw T, arbitrary AD, nonce and
// encryption scalar inputs are unavailable to production callers.
func EncryptAudit(context AuditContext, plain AuditPlain) (EnvelopeFrame, CipherRoot, error) {
	envelope, root, witness, err := EncryptAuditForProver(context, plain)
	witness.Clear()
	return envelope, root, err
}

// EncryptionWitness owns the scalar needed by the integrated proof. It is
// redacted by default; explicit witness export is inside the prover trust boundary.
type EncryptionWitness struct{ r scalarct.NonzeroScalar }

func (w EncryptionWitness) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "auditfield.EncryptionWitness(<redacted>)")
}
func (w EncryptionWitness) MarshalJSON() ([]byte, error) {
	return nil, errors.New("implicit encryption witness serialization is disabled")
}
func (w EncryptionWitness) MarshalText() ([]byte, error) {
	return nil, errors.New("implicit encryption witness serialization is disabled")
}
func (w *EncryptionWitness) Clear() { secretmem.Clear(&w.r) }
func (w EncryptionWitness) ToProverWitnessBE32() ([32]byte, error) {
	if w.r.IsValid() != 1 {
		return [32]byte{}, errors.New("invalid encryption witness")
	}
	var raw [32]byte
	subtle.WithDataIndependentTiming(func() { raw = w.r.Bytes() })
	return raw, nil
}

// EncryptAuditForProver samples fresh independent r/nonce exactly as EncryptAudit
// does and retains r solely for the integrated circuit's private witness.
// It accepts no caller-selected scalar, nonce, raw T, or arbitrary AD.
func EncryptAuditForProver(context AuditContext, plain AuditPlain) (EnvelopeFrame, CipherRoot, EncryptionWitness, error) {
	if err := secretprofile.Check(); err != nil {
		return EnvelopeFrame{}, CipherRoot{}, EncryptionWitness{}, err
	}
	r, err := scalarct.SampleNonzeroScalar(rand.Reader)
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, EncryptionWitness{}, err
	}
	defer secretmem.Clear(&r)
	var nonce Nonce128
	if _, err = io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return EnvelopeFrame{}, CipherRoot{}, EncryptionWitness{}, err
	}
	defer clear(nonce[:])
	var envelope EnvelopeFrame
	var root CipherRoot
	subtle.WithDataIndependentTiming(func() { envelope, root, err = encryptAuditWithNonce(context.AuditKey(), r, context, nonce, plain) })
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, EncryptionWitness{}, err
	}
	return envelope, root, EncryptionWitness{r: r}, nil
}

// encryptAuditWithNonce is deterministic test plumbing for the fixed
// relation. It is package-private, preventing selected nonce/r production use.
func encryptAuditWithNonce(key AuditKey, r scalarct.NonzeroScalar, context AuditContext, nonce Nonce128, plain AuditPlain) (EnvelopeFrame, CipherRoot, error) {
	defer secretmem.Clear(&r)
	if err := key.validate(); err != nil {
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	if err := context.validate(); err != nil || context.key.ID() != key.ID() {
		return EnvelopeFrame{}, CipherRoot{}, fmt.Errorf("invalid audit context or key binding")
	}
	if plain.Kind() != context.Kind() {
		return EnvelopeFrame{}, CipherRoot{}, fmt.Errorf("audit plaintext kind does not match context")
	}
	if r.IsValid() != 1 {
		return EnvelopeFrame{}, CipherRoot{}, fmt.Errorf("invalid audit encryption scalar")
	}

	t, err := context.T(nonce)
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	transcript, err := fieldElements(t[:])
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	message, err := fieldElements(plain.fields)
	defer clearElements(message)
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	pk, err := key.point.validPoint()
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, err
	}

	var ephemeral, shared edwardsct.Point
	defer secretmem.Clear(&shared)
	ephemeral.ScalarMultNonzero(pointPtr(edwardsct.Base()), r)
	shared.ScalarMultNonzero(&pk, r)
	ciphertext, err := sealAudit(&shared, &ephemeral, transcript, message, context.Kind())
	if err != nil {
		clearElements(transcript)
		clearElements(message)
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	encodedEphemeral, err := ephemeral.EncodePoint64()
	if err != nil {
		clearElements(transcript)
		clearElements(message)
		clearElements(ciphertext)
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	ephemeral64, err := ParsePoint64(encodedEphemeral[:])
	if err != nil {
		clearElements(transcript)
		clearElements(message)
		clearElements(ciphertext)
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	root, err := cipherRoot(context.Kind(), transcript, &ephemeral, ciphertext)
	if err != nil {
		clearElements(transcript)
		clearElements(message)
		clearElements(ciphertext)
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	cipherFields, err := fieldsFromElements(ciphertext[:len(ciphertext)-1])
	if err != nil {
		clearElements(transcript)
		clearElements(message)
		clearElements(ciphertext)
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	tag, err := fieldFromElement(ciphertext[len(ciphertext)-1])
	if err != nil {
		clearElements(transcript)
		clearElements(message)
		clearElements(ciphertext)
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	frame, err := NewEnvelopeFrame(context.Kind(), context.pi[9][31], context.pi[10][31], nonce, ephemeral64, cipherFields, tag)
	clearElements(transcript)
	clearElements(message)
	clearElements(ciphertext)
	if err != nil {
		return EnvelopeFrame{}, CipherRoot{}, err
	}
	return frame, root, nil
}

// DecryptAudit authenticates the exact envelope and returns plaintext only
// after root, context/key identity, and SAFE tag validation all succeed.
// Every failure is nil plus ErrAuditDecrypt so malformed public input cannot
// become a key or tag oracle.
func DecryptAudit(sk scalarct.NonzeroScalar, context AuditContext, envelope EnvelopeFrame, expectedRoot CipherRoot) (*AuditPlain, error) {
	defer secretmem.Clear(&sk)
	if sk.IsValid() != 1 || context.validate() != nil || expectedRoot.Validate() != nil {
		return nil, ErrAuditDecrypt
	}
	inputs, outputs := envelope.ActiveCounts()
	if envelope.Kind() != context.Kind() || context.kind.ValidateActiveCounts(inputs, outputs) != nil || inputs != context.pi[9][31] || outputs != context.pi[10][31] {
		return nil, ErrAuditDecrypt
	}
	if _, err := envelope.Bytes(); err != nil {
		return nil, ErrAuditDecrypt
	}
	t, err := context.T(envelope.Nonce())
	if err != nil {
		return nil, ErrAuditDecrypt
	}
	transcript, err := fieldElements(t[:])
	if err != nil {
		return nil, ErrAuditDecrypt
	}
	ciphertextFields := envelope.Ciphertext()
	ciphertext, err := fieldElements(ciphertextFields)
	if err != nil {
		clearElements(transcript)
		return nil, ErrAuditDecrypt
	}
	tag, ok := frct.FromCanonicalBE(envelope.Tag())
	if !ok {
		clearElements(transcript)
		clearElements(ciphertext)
		return nil, ErrAuditDecrypt
	}
	ciphertext = append(ciphertext, tag)
	ephemeral, err := envelope.EphemeralFrame().validPoint()
	if err != nil {
		clearElements(transcript)
		clearElements(ciphertext)
		return nil, ErrAuditDecrypt
	}
	actualRoot, err := cipherRoot(context.Kind(), transcript, &ephemeral, ciphertext)
	if err != nil || actualRoot != expectedRoot {
		clearElements(transcript)
		clearElements(ciphertext)
		return nil, ErrAuditDecrypt
	}

	var own edwardsct.Point
	base := edwardsct.Base()
	own.ScalarMultNonzero(&base, sk)
	registered, err := context.key.point.validPoint()
	if err != nil || !own.Equal(&registered) {
		clearElements(transcript)
		clearElements(ciphertext)
		return nil, ErrAuditDecrypt
	}
	var shared edwardsct.Point
	defer secretmem.Clear(&shared)
	shared.ScalarMultNonzero(&ephemeral, sk)
	plainElements, authenticated := openAudit(&shared, &ephemeral, transcript, ciphertext, context.Kind())
	clearElements(transcript)
	clearElements(ciphertext)
	if !authenticated {
		return nil, ErrAuditDecrypt
	}
	fields, err := fieldsFromElements(plainElements)
	clearElements(plainElements)
	if err != nil {
		return nil, ErrAuditDecrypt
	}
	plain, err := NewAuditPlain(context.Kind(), fields)
	clear(fields)
	if err != nil {
		return nil, ErrAuditDecrypt
	}
	return &plain, nil
}

// pointPtr turns a returned immutable value into a private local pointer.
func pointPtr(point edwardsct.Point) *edwardsct.Point { return &point }

func sealAudit(shared, ephemeral *edwardsct.Point, transcript, message []frct.Element, kind Kind) ([]frct.Element, error) {
	key, err := auditKDF(shared, ephemeral, transcript)
	defer secretmem.Clear(&key)
	if err != nil {
		return nil, err
	}
	aad := make([]frct.Element, 0, len(transcript)+3)
	aad = append(aad, transcript...)
	ex, ey, ok := pointCoordinates(ephemeral)
	if !ok {
		return nil, fmt.Errorf("invalid ephemeral point")
	}
	aad = append(aad, ex, ey, frct.Uint64(uint64(len(message))))
	tag, err := fixedTag(int(kind)) // tag 1..4: AE shapes in kind order.
	if err != nil {
		return nil, err
	}
	expected := aeSchedule(len(message), len(aad))
	s := newFixedSponge(tag, expected)
	defer s.clear()
	s.absorb(key[0], key[1])
	s.absorb(aad...)
	ciphertext := make([]frct.Element, len(message)+1)
	for i := range message {
		mask := s.squeeze()
		ciphertext[i].Add(&message[i], &mask)
		s.absorb(message[i])
	}
	ciphertext[len(message)] = s.squeeze()
	if err := s.finish(); err != nil {
		clearElements(ciphertext)
		return nil, err
	}
	return ciphertext, nil
}

func openAudit(shared, ephemeral *edwardsct.Point, transcript, ciphertext []frct.Element, kind Kind) ([]frct.Element, bool) {
	if len(ciphertext) < 1 {
		return nil, false
	}
	key, err := auditKDF(shared, ephemeral, transcript)
	defer secretmem.Clear(&key)
	if err != nil {
		return nil, false
	}
	n := len(ciphertext) - 1
	aad := make([]frct.Element, 0, len(transcript)+3)
	aad = append(aad, transcript...)
	ex, ey, ok := pointCoordinates(ephemeral)
	if !ok {
		return nil, false
	}
	aad = append(aad, ex, ey, frct.Uint64(uint64(n)))
	tag, err := fixedTag(int(kind))
	if err != nil {
		return nil, false
	}
	s := newFixedSponge(tag, aeSchedule(n, len(aad)))
	defer s.clear()
	s.absorb(key[0], key[1])
	s.absorb(aad...)
	plain := make([]frct.Element, n)
	for i := range plain {
		mask := s.squeeze()
		plain[i].Sub(&ciphertext[i], &mask)
		s.absorb(plain[i])
	}
	computedTag := s.squeeze()
	ok = s.finish() == nil && computedTag.Equal(&ciphertext[n]) == 1
	if !ok {
		clearElements(plain)
		return nil, false
	}
	return plain, true
}

func auditKDF(shared, ephemeral *edwardsct.Point, transcript []frct.Element) ([2]frct.Element, error) {
	sx, sy, ok := pointCoordinates(shared)
	if !ok {
		return [2]frct.Element{}, fmt.Errorf("invalid shared point")
	}
	ex, ey, ok := pointCoordinates(ephemeral)
	if !ok {
		return [2]frct.Element{}, fmt.Errorf("invalid ephemeral point")
	}
	values := make([]frct.Element, 0, 4+len(transcript))
	defer clearElements(values[:cap(values)])
	defer func() { sx, sy = frct.Element{}, frct.Element{} }()
	values = append(values, sx, sy, ex, ey)
	values = append(values, transcript...)
	return hash2(0, values)
}

func cipherRoot(kind Kind, transcript []frct.Element, ephemeral *edwardsct.Point, ciphertext []frct.Element) (CipherRoot, error) {
	ex, ey, ok := pointCoordinates(ephemeral)
	if !ok {
		return CipherRoot{}, fmt.Errorf("invalid ephemeral point")
	}
	values := make([]frct.Element, 0, len(transcript)+2+len(ciphertext))
	values = append(values, transcript...)
	values = append(values, ex, ey)
	values = append(values, ciphertext...)
	digest, err := hash2(4+int(kind), values) // tags 5..8: root shapes.
	if err != nil {
		return CipherRoot{}, err
	}
	left, err := fieldFromElement(digest[0])
	if err != nil {
		return CipherRoot{}, err
	}
	right, err := fieldFromElement(digest[1])
	if err != nil {
		return CipherRoot{}, err
	}
	return CipherRoot{Left: left, Right: right}, nil
}

func pointCoordinates(point *edwardsct.Point) (frct.Element, frct.Element, bool) {
	affine, ok := point.Affine()
	if !ok {
		return frct.Element{}, frct.Element{}, false
	}
	return affine.X, affine.Y, true
}

func aeSchedule(n, aad int) []uint32 {
	schedule := make([]uint32, 0, 3+2*n)
	schedule = append(schedule, ioAbsorbWord(2), ioAbsorbWord(aad))
	for i := 0; i < n; i++ {
		schedule = append(schedule, 1, ioAbsorbWord(1))
	}
	return append(schedule, 1)
}

func fieldElements(fields []Field32) ([]frct.Element, error) {
	result := make([]frct.Element, len(fields))
	for i := range fields {
		if err := validateField32(fields[i]); err != nil {
			clearElements(result)
			return nil, err
		}
		value, ok := frct.FromCanonicalBE(fields[i])
		if !ok {
			clearElements(result)
			return nil, ErrInvalidField
		}
		result[i] = value
	}
	return result, nil
}

func fieldsFromElements(elements []frct.Element) ([]Field32, error) {
	fields := make([]Field32, len(elements))
	for i := range elements {
		var err error
		fields[i], err = fieldFromElement(elements[i])
		if err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func fieldFromElement(element frct.Element) (Field32, error) {
	return Field32(element.Bytes()), nil
}

func clearElements(elements []frct.Element) {
	for i := range elements {
		elements[i] = frct.Element{}
	}
}
