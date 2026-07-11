package types

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

func TestNotePlaintextV1FixedRoundTripAndGolden(t *testing.T) {
	note := fixedPayloadTestNote()
	encoded, err := MarshalNotePlaintextV1(note)
	require.NoError(t, err)
	require.Len(t, encoded, NotePlaintextV1Size)
	decoded, err := UnmarshalNotePlaintextV1(encoded)
	require.NoError(t, err)
	require.Equal(t, note.ComputeCommitment().String(), decoded.ComputeCommitment().String())
	require.Equal(t, note.ComputeNullifier().String(), decoded.ComputeNullifier().String())
	require.Equal(t, note.Memo, decoded.Memo)

	digest := sha256.Sum256(encoded)
	const goldenSHA256 = "370bba3105acbcc05c14d158cdaffdaa2526fd13dfe1026e9e396e369527b626"
	require.Equal(t, goldenSHA256, hex.EncodeToString(digest[:]))
}

func TestNotePlaintextV1RejectsMalformedEncoding(t *testing.T) {
	encoded, err := MarshalNotePlaintextV1(fixedPayloadTestNote())
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		want   string
	}{
		{name: "truncated", mutate: func(v []byte) []byte { return v[:len(v)-1] }, want: "exactly"},
		{name: "extended", mutate: func(v []byte) []byte { return append(v, 0) }, want: "exactly"},
		{name: "wrong domain", mutate: func(v []byte) []byte { v[0] ^= 1; return v }, want: "domain"},
		{name: "reserved", mutate: func(v []byte) []byte { v[18] = 1; return v }, want: "reserved"},
		{name: "memo padding", mutate: func(v []byte) []byte { v[len(v)-1] = 1; return v }, want: "padding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := tc.mutate(append([]byte(nil), encoded...))
			_, err := UnmarshalNotePlaintextV1(mutated)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestDisclosurePlaintextV1FixedRoundTripAndGolden(t *testing.T) {
	note := fixedPayloadTestNote()
	payload := &DisclosurePlaintextV1{
		Plane: DisclosurePlaneFullV1, OutputIndex: 7,
		Policy: DisclosureFullMarkerV1, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmountToFrom,
		Commitment: note.ComputeCommitment(), Amount: note.Amount, AssetID: note.AssetID,
		SenderSpendKeyX: note.ReceiverSpendPubKeyX, SenderSpendKeyY: note.ReceiverSpendPubKeyY,
		SenderViewKeyX: note.ReceiverViewPubKeyX, SenderViewKeyY: note.ReceiverViewPubKeyY,
		RecipientSpendKeyX: note.ReceiverSpendPubKeyX, RecipientSpendKeyY: note.ReceiverSpendPubKeyY,
		RecipientViewKeyX: note.ReceiverViewPubKeyX, RecipientViewKeyY: note.ReceiverViewPubKeyY,
		DisclosureBlinding: big.NewInt(47),
	}
	encoded, err := MarshalDisclosurePlaintextV1(payload)
	require.NoError(t, err)
	require.Len(t, encoded, DisclosurePlaintextV1Size)
	decoded, err := UnmarshalDisclosurePlaintextV1(encoded)
	require.NoError(t, err)
	require.Equal(t, payload.OutputIndex, decoded.OutputIndex)
	require.Equal(t, payload.Commitment.String(), decoded.Commitment.String())
	require.Equal(t, payload.DisclosureBlinding.String(), decoded.DisclosureBlinding.String())

	digest := sha256.Sum256(encoded)
	const goldenSHA256 = "ed8eed29ced0945a5aa9fc1c4d9d499ca7b01a4fffbc0ff449f71fc7c799df38"
	require.Equal(t, goldenSHA256, hex.EncodeToString(digest[:]))
}

func TestDisclosurePlaintextV1RequiresAssetForEveryDisclosurePolicy(t *testing.T) {
	note := fixedPayloadTestNote()
	payload := &DisclosurePlaintextV1{
		Plane: DisclosurePlaneUserV1, OutputIndex: 0,
		Policy: TransferPrivacyPolicyDiscloseTo, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseTo,
		Commitment: note.ComputeCommitment(), Amount: new(big.Int), AssetID: new(big.Int),
		SenderSpendKeyX: new(big.Int), SenderSpendKeyY: new(big.Int),
		SenderViewKeyX: new(big.Int), SenderViewKeyY: new(big.Int),
		RecipientSpendKeyX: note.ReceiverSpendPubKeyX, RecipientSpendKeyY: note.ReceiverSpendPubKeyY,
		RecipientViewKeyX: note.ReceiverViewPubKeyX, RecipientViewKeyY: note.ReceiverViewPubKeyY,
		DisclosureBlinding: big.NewInt(47),
	}
	_, err := MarshalDisclosurePlaintextV1(payload)
	require.ErrorContains(t, err, "disclosure asset id must be non-zero")
	payload.AssetID = note.AssetID
	_, err = MarshalDisclosurePlaintextV1(payload)
	require.NoError(t, err)
}

func TestDisclosurePlaintextV1RejectsInvalidDisclosedKey(t *testing.T) {
	note := fixedPayloadTestNote()
	payload := &DisclosurePlaintextV1{
		Plane: DisclosurePlaneFullV1, OutputIndex: 0,
		Policy: DisclosureFullMarkerV1, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmountToFrom,
		Commitment: note.ComputeCommitment(), Amount: note.Amount, AssetID: note.AssetID,
		SenderSpendKeyX: note.ReceiverSpendPubKeyX, SenderSpendKeyY: note.ReceiverSpendPubKeyY,
		SenderViewKeyX: note.ReceiverViewPubKeyX, SenderViewKeyY: note.ReceiverViewPubKeyY,
		RecipientSpendKeyX: big.NewInt(0), RecipientSpendKeyY: big.NewInt(1),
		RecipientViewKeyX: note.ReceiverViewPubKeyX, RecipientViewKeyY: note.ReceiverViewPubKeyY,
		DisclosureBlinding: big.NewInt(47),
	}
	_, err := MarshalDisclosurePlaintextV1(payload)
	require.ErrorContains(t, err, "identity point is not allowed")
}

func TestEncryptedEnvelopeV1ExactFraming(t *testing.T) {
	for _, tc := range []struct {
		kind EncryptedEnvelopeKindV1
	}{
		{EnvelopeDepositNoteV1}, {EnvelopeTransferNoteV1}, {EnvelopeUserDisclosureV1},
		{EnvelopeAuditDisclosureV1}, {EnvelopeSelfViewDisclosureV1},
	} {
		size, err := encryptedCiphertextSizeV1(tc.kind)
		require.NoError(t, err)
		cipherText := make([]byte, size)
		wrapped, err := WrapEncryptedEnvelopeV1(tc.kind, cipherText)
		require.NoError(t, err)
		expectedSize, err := EncryptedEnvelopeV1Size(tc.kind)
		require.NoError(t, err)
		require.Len(t, wrapped, expectedSize)
		unwrapped, err := UnwrapEncryptedEnvelopeV1(wrapped, tc.kind)
		require.NoError(t, err)
		require.Equal(t, cipherText, unwrapped)

		_, err = UnwrapEncryptedEnvelopeV1(wrapped[:len(wrapped)-1], tc.kind)
		require.ErrorContains(t, err, "exactly")
		wrongReserved := append([]byte(nil), wrapped...)
		wrongReserved[19] = 1
		_, err = UnwrapEncryptedEnvelopeV1(wrongReserved, tc.kind)
		require.ErrorContains(t, err, "reserved")
	}
}

func fixedPayloadTestNote() *Note {
	curve := crypto_tedwards.GetEdwardsCurve()
	var spendKey, viewKey crypto_tedwards.PointAffine
	spendKey.ScalarMultiplication(&curve.Base, big.NewInt(17))
	viewKey.ScalarMultiplication(&curve.Base, big.NewInt(19))
	spendX, spendY := new(big.Int), new(big.Int)
	viewX, viewY := new(big.Int), new(big.Int)
	spendKey.X.BigInt(spendX)
	spendKey.Y.BigInt(spendY)
	viewKey.X.BigInt(viewX)
	viewKey.Y.BigInt(viewY)
	return &Note{
		ReceiverSpendPubKeyX: spendX, ReceiverSpendPubKeyY: spendY,
		ReceiverViewPubKeyX: viewX, ReceiverViewPubKeyY: viewY,
		Amount: big.NewInt(123), AssetID: ComputeAssetIDV1("uclair"),
		Randomness: big.NewInt(29), Memo: "fixed-v1",
	}
}
