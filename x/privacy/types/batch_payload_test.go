package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalBatchTransferPayloadV1IndependentGolden(t *testing.T) {
	msg := batchPayloadTestMessage(t)
	encoded, err := CanonicalBatchTransferPayloadBytesV1(msg)
	require.NoError(t, err)
	require.Equal(t, referenceCanonicalBatchTransferPayloadBytesV1(t, msg), encoded)

	digest, err := ComputeBatchTransferPayloadDigestV1(msg)
	require.NoError(t, err)
	referenceHash := sha256.Sum256(append([]byte(BatchTransferPayloadV1ByteDomain), encoded...))
	require.Equal(t, new(big.Int).SetBytes(referenceHash[:16]), digest.Hi)
	require.Equal(t, new(big.Int).SetBytes(referenceHash[16:]), digest.Lo)

	t.Logf("payload_bytes=%d digest=%s hi=%s lo=%s", len(encoded), hex.EncodeToString(referenceHash[:]), digest.Hi, digest.Lo)
	const goldenDigestHex = "f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0"
	const goldenHi = "322132945931579789235567236199104333743"
	const goldenLo = "14314064343031468430392382204273370288"
	require.Equal(t, goldenDigestHex, hex.EncodeToString(referenceHash[:]))
	require.Equal(t, goldenHi, digest.Hi.String())
	require.Equal(t, goldenLo, digest.Lo.String())

	relayerChanged := *msg
	relayerChanged.Creator = "clair1replacementrelayer"
	relayerChanged.Proof = bytes.Repeat([]byte{0xff}, 164)
	relayerDigest, err := ComputeBatchTransferPayloadDigestV1(&relayerChanged)
	require.NoError(t, err)
	require.Equal(t, digest, relayerDigest)

	expiryChanged := *msg
	expiryChanged.ExpiresAtUnix++
	expiryDigest, err := ComputeBatchTransferPayloadDigestV1(&expiryChanged)
	require.NoError(t, err)
	require.NotEqual(t, digest, expiryDigest)
}

func TestCanonicalMsgBatchTransferPayloadV1MatchesFrozenPrototypeExactly(t *testing.T) {
	prototype := batchPayloadTestMessage(t)
	production := productionBatchPayloadTestMessage(t)

	prototypeWire, err := prototype.Marshal()
	require.NoError(t, err)
	productionWire, err := production.Marshal()
	require.NoError(t, err)
	require.Equal(t, prototypeWire, productionWire, "production field numbers/order must match the frozen prototype")

	prototypePayload, err := CanonicalBatchTransferPayloadBytesV1(prototype)
	require.NoError(t, err)
	productionPayload, err := CanonicalMsgBatchTransferPayloadBytesV1(production)
	require.NoError(t, err)
	require.Equal(t, prototypePayload, productionPayload)

	payloadSize, err := CanonicalMsgBatchTransferPayloadSizeV1(production)
	require.NoError(t, err)
	require.Equal(t, uint64(len(productionPayload)), payloadSize)

	prototypeDigest, err := ComputeBatchTransferPayloadDigestV1(prototype)
	require.NoError(t, err)
	productionDigest, err := ComputeMsgBatchTransferPayloadDigestV1(production)
	require.NoError(t, err)
	require.Equal(t, prototypeDigest, productionDigest)
	require.Equal(t, "322132945931579789235567236199104333743", productionDigest.Hi.String())
	require.Equal(t, "14314064343031468430392382204273370288", productionDigest.Lo.String())

	// creator and proof are outer transaction framing, not owner-effect fields.
	// Canonical bytes/digest/size must therefore be available before proving and
	// remain stable across proof regeneration or relayer replacement.
	production.Creator = "replacement-relayer"
	production.Proof = bytes.Repeat([]byte{0x5a}, BatchTransferProofSizeV1)
	regeneratedPayload, err := CanonicalMsgBatchTransferPayloadBytesV1(production)
	require.NoError(t, err)
	require.Equal(t, productionPayload, regeneratedPayload)
	regeneratedSize, err := CanonicalMsgBatchTransferPayloadSizeV1(production)
	require.NoError(t, err)
	require.Equal(t, payloadSize, regeneratedSize)
	regeneratedDigest, err := ComputeMsgBatchTransferPayloadDigestV1(production)
	require.NoError(t, err)
	require.Equal(t, productionDigest, regeneratedDigest)

	production.Creator = string(bytes.Repeat([]byte{'x'}, MaxBatchTransferMessageBytesV1+1))
	production.Proof = nil
	prepareDigest, err := ComputeMsgBatchTransferPayloadDigestV1(production)
	require.NoError(t, err)
	require.Equal(t, productionDigest, prepareDigest)
	require.ErrorContains(t, ValidateMsgBatchTransferFramingV1(production), "proof must be exactly 164 bytes")
}

