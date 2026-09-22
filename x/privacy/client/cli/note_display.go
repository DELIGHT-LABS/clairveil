package cli

import (
	"encoding/json"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// displayedNote is the explicit list-notes JSON export boundary. SecretNoteV1
// itself remains nonserializable; this preserves the existing numeric JSON
// fields without building a private math/big note for display.
type displayedNote struct{ privacytypes.SecretNoteV1 }

func (n displayedNote) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		SpendX     json.Number `json:"rsx"`
		SpendY     json.Number `json:"rsy"`
		ViewX      json.Number `json:"rvx"`
		ViewY      json.Number `json:"rvy"`
		Amount     string      `json:"am"`
		Asset      json.Number `json:"as"`
		Randomness json.Number `json:"rn"`
		Memo       string      `json:"mm"`
	}{decimalField(n.ReceiverSpendPubKeyX.Bytes()), decimalField(n.ReceiverSpendPubKeyY.Bytes()), decimalField(n.ReceiverViewPubKeyX.Bytes()), decimalField(n.ReceiverViewPubKeyY.Bytes()), n.Amount.String(), decimalField(n.AssetID.Bytes()), decimalField(n.Randomness.Bytes()), n.Memo})
}

func decimalField(raw [32]byte) json.Number {
	var digits [78]byte // 256-bit values fit in 78 decimal digits.
	for _, value := range raw {
		carry := uint16(value)
		for i := len(digits) - 1; i >= 0; i-- {
			v := uint16(digits[i])*256 + carry
			digits[i], carry = byte(v%10), v/10
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
