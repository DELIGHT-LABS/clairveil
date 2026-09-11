package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/stretchr/testify/require"
)

func secretField(t *testing.T, value *big.Int) privacycrypto.FieldValue {
	t.Helper()
	encoded := value.FillBytes(make([]byte, 32))
	field, err := privacycrypto.ParseFieldValueBE32(encoded)
	require.NoError(t, err)
	return field
}

func secretFieldBytes(value privacycrypto.FieldValue) []byte {
	encoded := value.Bytes()
	return append([]byte(nil), encoded[:]...)
}

func TestSecretNoteV1LegacyMiMCDifferential(t *testing.T) {
	spendX, spendY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(17)))
	viewX, viewY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(19)))
	assetID := ComputeAssetIDV1("uclair")
	note := SecretNoteV1{
		ReceiverSpendPubKeyX: secretField(t, spendX), ReceiverSpendPubKeyY: secretField(t, spendY),
		ReceiverViewPubKeyX: secretField(t, viewX), ReceiverViewPubKeyY: secretField(t, viewY),
		Amount: 7, AssetID: secretField(t, assetID), Randomness: privacycrypto.FieldValueFromUint64(13),
	}
	commitment, err := SecretNoteCommitmentV1(note)
	require.NoError(t, err)
	nullifier, err := SecretNoteNullifierV1(note, commitment)
	require.NoError(t, err)

	publicCommitment := ComputeNoteCommitmentV1(spendX, spendY, viewX, viewY, big.NewInt(7), assetID, big.NewInt(13))
	publicNullifier := ComputeNoteNullifierV1(publicCommitment, big.NewInt(13), spendX, spendY)
	require.Equal(t, publicCommitment.FillBytes(make([]byte, 32)), secretFieldBytes(commitment))
	require.Equal(t, publicNullifier.FillBytes(make([]byte, 32)), secretFieldBytes(nullifier))
}

func TestSecretDisclosureLegacyMiMCDifferential(t *testing.T) {
	commitment := secretField(t, big.NewInt(101))
	input := SecretTransferDisclosureV1Input{
		Policy: TransferPrivacyPolicyDiscloseAmountToFrom, OutputIndex: TransferDisclosureRecipientOutputIndex,
		Commitment: commitment, Amount: 10, AssetID: privacycrypto.FieldValueFromUint64(7),
		FromSpendPubKeyX: privacycrypto.FieldValueFromUint64(17), FromSpendPubKeyY: privacycrypto.FieldValueFromUint64(19),
		FromViewPubKeyX: privacycrypto.FieldValueFromUint64(23), FromViewPubKeyY: privacycrypto.FieldValueFromUint64(29),
		ToSpendPubKeyX: privacycrypto.FieldValueFromUint64(11), ToSpendPubKeyY: privacycrypto.FieldValueFromUint64(13),
		ToViewPubKeyX: privacycrypto.FieldValueFromUint64(31), ToViewPubKeyY: privacycrypto.FieldValueFromUint64(37),
		Blinding: privacycrypto.FieldValueFromUint64(41),
	}
	secretDigest, err := SecretTransferDisclosureDigestV1(input)
	require.NoError(t, err)
	publicDigest, err := ComputeTransferDisclosureDigestBytes(
		input.Policy, input.OutputIndex, secretFieldBytes(input.Commitment), big.NewInt(int64(input.Amount)), big.NewInt(7),
		big.NewInt(17), big.NewInt(19), big.NewInt(23), big.NewInt(29),
		big.NewInt(11), big.NewInt(13), big.NewInt(31), big.NewInt(37), big.NewInt(41),
	)
	require.NoError(t, err)
	require.Equal(t, publicDigest, secretFieldBytes(secretDigest))

	full, err := SecretFullTransferDisclosureDigestV1(SecretFullTransferDisclosureV1Input{
		OutputIndex: input.OutputIndex, Commitment: input.Commitment, Amount: input.Amount, AssetID: input.AssetID,
		FromSpendPubKeyX: input.FromSpendPubKeyX, FromSpendPubKeyY: input.FromSpendPubKeyY, FromViewPubKeyX: input.FromViewPubKeyX, FromViewPubKeyY: input.FromViewPubKeyY,
		ToSpendPubKeyX: input.ToSpendPubKeyX, ToSpendPubKeyY: input.ToSpendPubKeyY, ToViewPubKeyX: input.ToViewPubKeyX, ToViewPubKeyY: input.ToViewPubKeyY, Blinding: input.Blinding,
	})
	require.NoError(t, err)
	publicFull, err := ComputeFullTransferDisclosureDigestBytes(
		input.OutputIndex, secretFieldBytes(input.Commitment), big.NewInt(int64(input.Amount)), big.NewInt(7),
		big.NewInt(17), big.NewInt(19), big.NewInt(23), big.NewInt(29), big.NewInt(11), big.NewInt(13), big.NewInt(31), big.NewInt(37), big.NewInt(41),
	)
	require.NoError(t, err)
	require.Equal(t, publicFull, secretFieldBytes(full))
}