func TestCanonicalMsgBatchTransferPayloadSizeV1IsLengthOnly(t *testing.T) {
	msg := productionBatchPayloadTestMessage(t)

	// Keep every frozen field length valid while making the semantic encodings
	// non-canonical. The precharge helper must not decode points/envelopes or
	// recompute disclosure digests.
	msg.Root = bytes.Repeat([]byte{0xff}, expectedFieldElementBytes)
	msg.Outputs[0].Ciphertext[0] ^= 0xff
	msg.AuditDisclosureTargetPubkey = make([]byte, expectedFieldElementBytes)

	require.NoError(t, ValidateMsgBatchTransferFramingV1(msg))
	size, err := CanonicalMsgBatchTransferPayloadSizeV1(msg)
	require.NoError(t, err)
	require.Positive(t, size)
	require.Error(t, ValidateMsgBatchTransferEffectsV1(msg))
}

func TestCanonicalMsgBatchTransferPayloadSizeV1MaxShapeGolden(t *testing.T) {
	msg := maxProductionBatchPayloadTestMessage(t)
	payload, err := CanonicalMsgBatchTransferPayloadBytesV1(msg)
	require.NoError(t, err)
	require.Len(t, payload, 65_384)

	size, err := CanonicalMsgBatchTransferPayloadSizeV1(msg)
	require.NoError(t, err)
	require.Equal(t, uint64(65_384), size)
	require.Less(t, msg.Size(), MaxBatchTransferMessageBytesV1)
	require.NoError(t, ValidateMsgBatchTransferFramingV1(msg))
}

