package scan

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

var ErrPrivacyScanOutputNotOwned = errors.New("privacy scan output is not decryptable by this wallet")

type FoundNote struct {
	Note           privacytypes.Note `json:"note"`
	Nullifier      string            `json:"nullifier"`
	IsSpent        bool              `json:"-"`
	TxHash         string            `json:"tx_hash"`
	Height         int64             `json:"height"`
	GlobalSequence uint64            `json:"global_sequence,omitempty"`
	OutputIndex    uint32            `json:"output_index,omitempty"`
	Commitment     string            `json:"commitment,omitempty"`
	AssetDenom     string            `json:"asset_denom,omitempty"`
}

type processOptions struct {
	SkipViewTagMismatch bool
	EventLimit          uint32
	MaxEncodedBytes     uint64
}

func ProcessTx(txRes *cmttypes.ResultTx, rootSeed []byte, spendScalar *big.Int, viewScalar *big.Int) []FoundNote {
	return processTxWithOptions(txRes, rootSeed, spendScalar, viewScalar, processOptions{})
}

func processTxWithOptions(txRes *cmttypes.ResultTx, rootSeed []byte, spendScalar *big.Int, viewScalar *big.Int, opts processOptions) []FoundNote {
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

			rawCipherText, err := privacytypes.UnwrapEncryptedEnvelopeV1(cipherBytes, privacytypes.EnvelopeDepositNoteV1)
			if err != nil {
				continue
			}
			noteBytes, err := privacycrypto.Decrypt(rawCipherText, rootSeed)
			if err != nil {
				continue
			}

			note, err := ParseNoteBytes(noteBytes)
			if err != nil {
				continue
			}
			if !noteCommitmentMatches(note, eventAttributeValue(event.Attributes, privacytypes.AttributeKeyCommitment)) {
				continue
			}

			found = append(found, BuildFoundNote(note, txRes))
		}

		if event.Type == "shielded_transfer" {
			targets := []struct {
				cipherTextKey string
				commitmentKey string
				viewTagKey    string
			}{
				{
					cipherTextKey: privacytypes.AttributeKeyCipherText1,
					commitmentKey: privacytypes.AttributeKeyCommitment1,
					viewTagKey:    privacytypes.AttributeKeyViewTag1,
				},
				{
					cipherTextKey: privacytypes.AttributeKeyCipherText2,
					commitmentKey: privacytypes.AttributeKeyCommitment2,
					viewTagKey:    privacytypes.AttributeKeyViewTag2,
				},
			}

			for outputIndex, target := range targets {
				cipherHex := eventAttributeValue(event.Attributes, target.cipherTextKey)
				if cipherHex == "" {
					continue
				}

				cipherBytes, err := hex.DecodeString(cipherHex)
				if err != nil {
					continue
				}

				commitmentHex := eventAttributeValue(event.Attributes, target.commitmentKey)
				noteBytes, err := decryptTransferOutput(
					cipherBytes,
					viewScalar,
					spendScalar,
					commitmentHex,
					uint32(outputIndex),
					eventAttributeValue(event.Attributes, target.viewTagKey),
					opts.SkipViewTagMismatch,
				)
				if err != nil {
					continue
				}

				note, err := ParseNoteBytes(noteBytes)
				if err != nil {
					continue
				}
				if !noteCommitmentMatches(note, commitmentHex) {
					continue
				}

				found = append(found, BuildFoundNote(note, txRes))
			}
		}
	}

	return found
}

func ProcessScanEvent(event *privacytypes.QueryScanEvent, rootSeed []byte, spendScalar *big.Int, viewScalar *big.Int) []FoundNote {
	return processScanEventWithOptions(event, rootSeed, spendScalar, viewScalar, processOptions{})
}

