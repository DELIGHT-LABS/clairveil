package transfer

import (
	"encoding/json"
	"fmt"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func EncryptOutputNotes(recipientNote privacytypes.Note, changeNote privacytypes.Note) ([][]byte, error) {
	recipientCipherText, err := encryptNoteForReceiver(recipientNote, "recipient")
	if err != nil {
		return nil, err
	}

	changeCipherText, err := encryptNoteForReceiver(changeNote, "change")
	if err != nil {
		return nil, err
	}

	return [][]byte{recipientCipherText, changeCipherText}, nil
}

func encryptNoteForReceiver(note privacytypes.Note, label string) ([]byte, error) {
	viewPubKey, err := viewPubKeyFromNote(note)
	if err != nil {
		return nil, fmt.Errorf("invalid %s note receiver view key: %w", label, err)
	}

	noteJSON, err := json.Marshal(note)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s note: %w", label, err)
	}

	cipherText, err := privacycrypto.AsymEncrypt(noteJSON, *viewPubKey)
	if err != nil {
		return nil, err
	}
	return cipherText, nil
}

func viewPubKeyFromNote(note privacytypes.Note) (*crypto_tedwards.PointAffine, error) {
	if note.ReceiverViewPubKeyX == nil || note.ReceiverViewPubKeyY == nil {
		return nil, fmt.Errorf("receiver view key coordinates must not be nil")
	}

	var point crypto_tedwards.PointAffine
	point.X.SetBigInt(note.ReceiverViewPubKeyX)
	point.Y.SetBigInt(note.ReceiverViewPubKeyY)
	return &point, nil
}
