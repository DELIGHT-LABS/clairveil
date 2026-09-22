package scan

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// walletWireV3 makes FieldValue persistence explicit. FieldValue deliberately
// has no JSON representation so fixed secret values cannot accidentally cross
// a general serialization boundary.
type walletWireV3 struct {
	Version         uint16                  `json:"version"`
	LastHeight      int64                   `json:"last_height"`
	LastSequence    uint64                  `json:"last_sequence,omitempty"`
	LastOutputIndex uint32                  `json:"last_output_index,omitempty"`
	Notes           []secretFoundNoteWireV3 `json:"notes"`
}
type secretFoundNoteWireV3 struct {
	Note           secretNoteWireV3 `json:"note"`
	Nullifier      string           `json:"nullifier"`
	IsSpent        bool             `json:"is_spent"`
	TxHash         string           `json:"tx_hash"`
	Height         int64            `json:"height"`
	GlobalSequence uint64           `json:"global_sequence,omitempty"`
	OutputIndex    uint32           `json:"output_index,omitempty"`
	Commitment     string           `json:"commitment,omitempty"`
	AssetDenom     string           `json:"asset_denom,omitempty"`
	AuditKeyID     string           `json:"audit_key_id,omitempty"`
	AuditKeyEpoch  uint64           `json:"audit_key_epoch,omitempty"`
}
type secretNoteWireV3 struct {
	SpendX     string `json:"receiver_spend_pub_key_x"`
	SpendY     string `json:"receiver_spend_pub_key_y"`
	ViewX      string `json:"receiver_view_pub_key_x"`
	ViewY      string `json:"receiver_view_pub_key_y"`
	Amount     string `json:"amount"`
	AssetID    string `json:"asset_id"`
	Randomness string `json:"randomness"`
	Memo       string `json:"memo"`
}

func (wallet LocalWalletData) MarshalJSON() ([]byte, error) {
	wire := walletWireV3{Version: 3, LastHeight: wallet.LastHeight, LastSequence: wallet.LastSequence, LastOutputIndex: wallet.LastOutputIndex, Notes: make([]secretFoundNoteWireV3, len(wallet.Notes))}
	for i, found := range wallet.Notes {
		wire.Notes[i] = secretFoundNoteWireV3{Note: secretNoteWire(found.Note), Nullifier: found.Nullifier, IsSpent: found.IsSpent, TxHash: found.TxHash, Height: found.Height, GlobalSequence: found.GlobalSequence, OutputIndex: found.OutputIndex, Commitment: found.Commitment, AssetDenom: found.AssetDenom, AuditKeyID: found.AuditKeyID, AuditKeyEpoch: found.AuditKeyEpoch}
	}
	return json.Marshal(wire)
}

func (wallet *LocalWalletData) UnmarshalJSON(data []byte) error {
	var wire walletWireV3
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Version != 3 {
		return fmt.Errorf("unsupported local wallet version %d", wire.Version)
	}
	var candidate LocalWalletData
	candidate.LastHeight, candidate.LastSequence, candidate.LastOutputIndex = wire.LastHeight, wire.LastSequence, wire.LastOutputIndex
	candidate.Notes = make([]SecretFoundNote, len(wire.Notes))
	for i, found := range wire.Notes {
		note, err := secretNoteFromWire(found.Note)
		if err != nil {
			return fmt.Errorf("wallet note %d: %w", i, err)
		}
		candidate.Notes[i] = SecretFoundNote{Note: note, Nullifier: found.Nullifier, IsSpent: found.IsSpent, TxHash: found.TxHash, Height: found.Height, GlobalSequence: found.GlobalSequence, OutputIndex: found.OutputIndex, Commitment: found.Commitment, AssetDenom: found.AssetDenom, AuditKeyID: found.AuditKeyID, AuditKeyEpoch: found.AuditKeyEpoch}
	}
	*wallet = candidate
	return nil
}
func secretNoteWire(note privacytypes.SecretNoteV1) secretNoteWireV3 {
	return secretNoteWireV3{SpendX: fieldWireHex(note.ReceiverSpendPubKeyX), SpendY: fieldWireHex(note.ReceiverSpendPubKeyY), ViewX: fieldWireHex(note.ReceiverViewPubKeyX), ViewY: fieldWireHex(note.ReceiverViewPubKeyY), Amount: note.Amount.String(), AssetID: fieldWireHex(note.AssetID), Randomness: fieldWireHex(note.Randomness), Memo: note.Memo}
}
func secretNoteFromWire(w secretNoteWireV3) (privacytypes.SecretNoteV1, error) {
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
	value, err := privacyamount.Parse(w.Amount)
	if err != nil {
		return privacytypes.SecretNoteV1{}, err
	}
	note, err := privacytypes.NewSecretNoteV1(sx, sy, vx, vy, value, asset, randomness, w.Memo)
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