func processScanEventWithOptions(event *privacytypes.QueryScanEvent, rootSeed []byte, spendScalar *big.Int, viewScalar *big.Int, opts processOptions) []FoundNote {
	if event == nil {
		return nil
	}

	found := make([]FoundNote, 0, len(event.Outputs))
	for _, output := range event.Outputs {
		if output == nil {
			continue
		}

		switch event.EventType {
		case privacytypes.EventTypeDeposit:
			if output.EncryptedNoteHex == "" {
				continue
			}
			cipherBytes, err := hex.DecodeString(output.EncryptedNoteHex)
			if err != nil {
				continue
			}
			rawCipherText, err := privacytypes.UnwrapEncryptedEnvelopeV1(cipherBytes, privacytypes.EnvelopeDepositNoteV1)
			if err != nil {
				continue
			}
			noteBytes, err := privacycrypto.Decrypt(rawCipherText, rootSeed)
			if err != nil {
				continue
			}
			note, err := ParseNoteBytes(noteBytes)
			if err != nil {
				continue
			}
			if !noteCommitmentMatches(note, output.CommitmentHex) {
				continue
			}
			found = append(found, BuildFoundNoteFromScanEvent(note, event))
		case privacytypes.EventTypeShieldedTransfer:
			if output.CipherTextHex == "" {
				continue
			}
			cipherBytes, err := hex.DecodeString(output.CipherTextHex)
			if err != nil {
				continue
			}

			noteBytes, err := decryptTransferOutput(cipherBytes, viewScalar, spendScalar, output.CommitmentHex, output.OutputIndex, output.ViewTagHex, opts.SkipViewTagMismatch)
			if err != nil {
				continue
			}

			note, err := ParseNoteBytes(noteBytes)
			if err != nil {
				continue
			}
			if !noteCommitmentMatches(note, output.CommitmentHex) {
				continue
			}
			found = append(found, BuildFoundNoteFromScanEvent(note, event))
		}
	}

	return found
}

func decryptTransferOutput(cipherBytes []byte, viewScalar *big.Int, spendScalar *big.Int, commitmentHex string, outputIndex uint32, viewTagHex string, skipViewTagMismatch bool) ([]byte, error) {
	rawCipherText, err := privacytypes.UnwrapEncryptedEnvelopeV1(cipherBytes, privacytypes.EnvelopeTransferNoteV1)
	if err != nil {
		return nil, fmt.Errorf("invalid transfer note envelope: %w", err)
	}
	if viewScalar != nil {
		if commitmentBytes, viewTagBytes, ok := decodeViewTagInputs(commitmentHex, viewTagHex); ok {
			noteBytes, err := privacycrypto.AsymDecryptWithViewTag(rawCipherText, viewScalar, commitmentBytes, outputIndex, viewTagBytes)
			if err == nil {
				return noteBytes, nil
			}
			if errors.Is(err, privacycrypto.ErrViewTagMismatch) && skipViewTagMismatch {
				return nil, fmt.Errorf("%w: %v", ErrPrivacyScanOutputNotOwned, err)
			}
		}

		noteBytes, err := privacycrypto.AsymDecrypt(rawCipherText, viewScalar)
		if err == nil {
			return noteBytes, nil
		}
	}
	if spendScalar != nil && (viewScalar == nil || spendScalar.Cmp(viewScalar) != 0) {
		noteBytes, err := privacycrypto.AsymDecrypt(rawCipherText, spendScalar)
		if err == nil {
			return noteBytes, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrPrivacyScanOutputNotOwned, err)
	}

	return nil, ErrPrivacyScanOutputNotOwned
}

func decodeViewTagInputs(commitmentHex string, viewTagHex string) ([]byte, []byte, bool) {
	if commitmentHex == "" || viewTagHex == "" {
		return nil, nil, false
	}
	commitmentBytes, err := hex.DecodeString(commitmentHex)
	if err != nil || len(commitmentBytes) != 32 {
		return nil, nil, false
	}
	viewTagBytes, err := hex.DecodeString(viewTagHex)
	if err != nil || len(viewTagBytes) != privacytypes.ViewTagLength {
		return nil, nil, false
	}
	return commitmentBytes, viewTagBytes, true
}

func ParseNoteBytes(data []byte) (*privacytypes.Note, error) {
	note, err := privacytypes.UnmarshalNotePlaintextV1(data)
	if err != nil {
		return nil, fmt.Errorf("invalid NotePlaintextV1: %w", err)
	}
	return note, nil
}

