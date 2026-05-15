package scan

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type FoundNote struct {
	Note      privacytypes.Note `json:"note"`
	Nullifier string            `json:"nullifier"`
	IsSpent   bool              `json:"-"`
	TxHash    string            `json:"tx_hash"`
	Height    int64             `json:"height"`
}

func ProcessTx(txRes *cmttypes.ResultTx, rootSeed []byte, spendScalar *big.Int, viewScalar *big.Int) []FoundNote {
	var found []FoundNote

	for _, event := range txRes.TxResult.Events {
		if event.Type == "deposit" {
			var encryptedNoteHex string
			for _, attr := range event.Attributes {
				if string(attr.Key) == "encrypted_note" {
					encryptedNoteHex = removeQuotes(string(attr.Value))
				}
			}
			if encryptedNoteHex == "" {
				continue
			}

			cipherBytes, err := hex.DecodeString(encryptedNoteHex)
			if err != nil {
				continue
			}

			noteBytes, err := privacycrypto.Decrypt(cipherBytes, rootSeed)
			if err != nil {
				continue
			}

			note, err := ParseNoteBytes(noteBytes)
			if err != nil {
				continue
			}

			found = append(found, BuildFoundNote(note, txRes))
		}

		if event.Type == "shielded_transfer" {
			targetKeys := []string{"cipher_text_1", "cipher_text_2"}

			for _, key := range targetKeys {
				var cipherHex string
				for _, attr := range event.Attributes {
					if string(attr.Key) == key {
						cipherHex = removeQuotes(string(attr.Value))
						break
					}
				}
				if cipherHex == "" {
					continue
				}

				cipherBytes, err := hex.DecodeString(cipherHex)
				if err != nil {
					continue
				}

				noteBytes, err := privacycrypto.AsymDecrypt(cipherBytes, viewScalar)
				if err != nil && spendScalar != nil && (viewScalar == nil || spendScalar.Cmp(viewScalar) != 0) {
					noteBytes, err = privacycrypto.AsymDecrypt(cipherBytes, spendScalar)
				}
				if err != nil {
					continue
				}

				note, err := ParseNoteBytes(noteBytes)
				if err != nil {
					continue
				}

				found = append(found, BuildFoundNote(note, txRes))
			}
		}
	}

	return found
}

func ParseNoteBytes(data []byte) (*privacytypes.Note, error) {
	var note privacytypes.Note
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, err
	}
	return &note, nil
}

func BuildFoundNote(note *privacytypes.Note, txRes *cmttypes.ResultTx) FoundNote {
	nullifier := note.ComputeNullifier()
	nullifierHex, err := privacyfield.CanonicalHexFromBigInt(nullifier)
	if err != nil {
		nullifierHex = hex.EncodeToString(nullifier.Bytes())
	}

	return FoundNote{
		Note:      *note,
		Nullifier: nullifierHex,
		TxHash:    fmt.Sprintf("%X", txRes.Hash),
		Height:    txRes.Height,
		IsSpent:   false,
	}
}

func removeQuotes(s string) string {
	if len(s) > 0 && s[0] == '"' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == '"' {
		s = s[:len(s)-1]
	}
	return s
}
