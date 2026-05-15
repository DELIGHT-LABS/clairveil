package transfer

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestEncryptOutputNotesDecryptsWithMatchingViewKeys(t *testing.T) {
	recipientViewScalar, recipientViewPubKey := testScalarAndPubKey(31)
	changeViewScalar, changeViewPubKey := testScalarAndPubKey(37)
	recipientSpendScalar, recipientSpendPubKey := testScalarAndPubKey(41)
	changeSpendScalar, changeSpendPubKey := testScalarAndPubKey(43)

	recipientNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(recipientSpendPubKey, true),
		ReceiverSpendPubKeyY: pointCoordinate(recipientSpendPubKey, false),
		ReceiverViewPubKeyX:  pointCoordinate(recipientViewPubKey, true),
		ReceiverViewPubKeyY:  pointCoordinate(recipientViewPubKey, false),
		Amount:               big.NewInt(7),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(303),
		Memo:                 "recipient",
	}
	changeNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(changeSpendPubKey, true),
		ReceiverSpendPubKeyY: pointCoordinate(changeSpendPubKey, false),
		ReceiverViewPubKeyX:  pointCoordinate(changeViewPubKey, true),
		ReceiverViewPubKeyY:  pointCoordinate(changeViewPubKey, false),
		Amount:               big.NewInt(2),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(404),
		Memo:                 "change",
	}

	cipherTexts, err := EncryptOutputNotes(recipientNote, changeNote)
	require.NoError(t, err)
	require.Len(t, cipherTexts, 2)

	recipientPlainText, err := privacycrypto.AsymDecrypt(cipherTexts[0], recipientViewScalar)
	require.NoError(t, err)
	changePlainText, err := privacycrypto.AsymDecrypt(cipherTexts[1], changeViewScalar)
	require.NoError(t, err)

	recipientJSON, err := json.Marshal(recipientNote)
	require.NoError(t, err)
	changeJSON, err := json.Marshal(changeNote)
	require.NoError(t, err)

	require.Equal(t, recipientJSON, recipientPlainText)
	require.Equal(t, changeJSON, changePlainText)
	require.NotNil(t, recipientSpendScalar)
	require.NotNil(t, changeSpendScalar)
}

func TestEncryptOutputNotesRejectsMissingViewKey(t *testing.T) {
	_, recipientSpendPubKey := testScalarAndPubKey(47)
	_, changeSpendPubKey := testScalarAndPubKey(53)
	_, changeViewPubKey := testScalarAndPubKey(59)

	recipientNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(recipientSpendPubKey, true),
		ReceiverSpendPubKeyY: pointCoordinate(recipientSpendPubKey, false),
		Amount:               big.NewInt(1),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(505),
	}
	changeNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(changeSpendPubKey, true),
		ReceiverSpendPubKeyY: pointCoordinate(changeSpendPubKey, false),
		ReceiverViewPubKeyX:  pointCoordinate(changeViewPubKey, true),
		ReceiverViewPubKeyY:  pointCoordinate(changeViewPubKey, false),
		Amount:               big.NewInt(2),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(606),
	}

	_, err := EncryptOutputNotes(recipientNote, changeNote)
	require.ErrorContains(t, err, "invalid recipient note receiver view key")
}
