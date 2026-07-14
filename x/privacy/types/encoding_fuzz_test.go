package types

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

func FuzzNotePlaintextV1DecoderCanonicalRoundTrip(f *testing.F) {
	valid, err := MarshalNotePlaintextV1(fixedPayloadTestNote())
	if err != nil {
		f.Fatalf("build NotePlaintextV1 seed: %v", err)
	}
	f.Add(valid)
	f.Add([]byte(nil))
	f.Add(make([]byte, NotePlaintextV1Size))
	f.Add(make([]byte, NotePlaintextV1Size+1))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > NotePlaintextV1Size+1 {
			return
		}
		note, err := UnmarshalNotePlaintextV1(encoded)
		if err != nil {
			return
		}
		roundTrip, err := MarshalNotePlaintextV1(note)
		if err != nil {
			t.Fatalf("accepted note did not re-encode: %v", err)
		}
		if !bytes.Equal(encoded, roundTrip) {
			t.Fatal("accepted NotePlaintextV1 was not canonical")
		}
		if note.ComputeCommitment().Cmp(note.ComputeCommitment()) != 0 || note.ComputeNullifier().Cmp(note.ComputeNullifier()) != 0 {
			t.Fatal("accepted NotePlaintextV1 produced nondeterministic identifiers")
		}
	})
}

func FuzzDisclosurePlaintextV1DecoderCanonicalRoundTrip(f *testing.F) {
	note := fixedPayloadTestNote()
	valid, err := MarshalDisclosurePlaintextV1(&DisclosurePlaintextV1{
		Plane: DisclosurePlaneFullV1, OutputIndex: 7,
		Policy: DisclosureFullMarkerV1, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmountToFrom,
		Commitment: note.ComputeCommitment(), Amount: note.Amount, AssetID: note.AssetID,
		SenderSpendKeyX: note.ReceiverSpendPubKeyX, SenderSpendKeyY: note.ReceiverSpendPubKeyY,
		SenderViewKeyX: note.ReceiverViewPubKeyX, SenderViewKeyY: note.ReceiverViewPubKeyY,
		RecipientSpendKeyX: note.ReceiverSpendPubKeyX, RecipientSpendKeyY: note.ReceiverSpendPubKeyY,
		RecipientViewKeyX: note.ReceiverViewPubKeyX, RecipientViewKeyY: note.ReceiverViewPubKeyY,
		DisclosureBlinding: big.NewInt(47),
	})
	if err != nil {
		f.Fatalf("build DisclosurePlaintextV1 seed: %v", err)
	}
	f.Add(valid)
	f.Add([]byte(nil))
	f.Add(make([]byte, DisclosurePlaintextV1Size))
	f.Add(make([]byte, DisclosurePlaintextV1Size+1))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > DisclosurePlaintextV1Size+1 {
			return
		}
		payload, err := UnmarshalDisclosurePlaintextV1(encoded)
		if err != nil {
			return
		}
		roundTrip, err := MarshalDisclosurePlaintextV1(payload)
		if err != nil {
			t.Fatalf("accepted disclosure did not re-encode: %v", err)
		}
		if !bytes.Equal(encoded, roundTrip) {
			t.Fatal("accepted DisclosurePlaintextV1 was not canonical")
		}
	})
}

func FuzzMsgBatchTransferDecoderAndCanonicalPayload(f *testing.F) {
	seed := batchFramingSeed(f)
	valid, err := seed.Marshal()
	if err != nil {
		f.Fatalf("marshal MsgBatchTransfer seed: %v", err)
	}
	f.Add(valid)
	f.Add([]byte(nil))
	f.Add(make([]byte, MaxBatchTransferMessageBytesV1+1))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > MaxBatchTransferMessageBytesV1+1 {
			return
		}
		var msg MsgBatchTransfer
		if err := msg.Unmarshal(encoded); err != nil {
			return
		}
		_ = msg.ValidateBasic()
		canonical, canonicalErr := CanonicalMsgBatchTransferPayloadBytesV1(&msg)
		if canonicalErr != nil {
			return
		}
		size, err := CanonicalMsgBatchTransferPayloadSizeV1(&msg)
		if err != nil {
			t.Fatalf("canonical payload encoded but size rejected it: %v", err)
		}
		if uint64(len(canonical)) != size {
			t.Fatalf("canonical payload size mismatch: encoded=%d measured=%d", len(canonical), size)
		}
		reencoded, err := msg.Marshal()
		if err != nil {
			t.Fatalf("accepted MsgBatchTransfer did not marshal: %v", err)
		}
		var roundTrip MsgBatchTransfer
		if err := roundTrip.Unmarshal(reencoded); err != nil {
			t.Fatalf("accepted MsgBatchTransfer did not round trip: %v", err)
		}
		roundTripCanonical, err := CanonicalMsgBatchTransferPayloadBytesV1(&roundTrip)
		if err != nil || !bytes.Equal(canonical, roundTripCanonical) {
			t.Fatalf("MsgBatchTransfer canonical effect changed after protobuf round trip: %v", err)
		}
	})
}

