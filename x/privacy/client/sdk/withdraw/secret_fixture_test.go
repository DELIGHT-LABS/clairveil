package withdraw

import (
	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"math/big"
)

// Test-only adapter keeps established fixture values readable. Production
// builders and decoders never accept a legacy private big.Int note.
func testSecretNoteFixture(n privacytypes.Note) privacytypes.SecretNoteV1 {
	field := func(v *big.Int) privacycrypto.FieldValue {
		if v == nil {
			return privacycrypto.FieldValue{}
		}
		var raw [32]byte
		v.FillBytes(raw[:])
		f, err := privacycrypto.ParseFieldValueBE32(raw[:])
		if err != nil {
			panic(err)
		}
		return f
	}
	var nativeAmount privacyamount.Amount128
	if n.Amount != nil {
		nativeAmount, _ = privacytypes.Amount128FromBigInt(n.Amount)
	}
	return privacytypes.SecretNoteV1{ReceiverSpendPubKeyX: field(n.ReceiverSpendPubKeyX), ReceiverSpendPubKeyY: field(n.ReceiverSpendPubKeyY), ReceiverViewPubKeyX: field(n.ReceiverViewPubKeyX), ReceiverViewPubKeyY: field(n.ReceiverViewPubKeyY), Amount: nativeAmount, AssetID: field(n.AssetID), Randomness: field(n.Randomness), Memo: n.Memo}
}
func testSecretFieldBig(v privacycrypto.FieldValue) *big.Int {
	b := v.Bytes()
	return new(big.Int).SetBytes(b[:])
}
func testSecretCommitment(n privacytypes.SecretNoteV1) *big.Int {
	v, err := privacytypes.SecretNoteCommitmentV1(n)
	if err != nil {
		panic(err)
	}
	return testSecretFieldBig(v)
}
func testSecretNullifier(n privacytypes.SecretNoteV1) *big.Int {
	c, err := privacytypes.SecretNoteCommitmentV1(n)
	if err != nil {
		panic(err)
	}
	v, err := privacytypes.SecretNoteNullifierV1(n, c)
	if err != nil {
		panic(err)
	}
	return testSecretFieldBig(v)
}