func TestValidateMsgBatchTransferFramingV1Bounds(t *testing.T) {
	const expectedHardCap = 128 << 10
	require.Equal(t, expectedHardCap, MaxBatchTransferMessageBytesV1)

	tests := []struct {
		name    string
		mutate  func(*MsgBatchTransfer)
		message string
	}{
		{"nil output", func(msg *MsgBatchTransfer) { msg.Outputs[0] = nil }, "output 0 is required"},
		{"proof length", func(msg *MsgBatchTransfer) { msg.Proof = msg.Proof[:BatchTransferProofSizeV1-1] }, "proof must be exactly 164 bytes"},
		{"no inputs", func(msg *MsgBatchTransfer) { msg.Nullifiers = nil }, "input count must be in 1..16"},
		{"too many inputs", func(msg *MsgBatchTransfer) {
			msg.Nullifiers = make([][]byte, BatchJoinSplitV1MaxInputs+1)
		}, "input count must be in 1..16"},
		{"no outputs", func(msg *MsgBatchTransfer) { msg.Outputs = nil }, "output count must be in 1..32"},
		{"root length", func(msg *MsgBatchTransfer) { msg.Root = msg.Root[:31] }, "merkle root must be exactly 32 bytes"},
		{"nullifier length", func(msg *MsgBatchTransfer) { msg.Nullifiers[0] = msg.Nullifiers[0][:31] }, "nullifier 0 must be exactly 32 bytes"},
		{"ciphertext length", func(msg *MsgBatchTransfer) { msg.Outputs[0].Ciphertext = msg.Outputs[0].Ciphertext[:429] }, "ciphertext must be exactly 430 bytes"},
		{"view tag length", func(msg *MsgBatchTransfer) { msg.Outputs[0].ViewTag = msg.Outputs[0].ViewTag[:1] }, "view tag must be exactly 2 bytes"},
		{"user payload length", func(msg *MsgBatchTransfer) { msg.Outputs[1].UserDisclosurePayload = []byte{1} }, "user disclosure payload has invalid fixed length"},
		{"full digest length", func(msg *MsgBatchTransfer) { msg.Outputs[0].FullDisclosureDigest = nil }, "full disclosure digest must be exactly 32 bytes"},
		{"audit payload length", func(msg *MsgBatchTransfer) { msg.Outputs[0].AuditDisclosurePayload = nil }, "audit disclosure payload must be exactly 472 bytes"},
		{"self view length", func(msg *MsgBatchTransfer) { msg.Outputs[0].SelfViewDisclosurePayload = []byte{1} }, "self-view disclosure payload must be empty or exactly 472 bytes"},
		{"audit target length", func(msg *MsgBatchTransfer) { msg.AuditDisclosureTargetPubkey = nil }, "audit disclosure target pubkey must be exactly 32 bytes"},
		{"hard cap", func(msg *MsgBatchTransfer) {
			msg.Creator = string(bytes.Repeat([]byte{'a'}, MaxBatchTransferMessageBytesV1))
		}, "exceeds 131072-byte hard cap"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			msg := productionBatchPayloadTestMessage(t)
			test.mutate(msg)
			require.ErrorContains(t, ValidateMsgBatchTransferFramingV1(msg), test.message)
		})
	}
}

func TestValidateBatchTransferWirePrototypeV1RejectsNonCanonicalEffects(t *testing.T) {
	t.Run("duplicate commitment", func(t *testing.T) {
		msg := batchPayloadTestMessage(t)
		msg.Outputs[1].Commitment = append([]byte(nil), msg.Outputs[0].Commitment...)
		require.ErrorContains(t, ValidateBatchTransferWirePrototypeV1(msg), "duplicate")
	})

	t.Run("mixed self view mode", func(t *testing.T) {
		msg := batchPayloadTestMessage(t)
		msg.Outputs[1].SelfViewDisclosurePayload = nil
		require.ErrorContains(t, ValidateBatchTransferWirePrototypeV1(msg), "batch-level all-or-none")
	})

	t.Run("all private with user payload", func(t *testing.T) {
		msg := batchPayloadTestMessage(t)
		msg.Outputs[0].UserDisclosurePayload = batchPayloadTestEnvelope(t, EnvelopeUserDisclosureV1)
		require.ErrorContains(t, ValidateBatchTransferWirePrototypeV1(msg), "all-private")
	})

	t.Run("invalid audit target", func(t *testing.T) {
		msg := batchPayloadTestMessage(t)
		msg.AuditDisclosureTargetPubkey = make([]byte, 32)
		require.ErrorContains(t, ValidateBatchTransferWirePrototypeV1(msg), "target pubkey is invalid")
	})

	t.Run("unknown user mode", func(t *testing.T) {
		msg := batchPayloadTestMessage(t)
		msg.Outputs[1].UserDisclosureMode = UserDisclosureMode(99)
		require.ErrorContains(t, ValidateBatchTransferWirePrototypeV1(msg), "unsupported user disclosure mode")
	})
}

