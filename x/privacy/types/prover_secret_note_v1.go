package types

import "math/big"

// ToProverWitnessV1 is the sole SecretNoteV1 conversion to the legacy
// math/big Note representation. It is for immediate gnark witness assembly;
// wallet, scan, encryption and disclosure paths must retain SecretNoteV1.
func (note SecretNoteV1) ToProverWitnessV1() Note {
	toBig := func(fieldBytes [32]byte) *big.Int { return new(big.Int).SetBytes(fieldBytes[:]) }
	spendX, spendY := note.ReceiverSpendPubKeyX.Bytes(), note.ReceiverSpendPubKeyY.Bytes()
	viewX, viewY := note.ReceiverViewPubKeyX.Bytes(), note.ReceiverViewPubKeyY.Bytes()
	assetID, randomness := note.AssetID.Bytes(), note.Randomness.Bytes()
	return Note{
		ReceiverSpendPubKeyX: toBig(spendX), ReceiverSpendPubKeyY: toBig(spendY),
		ReceiverViewPubKeyX: toBig(viewX), ReceiverViewPubKeyY: toBig(viewY),
		Amount: toBig(note.Amount.Bytes32()), AssetID: toBig(assetID), Randomness: toBig(randomness), Memo: note.Memo,
	}
}