func noteCommitmentMatches(note *privacytypes.Note, commitmentHex string) bool {
	if note == nil || commitmentHex == "" {
		return false
	}

	expected, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	if err != nil {
		return false
	}
	commitmentBytes, err := privacyfield.DecodeCanonicalHex(commitmentHex, "commitment")
	if err != nil {
		return false
	}
	return expected == hex.EncodeToString(commitmentBytes)
}

func BuildFoundNote(note *privacytypes.Note, txRes *cmttypes.ResultTx) FoundNote {
	return buildFoundNote(note, fmt.Sprintf("%X", txRes.Hash), txRes.Height)
}

func BuildFoundNoteFromScanEvent(note *privacytypes.Note, event *privacytypes.QueryScanEvent) FoundNote {
	if event == nil {
		return buildFoundNote(note, "", 0)
	}
	return buildFoundNote(note, event.TxHashHex, event.Height)
}

// ProcessPrivacyScanOutput decrypts one ciphertext-bearing V2 record. A view
// tag mismatch is ignored by default; tag-only skipping is an explicit opt-in.
func ProcessPrivacyScanOutput(output *privacytypes.PrivacyScanOutputV2, rootSeed []byte, spendScalar, viewScalar *big.Int, tagOnlyFastMode bool) (*FoundNote, error) {
	if output == nil {
		return nil, fmt.Errorf("privacy scan output is required")
	}
	if err := privacyfield.ValidateCanonicalBytes32(output.Commitment); err != nil || new(big.Int).SetBytes(output.Commitment).Sign() == 0 {
		return nil, fmt.Errorf("privacy scan commitment is not an active canonical field")
	}
	var noteBytes []byte
	var err error
	switch output.EventType {
	case privacytypes.EventTypeDeposit:
		var raw []byte
		raw, err = privacytypes.UnwrapEncryptedEnvelopeV1(output.EncryptedNote, privacytypes.EnvelopeDepositNoteV1)
		if err != nil {
			return nil, fmt.Errorf("invalid deposit note envelope: %w", err)
		}
		noteBytes, err = privacycrypto.Decrypt(raw, rootSeed)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPrivacyScanOutputNotOwned, err)
		}
	case privacytypes.EventTypeShieldedTransfer, privacytypes.EventTypeBatchTransferV1:
		if len(output.ViewTag) != privacytypes.ViewTagLength {
			return nil, fmt.Errorf("privacy scan view tag has invalid framing")
		}
		noteBytes, err = decryptTransferOutput(output.Ciphertext, viewScalar, spendScalar, hex.EncodeToString(output.Commitment), output.OutputIndex, hex.EncodeToString(output.ViewTag), tagOnlyFastMode)
	default:
		return nil, fmt.Errorf("unsupported privacy scan output event type %q", output.EventType)
	}
	if err != nil {
		return nil, err
	}
	note, err := ParseNoteBytes(noteBytes)
	if err != nil {
		return nil, err
	}
	commitmentHex := hex.EncodeToString(output.Commitment)
	if !noteCommitmentMatches(note, commitmentHex) {
		return nil, fmt.Errorf("NoteV1 commitment mismatch")
	}
	found := buildFoundNote(note, hex.EncodeToString(output.TxHash), output.Height)
	found.GlobalSequence = output.GlobalSequence
	found.OutputIndex = output.OutputIndex
	found.Commitment = commitmentHex
	return &found, nil
}

func buildFoundNote(note *privacytypes.Note, txHash string, height int64) FoundNote {
	nullifier := note.ComputeNullifier()
	nullifierHex, err := privacyfield.CanonicalHexFromBigInt(nullifier)
	if err != nil {
		nullifierHex = hex.EncodeToString(nullifier.Bytes())
	}

	return FoundNote{
		Note:      *note,
		Nullifier: nullifierHex,
		TxHash:    txHash,
		Height:    height,
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

func eventAttributeValue(attrs []abci.EventAttribute, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return removeQuotes(string(attr.Value))
		}
	}
	return ""
}