func TestBatchTransferPayloadDigestV1BindsEveryEffectClass(t *testing.T) {
	base := batchPayloadTestMessage(t)
	want, err := ComputeBatchTransferPayloadDigestV1(base)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*BatchTransferWirePrototypeV1)
	}{
		{"root", func(msg *BatchTransferWirePrototypeV1) { msg.Root = batchPayloadTestField(41) }},
		{"input count", func(msg *BatchTransferWirePrototypeV1) { msg.Nullifiers = msg.Nullifiers[:1] }},
		{"nullifier order", func(msg *BatchTransferWirePrototypeV1) {
			msg.Nullifiers[0], msg.Nullifiers[1] = msg.Nullifiers[1], msg.Nullifiers[0]
		}},
		{"output count", func(msg *BatchTransferWirePrototypeV1) { msg.Outputs = msg.Outputs[:1] }},
		{"output order", func(msg *BatchTransferWirePrototypeV1) {
			msg.Outputs[0], msg.Outputs[1] = msg.Outputs[1], msg.Outputs[0]
		}},
		{"commitment", func(msg *BatchTransferWirePrototypeV1) { msg.Outputs[0].Commitment = batchPayloadTestField(43) }},
		{"ciphertext", func(msg *BatchTransferWirePrototypeV1) {
			msg.Outputs[0].Ciphertext[len(msg.Outputs[0].Ciphertext)-1] ^= 1
		}},
		{"view tag", func(msg *BatchTransferWirePrototypeV1) { msg.Outputs[0].ViewTag[0] ^= 1 }},
		{"user policy", func(msg *BatchTransferWirePrototypeV1) {
			msg.Outputs[1].UserPrivacyPolicy = TransferPrivacyPolicyDiscloseToFrom
		}},
		{"user digest", func(msg *BatchTransferWirePrototypeV1) { msg.Outputs[1].UserDisclosureDigest[31] ^= 1 }},
		{"user target", func(msg *BatchTransferWirePrototypeV1) {
			msg.Outputs[1].UserDisclosureTargetPubkey = batchPayloadTestPointBytes(47)
		}},
		{"user payload", func(msg *BatchTransferWirePrototypeV1) {
			msg.Outputs[1].UserDisclosurePayload[len(msg.Outputs[1].UserDisclosurePayload)-1] ^= 1
		}},
		{"full digest", func(msg *BatchTransferWirePrototypeV1) { msg.Outputs[0].FullDisclosureDigest[31] ^= 1 }},
		{"audit payload", func(msg *BatchTransferWirePrototypeV1) {
			msg.Outputs[0].AuditDisclosurePayload[len(msg.Outputs[0].AuditDisclosurePayload)-1] ^= 1
		}},
		{"self-view disabled sentinel", func(msg *BatchTransferWirePrototypeV1) {
			for _, output := range msg.Outputs {
				output.SelfViewDisclosurePayload = nil
			}
		}},
		{"audit key id", func(msg *BatchTransferWirePrototypeV1) { msg.AuditKeyId = "audit-key.production-2" }},
		{"audit key epoch", func(msg *BatchTransferWirePrototypeV1) { msg.AuditKeyEpoch++ }},
		{"audit target", func(msg *BatchTransferWirePrototypeV1) {
			msg.AuditDisclosureTargetPubkey = batchPayloadTestPointBytes(53)
		}},
		{"expiry", func(msg *BatchTransferWirePrototypeV1) { msg.ExpiresAtUnix++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			msg := cloneBatchPayloadTestMessage(t, base)
			test.mutate(msg)
			got, err := ComputeBatchTransferPayloadDigestV1(msg)
			require.NoError(t, err)
			require.NotEqual(t, want, got)
		})
	}
}