func TestSecretBatchDisclosureAllPoliciesDifferential(t *testing.T) {
	zero := privacycrypto.FieldValueFromUint64(0)
	fromSpendX, fromSpendY := secretPoint(t, 17)
	fromViewX, fromViewY := secretPoint(t, 19)
	toSpendX, toSpendY := secretPoint(t, 23)
	toViewX, toViewY := secretPoint(t, 29)
	for policy := uint32(0); policy <= TransferPrivacyPolicyDiscloseAmountToFrom; policy++ {
		t.Run(fmt.Sprintf("policy-%d", policy), func(t *testing.T) {
			selectedAmount := uint64(0)
			if policy&TransferPrivacyPolicyDiscloseAmount != 0 {
				selectedAmount = 7
			}
			fromX, fromY, fromVX, fromVY := zero, zero, zero, zero
			if policy&TransferPrivacyPolicyDiscloseFrom != 0 {
				fromX, fromY, fromVX, fromVY = fromSpendX, fromSpendY, fromViewX, fromViewY
			}
			toX, toY, toVX, toVY := zero, zero, zero, zero
			if policy&TransferPrivacyPolicyDiscloseTo != 0 {
				toX, toY, toVX, toVY = toSpendX, toSpendY, toViewX, toViewY
			}
			secretInput := SecretBatchUserDisclosureV1Input{
				OutputIndex: 3, Commitment: privacycrypto.FieldValueFromUint64(101), Policy: policy, DisclosedFieldBitmap: policy,
				SelectedAmount: selectedAmount, SelectedFromSpendX: fromX, SelectedFromSpendY: fromY, SelectedFromViewX: fromVX, SelectedFromViewY: fromVY,
				SelectedToSpendX: toX, SelectedToSpendY: toY, SelectedToViewX: toVX, SelectedToViewY: toVY,
				AssetID: func() privacycrypto.FieldValue {
					if policy == 0 {
						return zero
					}
					return privacycrypto.FieldValueFromUint64(7)
				}(),
				Blinding: func() privacycrypto.FieldValue {
					if policy == 0 {
						return zero
					}
					return privacycrypto.FieldValueFromUint64(43)
				}(),
			}
			got, err := SecretBatchUserDisclosureDigestV1(secretInput)
			require.NoError(t, err)
			publicInput := BatchUserDisclosureV1Input{
				OutputIndex: 3, Commitment: big.NewInt(101), Policy: policy, DisclosedFieldBitmap: policy,
				SelectedAmount:        new(big.Int).SetUint64(selectedAmount),
				SelectedFromSpendKeyX: fieldBig(fromX), SelectedFromSpendKeyY: fieldBig(fromY), SelectedFromViewKeyX: fieldBig(fromVX), SelectedFromViewKeyY: fieldBig(fromVY),
				SelectedToSpendKeyX: fieldBig(toX), SelectedToSpendKeyY: fieldBig(toY), SelectedToViewKeyX: fieldBig(toVX), SelectedToViewKeyY: fieldBig(toVY),
				AssetID: fieldBig(secretInput.AssetID), UserDisclosureBlinding: fieldBig(secretInput.Blinding),
			}
			want, err := ComputeBatchUserDisclosureDigestV1(publicInput)
			require.NoError(t, err)
			require.Equal(t, want.FillBytes(make([]byte, 32)), secretFieldBytes(got))
		})
	}
}

