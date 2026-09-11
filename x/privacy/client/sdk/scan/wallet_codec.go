package scan

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// walletWireV2 makes FieldValue persistence explicit. FieldValue deliberately
// has no JSON representation so fixed secret values cannot accidentally cross
// a general serialization boundary.
type walletWireV2 struct {
	Version         uint16                  `json:"version"`
	LastHeight      int64                   `json:"last_height"`
	LastSequence    uint64                  `json:"last_sequence,omitempty"`
	LastOutputIndex uint32                  `json:"last_output_index,omitempty"`
	Notes           []secretFoundNoteWireV2 `json:"notes"`
}
type secretFoundNoteWireV2 struct {
	Note           secretNoteWireV2 `json:"note"`
	Nullifier      string           `json:"nullifier"`
	IsSpent        bool             `json:"is_spent"`
	TxHash         string           `json:"tx_hash"`
	Height         int64            `json:"height"`
	GlobalSequence uint64           `json:"global_sequence,omitempty"`
	OutputIndex    uint32           `json:"output_index,omitempty"`
	Commitment     string           `json:"commitment,omitempty"`
	AssetDenom     string           `json:"asset_denom,omitempty"`
}
type secretNoteWireV2 struct {
	SpendX     string `json:"receiver_spend_pub_key_x"`
	SpendY     string `json:"receiver_spend_pub_key_y"`
	ViewX      string `json:"receiver_view_pub_key_x"`
	ViewY      string `json:"receiver_view_pub_key_y"`
	Amount     uint64 `json:"amount"`
	AssetID    string `json:"asset_id"`
	Randomness string `json:"randomness"`
	Memo       string `json:"memo"`
}

func (wallet LocalWalletData) MarshalJSON() ([]byte, error) {
	wire := walletWireV2{Version: 2, LastHeight: wallet.LastHeight, LastSequence: wallet.LastSequence, LastOutputIndex: wallet.LastOutputIndex, Notes: make([]secretFoundNoteWireV2, len(wallet.Notes))}
	for i, found := range wallet.Notes {
		wire.Notes[i] = secretFoundNoteWireV2{Note: secretNoteWire(found.Note), Nullifier: found.Nullifier, IsSpent: found.IsSpent, TxHash: found.TxHash, Height: found.Height, GlobalSequence: found.GlobalSequence, OutputIndex: found.OutputIndex, Commitment: found.Commitment, AssetDenom: found.AssetDenom}
	}
	return json.Marshal(wire)
}

func (wallet *LocalWalletData) UnmarshalJSON(data []byte) error {
	var wire walletWireV2
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Version != 0 && wire.Version != 2 {
		return fmt.Errorf("unsupported local wallet version %d", wire.Version)
	}
	if wire.Version == 0 {
		legacy, err := unmarshalLegacyWalletV1(data)
		if err != nil {
			return fmt.Errorf("unsupported local wallet version %d: %w", wire.Version, err)
		}
		*wallet = *legacy
		return nil
	}
	var candidate LocalWalletData
	candidate.LastHeight, candidate.LastSequence, candidate.LastOutputIndex = wire.LastHeight, wire.LastSequence, wire.LastOutputIndex
	candidate.Notes = make([]SecretFoundNote, len(wire.Notes))
	for i, found := range wire.Notes {
		note, err := secretNoteFromWire(found.Note)
		if err != nil {
			return fmt.Errorf("wallet note %d: %w", i, err)
		}
		candidate.Notes[i] = SecretFoundNote{Note: note, Nullifier: found.Nullifier, IsSpent: found.IsSpent, TxHash: found.TxHash, Height: found.Height, GlobalSequence: found.GlobalSequence, OutputIndex: found.OutputIndex, Commitment: found.Commitment, AssetDenom: found.AssetDenom}
	}
	*wallet = candidate
	return nil
}
func secretNoteWire(note privacytypes.SecretNoteV1) secretNoteWireV2 {
	return secretNoteWireV2{SpendX: fieldWireHex(note.ReceiverSpendPubKeyX), SpendY: fieldWireHex(note.ReceiverSpendPubKeyY), ViewX: fieldWireHex(note.ReceiverViewPubKeyX), ViewY: fieldWireHex(note.ReceiverViewPubKeyY), Amount: note.Amount, AssetID: fieldWireHex(note.AssetID), Randomness: fieldWireHex(note.Randomness), Memo: note.Memo}
}
func secretNoteFromWire(w secretNoteWireV2) (privacytypes.SecretNoteV1, error) {
	sx, e := fieldFromWireHex(w.SpendX)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	sy, e := fieldFromWireHex(w.SpendY)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	vx, e := fieldFromWireHex(w.ViewX)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	vy, e := fieldFromWireHex(w.ViewY)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	asset, e := fieldFromWireHex(w.AssetID)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	randomness, e := fieldFromWireHex(w.Randomness)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	note, err := privacytypes.NewSecretNoteV1(sx, sy, vx, vy, w.Amount, asset, randomness, w.Memo)
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	return *note, nil
}
func fieldWireHex(value privacycrypto.FieldValue) string {
	raw := value.Bytes()
	return hex.EncodeToString(raw[:])
}
func fieldFromWireHex(text string) (privacycrypto.FieldValue, error) {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != 32 {
		return privacycrypto.FieldValue{}, fmt.Errorf("invalid canonical field encoding")
	}
	return privacycrypto.ParseFieldValueBE32(raw)
}