func TestBatchTransferWirePrototypeV1PublicDisclosureRecomputesDigest(t *testing.T) {
	msg := batchPayloadTestMessage(t)
	zero := func() *big.Int { return new(big.Int) }
	plaintext := &DisclosurePlaintextV1{
		Plane: DisclosurePlaneUserV1, OutputIndex: 0,
		Policy: TransferPrivacyPolicyDiscloseAmount, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmount,
		Commitment: new(big.Int).SetBytes(msg.Outputs[0].Commitment), Amount: big.NewInt(7), AssetID: ComputeAssetIDV1("uclair"),
		SenderSpendKeyX: zero(), SenderSpendKeyY: zero(), SenderViewKeyX: zero(), SenderViewKeyY: zero(),
		RecipientSpendKeyX: zero(), RecipientSpendKeyY: zero(), RecipientViewKeyX: zero(), RecipientViewKeyY: zero(),
		DisclosureBlinding: big.NewInt(43),
	}
	payload, err := MarshalDisclosurePlaintextV1(plaintext)
	require.NoError(t, err)
	digest, err := ComputeBatchUserDisclosureDigestV1(BatchUserDisclosureV1Input{
		OutputIndex: 0, Commitment: plaintext.Commitment, Policy: plaintext.Policy,
		DisclosedFieldBitmap: plaintext.DisclosedFieldBitmap, SelectedAmount: plaintext.Amount,
		SelectedFromSpendKeyX: plaintext.SenderSpendKeyX, SelectedFromSpendKeyY: plaintext.SenderSpendKeyY,
		SelectedFromViewKeyX: plaintext.SenderViewKeyX, SelectedFromViewKeyY: plaintext.SenderViewKeyY,
		SelectedToSpendKeyX: plaintext.RecipientSpendKeyX, SelectedToSpendKeyY: plaintext.RecipientSpendKeyY,
		SelectedToViewKeyX: plaintext.RecipientViewKeyX, SelectedToViewKeyY: plaintext.RecipientViewKeyY,
		AssetID: plaintext.AssetID, UserDisclosureBlinding: plaintext.DisclosureBlinding,
	})
	require.NoError(t, err)
	msg.Outputs[0].UserPrivacyPolicy = plaintext.Policy
	msg.Outputs[0].UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
	msg.Outputs[0].UserDisclosureDigest = digest.FillBytes(make([]byte, 32))
	msg.Outputs[0].UserDisclosurePayload = payload
	require.NoError(t, ValidateBatchTransferWirePrototypeV1(msg))

	msg.Outputs[0].UserDisclosureDigest[31] ^= 1
	require.ErrorContains(t, ValidateBatchTransferWirePrototypeV1(msg), "does not match plaintext")
}

func batchPayloadTestMessage(t testing.TB) *BatchTransferWirePrototypeV1 {
	t.Helper()
	outputs := []*BatchTransferOutputWirePrototypeV1{
		{
			Commitment: batchPayloadTestField(11), Ciphertext: batchPayloadTestEnvelope(t, EnvelopeTransferNoteV1), ViewTag: []byte{1, 2},
			UserPrivacyPolicy: TransferPrivacyPolicyAllPrivate, UserDisclosureMode: UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			FullDisclosureDigest: batchPayloadTestField(13), AuditDisclosurePayload: batchPayloadTestEnvelope(t, EnvelopeAuditDisclosureV1),
			SelfViewDisclosurePayload: batchPayloadTestEnvelope(t, EnvelopeSelfViewDisclosureV1),
		},
		{
			Commitment: batchPayloadTestField(17), Ciphertext: batchPayloadTestEnvelope(t, EnvelopeTransferNoteV1), ViewTag: []byte{3, 4},
			UserPrivacyPolicy:    TransferPrivacyPolicyDiscloseAmountToFrom,
			UserDisclosureMode:   UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
			UserDisclosureDigest: batchPayloadTestField(19), UserDisclosureTargetPubkey: batchPayloadTestPointBytes(23),
			UserDisclosurePayload: batchPayloadTestEnvelope(t, EnvelopeUserDisclosureV1),
			FullDisclosureDigest:  batchPayloadTestField(29), AuditDisclosurePayload: batchPayloadTestEnvelope(t, EnvelopeAuditDisclosureV1),
			SelfViewDisclosurePayload: batchPayloadTestEnvelope(t, EnvelopeSelfViewDisclosureV1),
		},
	}
	return &BatchTransferWirePrototypeV1{
		Creator: "clair1relayer", Proof: bytes.Repeat([]byte{0xa5}, 164), Root: batchPayloadTestField(31),
		Nullifiers: [][]byte{batchPayloadTestField(5), batchPayloadTestField(7)}, Outputs: outputs,
		AuditKeyId: "audit-key.production-1", AuditKeyEpoch: 9,
		AuditDisclosureTargetPubkey: batchPayloadTestPointBytes(37), ExpiresAtUnix: 2_000_000_000,
	}
}

