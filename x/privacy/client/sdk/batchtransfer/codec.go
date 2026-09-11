package batchtransfer

import (
	"bytes"
	"encoding/json"
	"fmt"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// preparedNoteWire is the explicit private prover transport boundary. It keeps
// the existing numeric Note JSON without constructing variable-width secrets.
type preparedNoteWire struct {
	SpendX     json.Number `json:"rsx"`
	SpendY     json.Number `json:"rsy"`
	ViewX      json.Number `json:"rvx"`
	ViewY      json.Number `json:"rvy"`
	Amount     uint64      `json:"am"`
	Asset      json.Number `json:"as"`
	Randomness json.Number `json:"rn"`
	Memo       string      `json:"mm"`
}

func decimalField(v privacycrypto.FieldValue) json.Number {
	raw := v.Bytes()
	var digits [78]byte
	for _, b := range raw {
		carry := uint16(b)
		for i := len(digits) - 1; i >= 0; i-- {
			n := uint16(digits[i])*256 + carry
			digits[i], carry = byte(n%10), n/10
		}
	}
	start := 0
	for start < len(digits)-1 && digits[start] == 0 {
		start++
	}
	for i := range digits {
		digits[i] += '0'
	}
	return json.Number(string(digits[start:]))
}
func parseDecimalField(text json.Number) (privacycrypto.FieldValue, error) {
	if len(text) == 0 || len(text) > 78 {
		return privacycrypto.FieldValue{}, fmt.Errorf("invalid field decimal length")
	}
	var raw [32]byte
	for _, d := range text {
		if d < '0' || d > '9' {
			return privacycrypto.FieldValue{}, fmt.Errorf("invalid field decimal")
		}
		carry := uint16(d - '0')
		for i := 31; i >= 0; i-- {
			n := uint16(raw[i])*10 + carry
			raw[i], carry = byte(n), n>>8
		}
		if carry != 0 {
			return privacycrypto.FieldValue{}, fmt.Errorf("field exceeds 256 bits")
		}
	}
	return privacycrypto.ParseFieldValueBE32(raw[:])
}
func noteWire(n privacytypes.SecretNoteV1) preparedNoteWire {
	return preparedNoteWire{decimalField(n.ReceiverSpendPubKeyX), decimalField(n.ReceiverSpendPubKeyY), decimalField(n.ReceiverViewPubKeyX), decimalField(n.ReceiverViewPubKeyY), n.Amount, decimalField(n.AssetID), decimalField(n.Randomness), n.Memo}
}
func (w preparedNoteWire) note() (privacytypes.SecretNoteV1, error) {
	var fields [6]privacycrypto.FieldValue
	for i, v := range []json.Number{w.SpendX, w.SpendY, w.ViewX, w.ViewY, w.Asset, w.Randomness} {
		f, e := parseDecimalField(v)
		if e != nil {
			return privacytypes.SecretNoteV1{}, e
		}
		fields[i] = f
	}
	n, e := privacytypes.NewSecretNoteV1(fields[0], fields[1], fields[2], fields[3], w.Amount, fields[4], fields[5], w.Memo)
	if e != nil {
		return privacytypes.SecretNoteV1{}, e
	}
	return *n, nil
}
func (p PreparedBatchTransferInput) MarshalJSON() ([]byte, error) {
	type alias PreparedBatchTransferInput
	return json.Marshal(struct {
		Note preparedNoteWire `json:"note"`
		*alias
	}{noteWire(p.Note), (*alias)(&p)})
}
func (p *PreparedBatchTransferInput) UnmarshalJSON(raw []byte) error {
	type alias PreparedBatchTransferInput
	var candidate PreparedBatchTransferInput
	w := struct {
		Note preparedNoteWire `json:"note"`
		*alias
	}{alias: (*alias)(&candidate)}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&w); e != nil {
		return e
	}
	n, e := w.Note.note()
	if e != nil {
		return e
	}
	candidate.Note = n
	*p = candidate
	return nil
}
func (p PreparedBatchTransferOutput) MarshalJSON() ([]byte, error) {
	// Field order is part of the existing prepared-payload JSON hash.
	return json.Marshal(struct {
		Kind           OutputKind                      `json:"kind"`
		Note           preparedNoteWire                `json:"note"`
		PrivacyPolicy  uint32                          `json:"privacy_policy"`
		DisclosureMode privacytypes.UserDisclosureMode `json:"disclosure_mode"`
		Target         []byte                          `json:"disclosure_target_pubkey,omitempty"`
		User           json.Number                     `json:"user_disclosure_blinding"`
		Full           json.Number                     `json:"full_disclosure_blinding"`
	}{p.Kind, noteWire(p.Note), p.PrivacyPolicy, p.DisclosureMode, p.DisclosureTargetPubKey, decimalField(p.UserDisclosureBlinding), decimalField(p.FullDisclosureBlinding)})
}
func (p *PreparedBatchTransferOutput) UnmarshalJSON(raw []byte) error {
	type alias PreparedBatchTransferOutput
	var candidate PreparedBatchTransferOutput
	w := struct {
		Note preparedNoteWire `json:"note"`
		User json.Number      `json:"user_disclosure_blinding"`
		Full json.Number      `json:"full_disclosure_blinding"`
		*alias
	}{alias: (*alias)(&candidate)}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&w); e != nil {
		return e
	}
	n, e := w.Note.note()
	if e != nil {
		return e
	}
	u, e := parseDecimalField(w.User)
	if e != nil {
		return e
	}
	f, e := parseDecimalField(w.Full)
	if e != nil {
		return e
	}
	candidate.Note, candidate.UserDisclosureBlinding, candidate.FullDisclosureBlinding = n, u, f
	*p = candidate
	return nil
}