func FuzzMsgTransferDecoderAndCanonicalPayload(f *testing.F) {
	valid, err := intentTestTransferMessage().Marshal()
	if err != nil {
		f.Fatalf("marshal MsgTransfer seed: %v", err)
	}
	f.Add(valid)
	f.Add([]byte(nil))
	f.Add(make([]byte, 1024))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > MaxBatchTransferMessageBytesV1+1 {
			return
		}
		var msg MsgTransfer
		if err := msg.Unmarshal(encoded); err != nil {
			return
		}
		_ = msg.ValidateBasic()
		canonical, err := CanonicalTransferPayloadBytesV1(&msg)
		if err != nil {
			return
		}
		reencoded, err := msg.Marshal()
		if err != nil {
			t.Fatalf("accepted MsgTransfer did not marshal: %v", err)
		}
		var roundTrip MsgTransfer
		if err := roundTrip.Unmarshal(reencoded); err != nil {
			t.Fatalf("accepted MsgTransfer did not unmarshal: %v", err)
		}
		roundTripCanonical, err := CanonicalTransferPayloadBytesV1(&roundTrip)
		if err != nil || !bytes.Equal(canonical, roundTripCanonical) {
			t.Fatalf("MsgTransfer canonical effect changed after protobuf round trip: %v", err)
		}
	})
}

func FuzzBatchVectorRootActivePrefixAndDisabledSentinel(f *testing.F) {
	f.Add(byte(0), byte(0), []byte("batch-vector-fuzz-seed"))
	f.Add(byte(1), byte(15), []byte(nil))
	f.Add(byte(2), byte(31), bytes.Repeat([]byte{0xff}, 64))
	f.Add(byte(3), byte(7), []byte("disclosure"))

	f.Fuzz(func(t *testing.T, kindByte, countByte byte, entropy []byte) {
		if len(entropy) > 4<<10 {
			return
		}
		kinds := [...]BatchVectorKindV1{
			BatchVectorNullifierV1,
			BatchVectorCommitmentV1,
			BatchVectorUserDisclosureV1,
			BatchVectorFullDisclosureV1,
		}
		kind := kinds[int(kindByte)%len(kinds)]
		capacity, err := kind.Capacity()
		if err != nil {
			t.Fatal(err)
		}
		count := uint32(countByte)%capacity + 1
		values := make([]*big.Int, capacity)
		for i := range values {
			values[i] = new(big.Int)
		}
		for i := uint32(0); i < count; i++ {
			h := sha256.New()
			_, _ = h.Write([]byte{kindByte, countByte, byte(i)})
			_, _ = h.Write(entropy)
			values[i].Mod(new(big.Int).SetBytes(h.Sum(nil)), fr.Modulus())
			if values[i].Sign() == 0 {
				values[i].SetUint64(uint64(i) + 1)
			}
		}
		root, err := ComputeBatchVectorRootV1(kind, count, values)
		if err != nil {
			t.Fatalf("valid generated vector rejected: %v", err)
		}
		copyValues := make([]*big.Int, len(values))
		for i := range values {
			copyValues[i] = new(big.Int).Set(values[i])
		}
		repeated, err := ComputeBatchVectorRootV1(kind, count, copyValues)
		if err != nil || root.Cmp(repeated) != 0 {
			t.Fatalf("vector root is not deterministic: %v", err)
		}
		if count < capacity {
			copyValues[count].SetUint64(1)
			if _, err := ComputeBatchVectorRootV1(kind, count, copyValues); err == nil {
				t.Fatal("non-zero disabled vector sentinel was accepted")
			}
		}
	})
}

func batchFramingSeed(tb testing.TB) *MsgBatchTransfer {
	tb.Helper()
	msg := productionBatchPayloadTestMessage(tb)
	msg.Creator = testCreatorAddress()
	return msg
}