func TestSecretBatchFullDisclosureDifferential(t *testing.T) {
	senderSpendX, senderSpendY := secretPoint(t, 17)
	senderViewX, senderViewY := secretPoint(t, 19)
	recipientSpendX, recipientSpendY := secretPoint(t, 23)
	recipientViewX, recipientViewY := secretPoint(t, 29)
	input := SecretBatchFullDisclosureV1Input{
		OutputIndex: 3, Commitment: privacycrypto.FieldValueFromUint64(101), Amount: 7, AssetID: privacycrypto.FieldValueFromUint64(7),
		SenderSpendX: senderSpendX, SenderSpendY: senderSpendY, SenderViewX: senderViewX, SenderViewY: senderViewY,
		RecipientSpendX: recipientSpendX, RecipientSpendY: recipientSpendY, RecipientViewX: recipientViewX, RecipientViewY: recipientViewY, Blinding: privacycrypto.FieldValueFromUint64(43),
	}
	got, err := SecretBatchFullDisclosureDigestV1(input)
	require.NoError(t, err)
	want, err := ComputeBatchFullDisclosureDigestV1(BatchFullDisclosureV1Input{
		OutputIndex: input.OutputIndex, Commitment: fieldBig(input.Commitment), Amount: new(big.Int).SetUint64(input.Amount), AssetID: fieldBig(input.AssetID),
		SenderSpendKeyX: fieldBig(input.SenderSpendX), SenderSpendKeyY: fieldBig(input.SenderSpendY), SenderViewKeyX: fieldBig(input.SenderViewX), SenderViewKeyY: fieldBig(input.SenderViewY),
		RecipientSpendKeyX: fieldBig(input.RecipientSpendX), RecipientSpendKeyY: fieldBig(input.RecipientSpendY), RecipientViewKeyX: fieldBig(input.RecipientViewX), RecipientViewKeyY: fieldBig(input.RecipientViewY), FullDisclosureBlinding: fieldBig(input.Blinding),
	})
	require.NoError(t, err)
	require.Equal(t, want.FillBytes(make([]byte, 32)), secretFieldBytes(got))
}

func TestSecretTransferDisclosureValidationParity(t *testing.T) {
	base := SecretTransferDisclosureV1Input{
		Policy: TransferPrivacyPolicyDiscloseAmountToFrom, OutputIndex: TransferDisclosureRecipientOutputIndex,
		Commitment: privacycrypto.FieldValueFromUint64(101), Amount: 10, AssetID: privacycrypto.FieldValueFromUint64(7),
		FromSpendPubKeyX: privacycrypto.FieldValueFromUint64(17), FromSpendPubKeyY: privacycrypto.FieldValueFromUint64(19),
		FromViewPubKeyX: privacycrypto.FieldValueFromUint64(23), FromViewPubKeyY: privacycrypto.FieldValueFromUint64(29),
		ToSpendPubKeyX: privacycrypto.FieldValueFromUint64(11), ToSpendPubKeyY: privacycrypto.FieldValueFromUint64(13),
		ToViewPubKeyX: privacycrypto.FieldValueFromUint64(31), ToViewPubKeyY: privacycrypto.FieldValueFromUint64(37), Blinding: privacycrypto.FieldValueFromUint64(41),
	}
	zero := privacycrypto.FieldValueFromUint64(0)
	for _, tc := range []struct {
		name   string
		mutate func(*SecretTransferDisclosureV1Input)
	}{
		{"valid", func(*SecretTransferDisclosureV1Input) {}},
		{"all-private permits zero asset and blinding", func(input *SecretTransferDisclosureV1Input) {
			input.Policy, input.AssetID, input.Blinding = TransferPrivacyPolicyAllPrivate, zero, zero
		}},
		{"enabled requires asset", func(input *SecretTransferDisclosureV1Input) { input.AssetID = zero }},
		{"enabled requires blinding", func(input *SecretTransferDisclosureV1Input) { input.Blinding = zero }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.mutate(&input)
			got, gotErr := SecretTransferDisclosureDigestV1(input)
			want, wantErr := legacyTransferDisclosureDigest(input)
			require.Equal(t, wantErr != nil, gotErr != nil)
			if wantErr == nil {
				require.Equal(t, want, secretFieldBytes(got))
			}
		})
	}
}

