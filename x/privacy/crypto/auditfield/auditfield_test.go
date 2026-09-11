package auditfield

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestParseField32StrictCanonical(t *testing.T) {
	// These vectors are intentionally independent from fieldModulusBE so a
	// copied Fp modulus cannot make the test pass.
	pMinusOne := mustHex32(t, "30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000000")
	p := mustHex32(t, "30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000001")
	pPlusOne := mustHex32(t, "30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000002")
	fpMinusOne := mustHex32(t, "30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd46")
	field, err := ParseField32(pMinusOne[:])
	if err != nil {
		t.Fatalf("p-1: %v", err)
	}
	if !bytes.Equal(field.Bytes(), pMinusOne[:]) {
		t.Fatal("field bytes changed")
	}
	if _, err := ParseField32(p[:]); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("p accepted: %v", err)
	}
	if _, err := ParseField32(pPlusOne[:]); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("p+1 accepted: %v", err)
	}
	if _, err := ParseField32(fpMinusOne[:]); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("Fp-1 accepted as Fr: %v", err)
	}
	if _, err := ParseField32(make([]byte, FieldSize-1)); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("short field: %v", err)
	}
}

func mustHex32(t *testing.T, encoded string) [FieldSize]byte {
	t.Helper()
	raw, err := hex.DecodeString(encoded)
	if err != nil || len(raw) != FieldSize {
		t.Fatalf("invalid test vector %q: %v", encoded, err)
	}
	var value [FieldSize]byte
	copy(value[:], raw)
	return value
}

func TestPublicInputSchemaIsCompleteAndDefensive(t *testing.T) {
	schema := PublicInputSchema()
	if len(schema) != 23 {
		t.Fatalf("schema length = %d", len(schema))
	}
	if schema[0] != (PublicInputField{"NetworkHi", "uint128"}) || schema[22] != (PublicInputField{"CipherRoot1", "bn254-fr"}) {
		t.Fatalf("unexpected schema endpoints: %#v %#v", schema[0], schema[22])
	}
	schema[0].Name = "mutated"
	if PublicInputSchema()[0].Name != "NetworkHi" {
		t.Fatal("schema caller mutation changed package table")
	}
}

func TestEnvelopeFramesRoundTripAndRejectMalformedInput(t *testing.T) {
	cases := []struct {
		kind            Kind
		inputs, outputs uint8
	}{
		{KindDeposit, 0, 1}, {KindWithdraw, 1, 0}, {KindTransfer2x2, 2, 2}, {KindBatch16x32, 16, 32},
	}
	for _, test := range cases {
		t.Run(string(rune('0'+test.kind)), func(t *testing.T) {
			fields, err := test.kind.PlaintextFieldCount()
			if err != nil {
				t.Fatal(err)
			}
			ciphertext := make([]Field32, fields)
			var nonce Nonce128
			nonce[15] = byte(test.kind)
			frame, err := NewEnvelopeFrame(test.kind, test.inputs, test.outputs, nonce, testPoint(t), ciphertext, Field32{})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := frame.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			want, _ := test.kind.EnvelopeSize()
			if len(raw) != want {
				t.Fatalf("frame length = %d, want %d", len(raw), want)
			}
			decoded, err := ParseEnvelopeFrame(test.kind, test.inputs, test.outputs, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded.Nonce().Bytes(), nonce[:]) {
				t.Fatal("nonce did not round trip")
			}
			raw[0] ^= 1
			if _, err := ParseEnvelopeFrame(test.kind, test.inputs, test.outputs, raw); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("header mutation accepted: %v", err)
			}
		})
	}
}

func TestEnvelopeHeaderAndNegativeFraming(t *testing.T) {
	frame, err := NewEnvelopeFrame(KindDeposit, 0, 1, Nonce128{}, testPoint(t), make([]Field32, 6), Field32{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(raw[:16]); got != "ce935649ba1f847d6defd48478ff34f3" {
		t.Fatalf("header domain prefix = %s", got)
	}
	if _, err := ParseEnvelopeFrame(KindDeposit, 0, 0, raw); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("count mismatch accepted: %v", err)
	}
	if _, err := ParseEnvelopeFrame(KindDeposit, 0, 1, append(raw, 0)); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("trailing byte accepted: %v", err)
	}
	raw[23] = 1
	if _, err := ParseEnvelopeFrame(KindDeposit, 0, 1, raw); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("reserved byte accepted: %v", err)
	}
}

func TestEnvelopeRejectsNonCanonicalCiphertextAndPointCoordinate(t *testing.T) {
	frame, err := NewEnvelopeFrame(KindWithdraw, 1, 0, Nonce128{}, testPoint(t), make([]Field32, 2), Field32{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pointOffset := EnvelopeHeaderSize + NonceSize
	copy(raw[pointOffset:pointOffset+FieldSize], fieldModulusBE[:])
	if _, err := ParseEnvelopeFrame(KindWithdraw, 1, 0, raw); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("noncanonical point coordinate accepted: %v", err)
	}
	raw, _ = frame.Bytes()
	cipherOffset := EnvelopeHeaderSize + NonceSize + PointSize
	copy(raw[cipherOffset:cipherOffset+FieldSize], fieldModulusBE[:])
	if _, err := ParseEnvelopeFrame(KindWithdraw, 1, 0, raw); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("noncanonical ciphertext accepted: %v", err)
	}
}

