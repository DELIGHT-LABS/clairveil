package cli

import (
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
	var amount uint64
	if n.Amount != nil {
		amount = n.Amount.Uint64()
	}
	return privacytypes.SecretNoteV1{ReceiverSpendPubKeyX: field(n.ReceiverSpendPubKeyX), ReceiverSpendPubKeyY: field(n.ReceiverSpendPubKeyY), ReceiverViewPubKeyX: field(n.ReceiverViewPubKeyX), ReceiverViewPubKeyY: field(n.ReceiverViewPubKeyY), Amount: amount, AssetID: field(n.AssetID), Randomness: field(n.Randomness), Memo: n.Memo}
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

func testLegacyDisclosureFixture(p *privacytypes.SecretDisclosurePlaintextV1) *privacytypes.DisclosurePlaintextV1 {
	if p == nil {
		return nil
	}
	return &privacytypes.DisclosurePlaintextV1{Plane: p.Plane, OutputIndex: p.OutputIndex, Policy: p.Policy, DisclosedFieldBitmap: p.DisclosedFieldBitmap, Commitment: testSecretFieldBig(p.Commitment), Amount: new(big.Int).SetUint64(p.Amount), AssetID: testSecretFieldBig(p.AssetID), SenderSpendKeyX: testSecretFieldBig(p.SenderSpendKeyX), SenderSpendKeyY: testSecretFieldBig(p.SenderSpendKeyY), SenderViewKeyX: testSecretFieldBig(p.SenderViewKeyX), SenderViewKeyY: testSecretFieldBig(p.SenderViewKeyY), RecipientSpendKeyX: testSecretFieldBig(p.RecipientSpendKeyX), RecipientSpendKeyY: testSecretFieldBig(p.RecipientSpendKeyY), RecipientViewKeyX: testSecretFieldBig(p.RecipientViewKeyX), RecipientViewKeyY: testSecretFieldBig(p.RecipientViewKeyY), DisclosureBlinding: testSecretFieldBig(p.DisclosureBlinding)}
}
