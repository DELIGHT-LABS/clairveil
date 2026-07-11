package transfer

import (
	"fmt"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func EncryptOutputNotes(recipientNote privacytypes.Note, changeNote privacytypes.Note) ([][]byte, error) {
	recipientCipherText, _, err := encryptNoteForReceiver(recipientNote, "recipient", nil, 0, false)
	if err != nil {
		return nil, err
	}

	changeCipherText, _, err := encryptNoteForReceiver(changeNote, "change", nil, 1, false)
	if err != nil {
		return nil, err
	}

	return [][]byte{recipientCipherText, changeCipherText}, nil
}

func EncryptOutputNotesWithViewTags(recipientNote privacytypes.Note, changeNote privacytypes.Note, outputCommitments [][]byte) ([][]byte, [][]byte, error) {
	if len(outputCommitments) != 2 {
		return nil, nil, fmt.Errorf("transfer output encryption requires exactly 2 commitments; got %d", len(outputCommitments))
	}

	recipientCipherText, recipientViewTag, err := encryptNoteForReceiver(recipientNote, "recipient", outputCommitments[0], 0, true)
	if err != nil {
		return nil, nil, err
	}

	changeCipherText, changeViewTag, err := encryptNoteForReceiver(changeNote, "change", outputCommitments[1], 1, true)
	if err != nil {
		return nil, nil, err
	}

	return [][]byte{recipientCipherText, changeCipherText}, [][]byte{recipientViewTag, changeViewTag}, nil
}

func encryptNoteForReceiver(note privacytypes.Note, label string, outputCommitment []byte, outputIndex uint32, includeViewTag bool) ([]byte, []byte, error) {
	viewPubKey, err := viewPubKeyFromNote(note)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid %s note receiver view key: %w", label, err)
	}

	noteBytes, err := privacytypes.MarshalNotePlaintextV1(&note)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal %s NotePlaintextV1: %w", label, err)
	}

	if includeViewTag {
		rawCipherText, viewTag, err := privacycrypto.AsymEncryptWithViewTag(noteBytes, *viewPubKey, outputCommitment, outputIndex)
		if err != nil {
			return nil, nil, err
		}
		cipherText, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, rawCipherText)
		if err != nil {
			return nil, nil, err
		}
		return cipherText, viewTag, nil
	}

	rawCipherText, err := privacycrypto.AsymEncrypt(noteBytes, *viewPubKey)
	if err != nil {
		return nil, nil, err
	}
	cipherText, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, rawCipherText)
	if err != nil {
		return nil, nil, err
	}
	return cipherText, nil, nil
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