func TestSecretFullTransferDisclosureValidationParity(t *testing.T) {
	base := SecretFullTransferDisclosureV1Input{
		OutputIndex: TransferDisclosureRecipientOutputIndex, Commitment: privacycrypto.FieldValueFromUint64(101), Amount: 10, AssetID: privacycrypto.FieldValueFromUint64(7),
		FromSpendPubKeyX: privacycrypto.FieldValueFromUint64(17), FromSpendPubKeyY: privacycrypto.FieldValueFromUint64(19),
		FromViewPubKeyX: privacycrypto.FieldValueFromUint64(23), FromViewPubKeyY: privacycrypto.FieldValueFromUint64(29),
		ToSpendPubKeyX: privacycrypto.FieldValueFromUint64(11), ToSpendPubKeyY: privacycrypto.FieldValueFromUint64(13),
		ToViewPubKeyX: privacycrypto.FieldValueFromUint64(31), ToViewPubKeyY: privacycrypto.FieldValueFromUint64(37), Blinding: privacycrypto.FieldValueFromUint64(41),
	}
	zero := privacycrypto.FieldValueFromUint64(0)
	for _, tc := range []struct {
		name   string
		mutate func(*SecretFullTransferDisclosureV1Input)
	}{
		{"valid", func(*SecretFullTransferDisclosureV1Input) {}},
		{"zero asset remains valid", func(input *SecretFullTransferDisclosureV1Input) { input.AssetID = zero }},
		{"non-curve keys remain valid", func(input *SecretFullTransferDisclosureV1Input) {
			input.FromSpendPubKeyX = privacycrypto.FieldValueFromUint64(1)
		}},
		{"requires blinding", func(input *SecretFullTransferDisclosureV1Input) { input.Blinding = zero }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.mutate(&input)
			got, gotErr := SecretFullTransferDisclosureDigestV1(input)
			want, wantErr := legacyFullTransferDisclosureDigest(input)
			require.Equal(t, wantErr != nil, gotErr != nil)
			if wantErr == nil {
				require.Equal(t, want, secretFieldBytes(got))
			}
		})
	}
}