func productionBatchPayloadTestMessage(t testing.TB) *MsgBatchTransfer {
	t.Helper()
	prototype := batchPayloadTestMessage(t)
	outputs := make([]*BatchTransferOutput, len(prototype.Outputs))
	for i, output := range prototype.Outputs {
		outputs[i] = &BatchTransferOutput{
			Commitment:                 append([]byte(nil), output.Commitment...),
			Ciphertext:                 append([]byte(nil), output.Ciphertext...),
			ViewTag:                    append([]byte(nil), output.ViewTag...),
			UserPrivacyPolicy:          output.UserPrivacyPolicy,
			UserDisclosureMode:         output.UserDisclosureMode,
			UserDisclosureDigest:       append([]byte(nil), output.UserDisclosureDigest...),
			UserDisclosureTargetPubkey: append([]byte(nil), output.UserDisclosureTargetPubkey...),
			UserDisclosurePayload:      append([]byte(nil), output.UserDisclosurePayload...),
			FullDisclosureDigest:       append([]byte(nil), output.FullDisclosureDigest...),
			AuditDisclosurePayload:     append([]byte(nil), output.AuditDisclosurePayload...),
			SelfViewDisclosurePayload:  append([]byte(nil), output.SelfViewDisclosurePayload...),
		}
	}
	return &MsgBatchTransfer{
		Creator:                     prototype.Creator,
		Proof:                       append([]byte(nil), prototype.Proof...),
		Root:                        append([]byte(nil), prototype.Root...),
		Nullifiers:                  cloneBatchPayloadByteSlices(prototype.Nullifiers),
		Outputs:                     outputs,
		AuditKeyId:                  prototype.AuditKeyId,
		AuditKeyEpoch:               prototype.AuditKeyEpoch,
		AuditDisclosureTargetPubkey: append([]byte(nil), prototype.AuditDisclosureTargetPubkey...),
		ExpiresAtUnix:               prototype.ExpiresAtUnix,
	}
}

func maxProductionBatchPayloadTestMessage(t testing.TB) *MsgBatchTransfer {
	t.Helper()
	nullifiers := make([][]byte, BatchJoinSplitV1MaxInputs)
	for i := range nullifiers {
		nullifiers[i] = batchPayloadTestField(int64(i + 1))
	}
	outputs := make([]*BatchTransferOutput, BatchJoinSplitV1MaxOutputs)
	for i := range outputs {
		outputs[i] = &BatchTransferOutput{
			Commitment:                 batchPayloadTestField(int64(100 + i)),
			Ciphertext:                 batchPayloadTestEnvelope(t, EnvelopeTransferNoteV1),
			ViewTag:                    []byte{byte(i), byte(i + 1)},
			UserPrivacyPolicy:          TransferPrivacyPolicyDiscloseAmountToFrom,
			UserDisclosureMode:         UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
			UserDisclosureDigest:       batchPayloadTestField(int64(200 + i)),
			UserDisclosureTargetPubkey: batchPayloadTestPointBytes(23),
			UserDisclosurePayload:      batchPayloadTestEnvelope(t, EnvelopeUserDisclosureV1),
			FullDisclosureDigest:       batchPayloadTestField(int64(300 + i)),
			AuditDisclosurePayload:     batchPayloadTestEnvelope(t, EnvelopeAuditDisclosureV1),
			SelfViewDisclosurePayload:  batchPayloadTestEnvelope(t, EnvelopeSelfViewDisclosureV1),
		}
	}
	return &MsgBatchTransfer{
		Creator:                     "clair1relayer",
		Proof:                       bytes.Repeat([]byte{0xa5}, BatchTransferProofSizeV1),
		Root:                        batchPayloadTestField(99),
		Nullifiers:                  nullifiers,
		Outputs:                     outputs,
		AuditKeyId:                  strings.Repeat("a", AuditKeyIDV1MaxBytes),
		AuditKeyEpoch:               9,
		AuditDisclosureTargetPubkey: batchPayloadTestPointBytes(37),
		ExpiresAtUnix:               2_000_000_000,
	}
}