func TestAuditContextIsNonceFreeAndCopiesInputs(t *testing.T) {
	context := testContext(t, KindTransfer2x2, 2, 2)
	pi := context.PublicInputs()
	pi[0] = Field32FromUint64(9)
	context, err := NewAuditContext(KindTransfer2x2, pi)
	if err != nil {
		t.Fatal(err)
	}
	pi[0] = Field32{}
	values, err := context.T(Nonce128{15: 7})
	if err != nil {
		t.Fatal(err)
	}
	if values[0] != Field32FromUint64(2) || values[1] != Field32FromUint64(2) || values[2] != Field32FromUint64(3) || values[3] != Field32FromUint64(9) {
		t.Fatalf("unexpected T prefix: %#v", values[:4])
	}
	if values[24][31] != 7 {
		t.Fatal("nonce was not encoded as a uint128 field")
	}
	copyOut := context.PublicInputs()
	copyOut[0] = Field32{}
	if context.PublicInputs()[0] != Field32FromUint64(9) {
		t.Fatal("context exposed mutable PI")
	}
	firstHash, err := context.ContextHash(Nonce128{15: 7})
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := context.ContextHash(Nonce128{15: 8})
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("context hash omitted nonce-derived T field")
	}
	bad := testContext(t, KindDeposit, 0, 1).PublicInputs()
	bad[0] = mustHex32(t, "30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000001")
	if _, err := NewAuditContext(KindDeposit, bad); !errors.Is(err, ErrInvalidField) {
		t.Fatalf("context accepted caller-constructed p: %v", err)
	}
}

func TestEnvelopeConstructorCopiesCiphertext(t *testing.T) {
	ciphertext := []Field32{Field32FromUint64(1), Field32FromUint64(2)}
	frame, err := NewEnvelopeFrame(KindWithdraw, 1, 0, Nonce128{}, testPoint(t), ciphertext, Field32{})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] = Field32{}
	if frame.Ciphertext()[0] != Field32FromUint64(1) {
		t.Fatal("envelope retained caller ciphertext slice")
	}
}

func TestEncodeAuxGoldenAndP1cBoundary(t *testing.T) {
	encoded, err := EncodeAuxFrame(KindWithdraw, nil)
	if err != nil {
		t.Fatal(err)
	}
	const want = "00010200"
	if got := hex.EncodeToString(encoded); got != want {
		t.Fatalf("Aux golden mismatch\n got: %s\nwant: %s", got, want)
	}
	deposit := AuxOutput{RecoveryCiphertext: bytes.Repeat([]byte{0xaa}, depositRecoveryCiphertextSize)}
	if _, err := EncodeAuxFrame(KindDeposit, []AuxOutput{deposit}); err != nil {
		t.Fatalf("deposit frame: %v", err)
	}
	transferOutput := AuxOutput{RecoveryCiphertext: bytes.Repeat([]byte{0xaa}, noteRecoveryCiphertextSize), ViewTag: []byte{1, 2}}
	if _, err := EncodeAuxFrame(KindTransfer2x2, []AuxOutput{transferOutput, transferOutput}); err != nil {
		t.Fatalf("transfer recovery-only outputs: %v", err)
	}
	encryptedTarget := AuxOutput{RecoveryCiphertext: bytes.Repeat([]byte{0xaa}, noteRecoveryCiphertextSize), ViewTag: []byte{1, 2}, UserPolicy: 1, UserMode: 2, UserDigest: Field32FromUint64(1), SelfFullDigest: Field32FromUint64(2), UserTargetPubKey: testLegacyPoint(t), UserDisclosurePayload: bytes.Repeat([]byte{2}, encryptedDisclosurePayloadSize)}
	if _, err = EncodeAuxFrame(KindBatch16x32, []AuxOutput{encryptedTarget}); err != nil {
		t.Fatalf("valid recipient-encrypted target rejected: %v", err)
	}
	encryptedTarget.UserTargetPubKey = bytes.Repeat([]byte{1}, FieldSize)
	if _, err = EncodeAuxFrame(KindBatch16x32, []AuxOutput{encryptedTarget}); !errors.Is(err, ErrInvalidPoint) {
		t.Fatalf("invalid recipient-encrypted target accepted: %v", err)
	}
	if _, err := EncodeAuxFrame(KindDeposit, []AuxOutput{}); err == nil {
		t.Fatal("missing deposit output accepted")
	}
	if _, err := EncodeAuxFrame(KindDeposit, []AuxOutput{{RecoveryCiphertext: []byte{1}}}); err == nil {
		t.Fatal("short deposit recovery ciphertext accepted")
	}
	if _, err := EncodeAuxFrame(KindTransfer2x2, []AuxOutput{transferOutput, {RecoveryCiphertext: bytes.Repeat([]byte{1}, noteRecoveryCiphertextSize), ViewTag: []byte{1}}}); err == nil {
		t.Fatal("short view tag accepted")
	}
}