func TestSecretBatchUserDisclosureValidationParity(t *testing.T) {
	zero := privacycrypto.FieldValueFromUint64(0)
	fromSpendX, fromSpendY := secretPoint(t, 17)
	fromViewX, fromViewY := secretPoint(t, 19)
	toSpendX, toSpendY := secretPoint(t, 23)
	toViewX, toViewY := secretPoint(t, 29)
	base := SecretBatchUserDisclosureV1Input{
		OutputIndex: 3, Commitment: privacycrypto.FieldValueFromUint64(101), Policy: TransferPrivacyPolicyDiscloseAmountToFrom,
		DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmountToFrom, SelectedAmount: 7,
		SelectedFromSpendX: fromSpendX, SelectedFromSpendY: fromSpendY, SelectedFromViewX: fromViewX, SelectedFromViewY: fromViewY,
		SelectedToSpendX: toSpendX, SelectedToSpendY: toSpendY, SelectedToViewX: toViewX, SelectedToViewY: toViewY,
		AssetID: privacycrypto.FieldValueFromUint64(7), Blinding: privacycrypto.FieldValueFromUint64(43),
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SecretBatchUserDisclosureV1Input)
	}{
		{"valid", func(*SecretBatchUserDisclosureV1Input) {}},
		{"all-private valid sentinel", func(input *SecretBatchUserDisclosureV1Input) {
			*input = SecretBatchUserDisclosureV1Input{OutputIndex: input.OutputIndex, Commitment: input.Commitment}
		}},
		{"all-private rejects selected asset", func(input *SecretBatchUserDisclosureV1Input) {
			*input = SecretBatchUserDisclosureV1Input{OutputIndex: input.OutputIndex, Commitment: input.Commitment, AssetID: privacycrypto.FieldValueFromUint64(7)}
		}},
		{"all-private rejects nonzero blinding", func(input *SecretBatchUserDisclosureV1Input) {
			*input = SecretBatchUserDisclosureV1Input{OutputIndex: input.OutputIndex, Commitment: input.Commitment, Blinding: privacycrypto.FieldValueFromUint64(43)}
		}},
		{"requires active commitment", func(input *SecretBatchUserDisclosureV1Input) { input.Commitment = zero }},
		{"requires active asset", func(input *SecretBatchUserDisclosureV1Input) { input.AssetID = zero }},
		{"requires active blinding", func(input *SecretBatchUserDisclosureV1Input) { input.Blinding = zero }},
		{"hidden amount must be zero", func(input *SecretBatchUserDisclosureV1Input) {
			input.Policy, input.DisclosedFieldBitmap, input.SelectedAmount = TransferPrivacyPolicyDiscloseFrom, TransferPrivacyPolicyDiscloseFrom, 7
			input.SelectedToSpendX, input.SelectedToSpendY, input.SelectedToViewX, input.SelectedToViewY = zero, zero, zero, zero
		}},
		{"hidden sender keys must be zero", func(input *SecretBatchUserDisclosureV1Input) {
			input.Policy, input.DisclosedFieldBitmap, input.SelectedAmount = TransferPrivacyPolicyDiscloseAmount, TransferPrivacyPolicyDiscloseAmount, 7
			input.SelectedToSpendX, input.SelectedToSpendY, input.SelectedToViewX, input.SelectedToViewY = zero, zero, zero, zero
		}},
		{"disclosed keys must be curve points", func(input *SecretBatchUserDisclosureV1Input) {
			input.Policy, input.DisclosedFieldBitmap, input.SelectedAmount = TransferPrivacyPolicyDiscloseFrom, TransferPrivacyPolicyDiscloseFrom, 0
			input.SelectedFromSpendX, input.SelectedFromSpendY, input.SelectedFromViewX, input.SelectedFromViewY = privacycrypto.FieldValueFromUint64(1), privacycrypto.FieldValueFromUint64(2), privacycrypto.FieldValueFromUint64(3), privacycrypto.FieldValueFromUint64(4)
			input.SelectedToSpendX, input.SelectedToSpendY, input.SelectedToViewX, input.SelectedToViewY = zero, zero, zero, zero
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.mutate(&input)
			got, gotErr := SecretBatchUserDisclosureDigestV1(input)
			want, wantErr := ComputeBatchUserDisclosureDigestV1(legacyBatchUserDisclosureInput(input))
			require.Equal(t, wantErr != nil, gotErr != nil)
			if wantErr == nil {
				require.Equal(t, want.FillBytes(make([]byte, 32)), secretFieldBytes(got))
			}
		})
	}
}

