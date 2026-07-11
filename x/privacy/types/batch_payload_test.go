package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
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

func batchPayloadTestMessage(t *testing.T) *BatchTransferWirePrototypeV1 {
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

func cloneBatchPayloadTestMessage(t *testing.T, msg *BatchTransferWirePrototypeV1) *BatchTransferWirePrototypeV1 {
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

func batchPayloadTestEnvelope(t *testing.T, kind EncryptedEnvelopeKindV1) []byte {
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
