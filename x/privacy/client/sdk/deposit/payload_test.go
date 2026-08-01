package deposit

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func TestPreparedDepositProverPayloadRoundTripPreservesCircuitWitness(t *testing.T) {
	note := testDepositNote(t, 7)
	payload, err := BuildPreparedDepositProverPayload(note)
	require.NoError(t, err)
	require.NoError(t, ValidatePreparedDepositProverPayload(*payload))

	reconstructed, err := noteFromPreparedDepositProverPayload(*payload)
	require.NoError(t, err)
	require.Equal(t, note.ReceiverSpendPubKeyX, reconstructed.ReceiverSpendPubKeyX)
	require.Equal(t, note.ReceiverSpendPubKeyY, reconstructed.ReceiverSpendPubKeyY)
	require.Equal(t, note.ReceiverViewPubKeyX, reconstructed.ReceiverViewPubKeyX)
	require.Equal(t, note.ReceiverViewPubKeyY, reconstructed.ReceiverViewPubKeyY)
	require.Equal(t, note.Amount, reconstructed.Amount)
	require.Equal(t, note.AssetID, reconstructed.AssetID)
	require.Equal(t, note.Randomness, reconstructed.Randomness)
	require.Equal(t, note.ComputeCommitment(), reconstructed.ComputeCommitment())
	require.Empty(t, reconstructed.Memo)
}

func TestPreparedDepositProverPayloadPreservesZeroAmountAndCanonicalEncodings(t *testing.T) {
	note := testDepositNote(t, 0)
	payload, err := BuildPreparedDepositProverPayload(note)
	require.NoError(t, err)
	require.Equal(t, "0", payload.Amount)

	spend := testDepositPoint(11)
	view := testDepositPoint(13)
	spendBytes := spend.Bytes()
	viewBytes := view.Bytes()
	require.Equal(t, hex.EncodeToString(spendBytes[:]), payload.ReceiverSpendPubKeyHex)
	require.Equal(t, hex.EncodeToString(viewBytes[:]), payload.ReceiverViewPubKeyHex)

	assetBytes := make([]byte, privacyfield.ByteSize)
	note.AssetID.FillBytes(assetBytes)
	randomnessBytes := make([]byte, privacyfield.ByteSize)
	note.Randomness.FillBytes(randomnessBytes)
	require.Equal(t, hex.EncodeToString(assetBytes), payload.AssetIDHex)
	require.Equal(t, hex.EncodeToString(randomnessBytes), payload.RandomnessHex)
}

func TestValidatePreparedDepositProverPayloadRejectsInvalidWitnesses(t *testing.T) {
	base := testDepositPayload(t)
	identity := crypto_tedwards.PointAffine{}
	identity.Y.SetOne()
	identityBytes := identity.Bytes()
	orderTwo := crypto_tedwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	orderTwoBytes := orderTwo.Bytes()

	tests := []struct {
		name   string
		mutate func(*PreparedDepositProverPayload)
	}{
		{name: "wrong version", mutate: func(p *PreparedDepositProverPayload) { p.Version = "v2" }},
		{name: "uppercase hex", mutate: func(p *PreparedDepositProverPayload) { p.AssetIDHex = "A" + p.AssetIDHex[1:] }},
		{name: "0x prefix", mutate: func(p *PreparedDepositProverPayload) { p.RandomnessHex = "0x" + p.RandomnessHex[2:] }},
		{name: "odd length hex", mutate: func(p *PreparedDepositProverPayload) { p.AssetIDHex = p.AssetIDHex[:63] }},
		{name: "wrong width hex", mutate: func(p *PreparedDepositProverPayload) { p.RandomnessHex += "00" }},
		{name: "malformed point", mutate: func(p *PreparedDepositProverPayload) { p.ReceiverSpendPubKeyHex = strings.Repeat("00", 32) }},
		{name: "off curve point", mutate: func(p *PreparedDepositProverPayload) {
			p.ReceiverSpendPubKeyHex = hex.EncodeToString(testDepositOffCurveEncoding(t))
		}},
		{name: "identity point", mutate: func(p *PreparedDepositProverPayload) { p.ReceiverSpendPubKeyHex = hex.EncodeToString(identityBytes[:]) }},
		{name: "non subgroup point", mutate: func(p *PreparedDepositProverPayload) { p.ReceiverSpendPubKeyHex = hex.EncodeToString(orderTwoBytes[:]) }},
		{name: "leading zero amount", mutate: func(p *PreparedDepositProverPayload) { p.Amount = "07" }},
		{name: "non decimal amount", mutate: func(p *PreparedDepositProverPayload) { p.Amount = "7x" }},
		{name: "out of range amount", mutate: func(p *PreparedDepositProverPayload) {
			p.Amount = new(big.Int).Lsh(big.NewInt(1), privacytypes.ShieldedAmountBitLength+1).String()
		}},
		{name: "noncanonical asset id", mutate: func(p *PreparedDepositProverPayload) { p.AssetIDHex = hex.EncodeToString(fr.Modulus().Bytes()) }},
		{name: "noncanonical randomness", mutate: func(p *PreparedDepositProverPayload) { p.RandomnessHex = hex.EncodeToString(fr.Modulus().Bytes()) }},
		{name: "zero commitment", mutate: func(p *PreparedDepositProverPayload) { p.NoteCommitmentHex = strings.Repeat("00", 32) }},
		{name: "noncanonical commitment", mutate: func(p *PreparedDepositProverPayload) { p.NoteCommitmentHex = hex.EncodeToString(fr.Modulus().Bytes()) }},
		{name: "amount commitment mismatch", mutate: func(p *PreparedDepositProverPayload) { p.Amount = "8" }},
		{name: "key commitment mismatch", mutate: func(p *PreparedDepositProverPayload) { p.ReceiverViewPubKeyHex = testDepositPointHex(17) }},
		{name: "asset id commitment mismatch", mutate: func(p *PreparedDepositProverPayload) { p.AssetIDHex = testDepositFieldHex(big.NewInt(42)) }},
		{name: "randomness commitment mismatch", mutate: func(p *PreparedDepositProverPayload) { p.RandomnessHex = testDepositFieldHex(big.NewInt(43)) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := base
			tt.mutate(&payload)
			require.Error(t, ValidatePreparedDepositProverPayload(payload))
		})
	}
}