func cloneBatchPayloadByteSlices(values [][]byte) [][]byte {
	cloned := make([][]byte, len(values))
	for i, value := range values {
		cloned[i] = append([]byte(nil), value...)
	}
	return cloned
}

func cloneBatchPayloadTestMessage(t testing.TB, msg *BatchTransferWirePrototypeV1) *BatchTransferWirePrototypeV1 {
	t.Helper()
	encoded, err := msg.Marshal()
	require.NoError(t, err)
	var clone BatchTransferWirePrototypeV1
	require.NoError(t, clone.Unmarshal(encoded))
	return &clone
}

func batchPayloadTestField(value int64) []byte {
	return big.NewInt(value).FillBytes(make([]byte, 32))
}

func batchPayloadTestPointBytes(scalar int64) []byte {
	point := noteV1TestPoint(big.NewInt(scalar))
	encoded := point.Bytes()
	return append([]byte(nil), encoded[:]...)
}

func batchPayloadTestEnvelope(t testing.TB, kind EncryptedEnvelopeKindV1) []byte {
	t.Helper()
	size, err := EncryptedEnvelopeV1Size(kind)
	require.NoError(t, err)
	encoded, err := WrapEncryptedEnvelopeV1(kind, make([]byte, size-EncryptedEnvelopeV1HeaderSize))
	require.NoError(t, err)
	return encoded
}

// This deliberately does not call the production framing helpers.
func referenceCanonicalBatchTransferPayloadBytesV1(t *testing.T, msg *BatchTransferWirePrototypeV1) []byte {
	t.Helper()
	var encoded bytes.Buffer
	writeU32 := func(value uint32) {
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], value)
		_, _ = encoded.Write(raw[:])
	}
	writeU64 := func(value uint64) {
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], value)
		_, _ = encoded.Write(raw[:])
	}
	writeBytes := func(value []byte) {
		writeU32(uint32(len(value)))
		_, _ = encoded.Write(value)
	}

	writeU32(1)
	writeBytes(msg.Root)
	writeU32(uint32(len(msg.Nullifiers)))
	for _, nullifier := range msg.Nullifiers {
		writeBytes(nullifier)
	}
	writeU32(uint32(len(msg.Outputs)))
	for _, output := range msg.Outputs {
		writeBytes(output.Commitment)
		writeBytes(output.Ciphertext)
		writeBytes(output.ViewTag)
		writeU32(output.UserPrivacyPolicy)
		writeU32(uint32(output.UserDisclosureMode))
		writeBytes(output.UserDisclosureDigest)
		writeBytes(output.UserDisclosureTargetPubkey)
		writeBytes(output.UserDisclosurePayload)
		writeBytes(output.FullDisclosureDigest)
		writeBytes(output.AuditDisclosurePayload)
		writeBytes(output.SelfViewDisclosurePayload)
	}
	writeBytes([]byte(msg.AuditKeyId))
	writeU64(msg.AuditKeyEpoch)
	writeBytes(msg.AuditDisclosureTargetPubkey)
	writeU64(uint64(msg.ExpiresAtUnix))
	return encoded.Bytes()
}