func TestSecretBatchFullDisclosureValidationParity(t *testing.T) {
	zero := privacycrypto.FieldValueFromUint64(0)
	senderSpendX, senderSpendY := secretPoint(t, 17)
	senderViewX, senderViewY := secretPoint(t, 19)
	recipientSpendX, recipientSpendY := secretPoint(t, 23)
	recipientViewX, recipientViewY := secretPoint(t, 29)
	base := SecretBatchFullDisclosureV1Input{
		OutputIndex: 3, Commitment: privacycrypto.FieldValueFromUint64(101), Amount: 7, AssetID: privacycrypto.FieldValueFromUint64(7),
		SenderSpendX: senderSpendX, SenderSpendY: senderSpendY, SenderViewX: senderViewX, SenderViewY: senderViewY,
		RecipientSpendX: recipientSpendX, RecipientSpendY: recipientSpendY, RecipientViewX: recipientViewX, RecipientViewY: recipientViewY,
		Blinding: privacycrypto.FieldValueFromUint64(43),
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SecretBatchFullDisclosureV1Input)
	}{
		{"valid", func(*SecretBatchFullDisclosureV1Input) {}},
		{"requires commitment", func(input *SecretBatchFullDisclosureV1Input) { input.Commitment = zero }},
		{"requires asset", func(input *SecretBatchFullDisclosureV1Input) { input.AssetID = zero }},
		{"requires blinding", func(input *SecretBatchFullDisclosureV1Input) { input.Blinding = zero }},
		{"requires curve points", func(input *SecretBatchFullDisclosureV1Input) {
			input.SenderSpendX = privacycrypto.FieldValueFromUint64(1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.mutate(&input)
			got, gotErr := SecretBatchFullDisclosureDigestV1(input)
			want, wantErr := ComputeBatchFullDisclosureDigestV1(legacyBatchFullDisclosureInput(input))
			require.Equal(t, wantErr != nil, gotErr != nil)
			if wantErr == nil {
				require.Equal(t, want.FillBytes(make([]byte, 32)), secretFieldBytes(got))
			}
		})
	}
}

func legacyTransferDisclosureDigest(input SecretTransferDisclosureV1Input) ([]byte, error) {
	return ComputeTransferDisclosureDigestBytes(
		input.Policy, input.OutputIndex, secretFieldBytes(input.Commitment), new(big.Int).SetUint64(input.Amount), fieldBig(input.AssetID),
		fieldBig(input.FromSpendPubKeyX), fieldBig(input.FromSpendPubKeyY), fieldBig(input.FromViewPubKeyX), fieldBig(input.FromViewPubKeyY),
		fieldBig(input.ToSpendPubKeyX), fieldBig(input.ToSpendPubKeyY), fieldBig(input.ToViewPubKeyX), fieldBig(input.ToViewPubKeyY), fieldBig(input.Blinding),
	)
}

func legacyFullTransferDisclosureDigest(input SecretFullTransferDisclosureV1Input) ([]byte, error) {
	return ComputeFullTransferDisclosureDigestBytes(
		input.OutputIndex, secretFieldBytes(input.Commitment), new(big.Int).SetUint64(input.Amount), fieldBig(input.AssetID),
		fieldBig(input.FromSpendPubKeyX), fieldBig(input.FromSpendPubKeyY), fieldBig(input.FromViewPubKeyX), fieldBig(input.FromViewPubKeyY),
		fieldBig(input.ToSpendPubKeyX), fieldBig(input.ToSpendPubKeyY), fieldBig(input.ToViewPubKeyX), fieldBig(input.ToViewPubKeyY), fieldBig(input.Blinding),
	)
}