func TestValidatePreparedDepositProofRejectsInvalidVersionsBindingsAndProofs(t *testing.T) {
	payload := testDepositPayload(t)
	invalidProofHex := strings.Repeat("00", privacyzk.CanonicalBN254Groth16ProofSize)

	tests := []struct {
		name   string
		mutate func(*PreparedDepositProof)
	}{
		{name: "wrong proof version", mutate: func(p *PreparedDepositProof) { p.Version = "v2" }},
		{name: "response commitment mismatch", mutate: func(p *PreparedDepositProof) { p.NoteCommitmentHex = testDepositFieldHex(big.NewInt(1)) }},
		{name: "malformed proof hex", mutate: func(p *PreparedDepositProof) {
			p.ProofHex = strings.Repeat("gg", privacyzk.CanonicalBN254Groth16ProofSize)
		}},
		{name: "wrong proof size", mutate: func(p *PreparedDepositProof) { p.ProofHex = "00" }},
		{name: "noncanonical proof frame", mutate: func(p *PreparedDepositProof) { p.ProofHex = invalidProofHex }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof := PreparedDepositProof{
				Version:           PreparedDepositProofVersion,
				NoteCommitmentHex: payload.NoteCommitmentHex,
				ProofHex:          invalidProofHex,
			}
			tt.mutate(&proof)
			require.Error(t, ValidatePreparedDepositProof(payload, proof))
		})
	}
}

func testDepositNote(t testing.TB, amount int64) privacytypes.Note {
	t.Helper()
	spend := testDepositPoint(11)
	view := testDepositPoint(13)
	return privacytypes.Note{
		ReceiverSpendPubKeyX: spend.X.BigInt(new(big.Int)),
		ReceiverSpendPubKeyY: spend.Y.BigInt(new(big.Int)),
		ReceiverViewPubKeyX:  view.X.BigInt(new(big.Int)),
		ReceiverViewPubKeyY:  view.Y.BigInt(new(big.Int)),
		Amount:               big.NewInt(amount),
		AssetID:              privacytypes.ComputeAssetIDV1("uclair"),
		Randomness:           big.NewInt(17),
	}
}

func testDepositPayload(t testing.TB) PreparedDepositProverPayload {
	t.Helper()
	payload, err := BuildPreparedDepositProverPayload(testDepositNote(t, 7))
	require.NoError(t, err)
	return *payload
}

func testDepositPoint(scalar int64) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(scalar))
	return point
}

func testDepositPointHex(scalar int64) string {
	point := testDepositPoint(scalar)
	encoded := point.Bytes()
	return hex.EncodeToString(encoded[:])
}

func testDepositFieldHex(value *big.Int) string {
	encoded := make([]byte, privacyfield.ByteSize)
	value.FillBytes(encoded)
	return hex.EncodeToString(encoded)
}

func testDepositOffCurveEncoding(t testing.TB) []byte {
	t.Helper()
	for value := uint64(2); value < 1<<16; value++ {
		encoded := make([]byte, 32)
		new(big.Int).SetUint64(value).FillBytes(encoded)
		for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
			encoded[i], encoded[j] = encoded[j], encoded[i]
		}
		var point crypto_tedwards.PointAffine
		_, err := point.SetBytes(encoded)
		require.NoError(t, err)
		canonical := point.Bytes()
		if !point.IsOnCurve() && bytes.Equal(canonical[:], encoded) {
			return encoded
		}
	}
	t.Fatal("failed to find a canonical off-curve point encoding")
	return nil
}