// unmarshalLegacyWalletV1 accepts the former decimal Note JSON schema without
// materialising a legacy Note or a legacy integer object. Decimal field text is accumulated
// directly into its canonical 32-byte representation before FieldValue parses
// it, so migration preserves the fixed-secret boundary.
func unmarshalLegacyWalletV1(data []byte) (*LocalWalletData, error) {
	var legacy struct {
		LastHeight      int64  `json:"last_height"`
		LastSequence    uint64 `json:"last_sequence"`
		LastOutputIndex uint32 `json:"last_output_index"`
		Notes           []struct {
			Note           map[string]json.RawMessage `json:"note"`
			Nullifier      string                     `json:"nullifier"`
			TxHash         string                     `json:"tx_hash"`
			Height         int64                      `json:"height"`
			GlobalSequence uint64                     `json:"global_sequence"`
			OutputIndex    uint32                     `json:"output_index"`
			Commitment     string                     `json:"commitment"`
			AssetDenom     string                     `json:"asset_denom"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	wallet := &LocalWalletData{LastHeight: legacy.LastHeight, LastSequence: legacy.LastSequence, LastOutputIndex: legacy.LastOutputIndex, Notes: make([]SecretFoundNote, len(legacy.Notes))}
	for i, found := range legacy.Notes {
		field := func(names ...string) (privacycrypto.FieldValue, error) {
			for _, name := range names {
				if raw := found.Note[name]; len(raw) != 0 {
					return legacyFieldDecimal(raw)
				}
			}
			return privacycrypto.FieldValue{}, fmt.Errorf("missing legacy field")
		}
		sx, err := field("ReceiverSpendPubKeyX", "rsx")
		if err != nil {
			return nil, err
		}
		sy, err := field("ReceiverSpendPubKeyY", "rsy")
		if err != nil {
			return nil, err
		}
		vx, err := field("ReceiverViewPubKeyX", "rvx")
		if err != nil {
			return nil, err
		}
		vy, err := field("ReceiverViewPubKeyY", "rvy")
		if err != nil {
			return nil, err
		}
		asset, err := field("AssetID", "as")
		if err != nil {
			return nil, err
		}
		randomness, err := field("Randomness", "rn")
		if err != nil {
			return nil, err
		}
		amountRaw := found.Note["Amount"]
		if len(amountRaw) == 0 {
			amountRaw = found.Note["am"]
		}
		amount, err := legacyAmount(amountRaw)
		if err != nil {
			return nil, err
		}
		memoRaw := found.Note["Memo"]
		if len(memoRaw) == 0 {
			memoRaw = found.Note["mm"]
		}
		var memo string
		if err := json.Unmarshal(memoRaw, &memo); err != nil {
			return nil, err
		}
		note, err := privacytypes.NewSecretNoteV1(sx, sy, vx, vy, amount, asset, randomness, memo)
		if err != nil {
			return nil, err
		}
		wallet.Notes[i] = SecretFoundNote{Note: *note, Nullifier: found.Nullifier, TxHash: found.TxHash, Height: found.Height, GlobalSequence: found.GlobalSequence, OutputIndex: found.OutputIndex, Commitment: found.Commitment, AssetDenom: found.AssetDenom}
	}
	return wallet, nil
}
func legacyDecimal(raw json.RawMessage) (string, error) {
	// The former Note codec emitted JSON numbers. Do not reinterpret quoted,
	// empty, null, or noncanonical representations as a zero private field.
	text := string(raw)
	if len(text) == 0 || len(text) > 78 || (len(text) > 1 && text[0] == '0') {
		return "", fmt.Errorf("invalid legacy decimal field")
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("invalid legacy decimal field")
		}
	}
	return text, nil
}
func legacyAmount(raw json.RawMessage) (uint64, error) {
	text, err := legacyDecimal(raw)
	if err != nil {
		return 0, err
	}
	var result uint64
	for _, digit := range text {
		if digit < '0' || digit > '9' || result > (^uint64(0)-uint64(digit-'0'))/10 {
			return 0, fmt.Errorf("invalid legacy amount")
		}
		result = result*10 + uint64(digit-'0')
	}
	return result, nil
}
func legacyFieldDecimal(raw json.RawMessage) (privacycrypto.FieldValue, error) {
	text, err := legacyDecimal(raw)
	if err != nil {
		return privacycrypto.FieldValue{}, err
	}
	var output [32]byte
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return privacycrypto.FieldValue{}, fmt.Errorf("invalid legacy field")
		}
		carry := uint16(digit - '0')
		for i := len(output) - 1; i >= 0; i-- {
			value := uint16(output[i])*10 + carry
			output[i] = byte(value)
			carry = value >> 8
		}
		if carry != 0 {
			return privacycrypto.FieldValue{}, fmt.Errorf("legacy field exceeds 256 bits")
		}
	}
	return privacycrypto.ParseFieldValueBE32(output[:])
}