func legacyBatchUserDisclosureInput(input SecretBatchUserDisclosureV1Input) BatchUserDisclosureV1Input {
	return BatchUserDisclosureV1Input{
		OutputIndex: input.OutputIndex, Commitment: fieldBig(input.Commitment), Policy: input.Policy, DisclosedFieldBitmap: input.DisclosedFieldBitmap,
		SelectedAmount:        new(big.Int).SetUint64(input.SelectedAmount),
		SelectedFromSpendKeyX: fieldBig(input.SelectedFromSpendX), SelectedFromSpendKeyY: fieldBig(input.SelectedFromSpendY),
		SelectedFromViewKeyX: fieldBig(input.SelectedFromViewX), SelectedFromViewKeyY: fieldBig(input.SelectedFromViewY),
		SelectedToSpendKeyX: fieldBig(input.SelectedToSpendX), SelectedToSpendKeyY: fieldBig(input.SelectedToSpendY),
		SelectedToViewKeyX: fieldBig(input.SelectedToViewX), SelectedToViewKeyY: fieldBig(input.SelectedToViewY),
		AssetID: fieldBig(input.AssetID), UserDisclosureBlinding: fieldBig(input.Blinding),
	}
}

func legacyBatchFullDisclosureInput(input SecretBatchFullDisclosureV1Input) BatchFullDisclosureV1Input {
	return BatchFullDisclosureV1Input{
		OutputIndex: input.OutputIndex, Commitment: fieldBig(input.Commitment), Amount: new(big.Int).SetUint64(input.Amount), AssetID: fieldBig(input.AssetID),
		SenderSpendKeyX: fieldBig(input.SenderSpendX), SenderSpendKeyY: fieldBig(input.SenderSpendY),
		SenderViewKeyX: fieldBig(input.SenderViewX), SenderViewKeyY: fieldBig(input.SenderViewY),
		RecipientSpendKeyX: fieldBig(input.RecipientSpendX), RecipientSpendKeyY: fieldBig(input.RecipientSpendY),
		RecipientViewKeyX: fieldBig(input.RecipientViewX), RecipientViewKeyY: fieldBig(input.RecipientViewY),
		FullDisclosureBlinding: fieldBig(input.Blinding),
	}
}

func fieldBig(value privacycrypto.FieldValue) *big.Int {
	encoded := value.Bytes()
	return new(big.Int).SetBytes(encoded[:])
}

func secretPoint(t *testing.T, scalar int64) (privacycrypto.FieldValue, privacycrypto.FieldValue) {
	t.Helper()
	x, y := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(scalar)))
	return secretField(t, x), secretField(t, y)
}

func TestUnmarshalSecretNotePlaintextV1UsesFixedTransport(t *testing.T) {
	note := fixedPayloadTestNote()
	encoded, err := MarshalNotePlaintextV1(note)
	require.NoError(t, err)
	secret, err := UnmarshalSecretNotePlaintextV1(encoded)
	require.NoError(t, err)
	require.Equal(t, uint64(123), secret.Amount)
	roundTrip, err := MarshalSecretNotePlaintextV1(secret)
	require.NoError(t, err)
	require.Equal(t, encoded, roundTrip)
	commitment, err := SecretNoteCommitmentV1(*secret)
	require.NoError(t, err)
	require.Equal(t, note.ComputeCommitment().FillBytes(make([]byte, 32)), secretFieldBytes(commitment))
}

func TestSecretDisclosurePlaintextV1WireRoundTrip(t *testing.T) {
	note := fixedPayloadTestNote()
	payload := &DisclosurePlaintextV1{
		Plane: DisclosurePlaneFullV1, OutputIndex: 7, Policy: DisclosureFullMarkerV1, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmountToFrom,
		Commitment: note.ComputeCommitment(), Amount: note.Amount, AssetID: note.AssetID,
		SenderSpendKeyX: note.ReceiverSpendPubKeyX, SenderSpendKeyY: note.ReceiverSpendPubKeyY, SenderViewKeyX: note.ReceiverViewPubKeyX, SenderViewKeyY: note.ReceiverViewPubKeyY,
		RecipientSpendKeyX: note.ReceiverSpendPubKeyX, RecipientSpendKeyY: note.ReceiverSpendPubKeyY, RecipientViewKeyX: note.ReceiverViewPubKeyX, RecipientViewKeyY: note.ReceiverViewPubKeyY, DisclosureBlinding: big.NewInt(47),
	}
	encoded, err := MarshalDisclosurePlaintextV1(payload)
	require.NoError(t, err)
	secret, err := UnmarshalSecretDisclosurePlaintextV1(encoded)
	require.NoError(t, err)
	roundTrip, err := MarshalSecretDisclosurePlaintextV1(secret)
	require.NoError(t, err)
	require.Equal(t, encoded, roundTrip)
}

func TestComputeSecretAssetIDV1MatchesLegacy(t *testing.T) {
	secret := ComputeSecretAssetIDV1("uclair")
	raw := secret.Bytes()
	require.Equal(t, fixedFieldHex(ComputeAssetIDV1("uclair")), hex.EncodeToString(raw[:]))
}

func TestSecretTransportsRejectAutomaticDisclosure(t *testing.T) {
	for _, value := range []any{SecretNoteV1{Amount: 99887766, Memo: "private memo"}, SecretDisclosurePlaintextV1{Amount: 99887766}} {
		require.Contains(t, fmt.Sprintf("%+v", value), "<redacted>")
		require.NotContains(t, fmt.Sprintf("%#v", value), "99887766")
		_, err := json.Marshal(value)
		require.Error(t, err)
	}
}
func TestSecretDisclosureRejectsMalformedSemantics(t *testing.T) {
	p := SecretDisclosurePlaintextV1{Plane: DisclosurePlaneUserV1, Policy: 1, DisclosedFieldBitmap: 1, Commitment: privacycrypto.FieldValueFromUint64(1), AssetID: privacycrypto.FieldValueFromUint64(2), DisclosureBlinding: privacycrypto.FieldValueFromUint64(3), Amount: 4}
	encoded, err := MarshalSecretDisclosurePlaintextV1(&p)
	require.NoError(t, err)
	for _, mutate := range []func([]byte){
		func(b []byte) { b[18] = 3 },
		func(b []byte) { b[27] = 0 },
		func(b []byte) { b[31] = 2 },
		func(b []byte) { clear(b[32:64]) },
		func(b []byte) { clear(b[len(b)-32:]) },
		func(b []byte) { b[135] = 1 },
	} {
		bad := append([]byte(nil), encoded...)
		mutate(bad)
		_, legacyErr := UnmarshalDisclosurePlaintextV1(bad)
		require.Error(t, legacyErr)
		out, err := UnmarshalSecretDisclosurePlaintextV1(bad)
		require.Error(t, err)
		require.Nil(t, out)
	}
}

func TestSecretNoteCodecRejectsIdentityKey(t *testing.T) {
	sx, sy := secretPoint(t, 17)
	vx, vy := secretPoint(t, 19)
	note := SecretNoteV1{ReceiverSpendPubKeyX: sx, ReceiverSpendPubKeyY: sy, ReceiverViewPubKeyX: vx, ReceiverViewPubKeyY: vy, Amount: 1, AssetID: privacycrypto.FieldValueFromUint64(2), Randomness: privacycrypto.FieldValueFromUint64(3)}
	encoded, err := MarshalSecretNotePlaintextV1(&note)
	require.NoError(t, err)
	clear(encoded[20:84])
	encoded[83] = 1
	out, err := UnmarshalSecretNotePlaintextV1(encoded)
	require.Error(t, err)
	require.Nil(t, out)
	note.ReceiverSpendPubKeyX = privacycrypto.FieldValueFromUint64(0)
	note.ReceiverSpendPubKeyY = privacycrypto.FieldValueFromUint64(1)
	encoded, err = MarshalSecretNotePlaintextV1(&note)
	require.Error(t, err)
	require.Nil(t, encoded)
}
