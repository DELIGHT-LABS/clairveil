package deposit

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestPreparedDepositProverPayloadRoundTripPreservesCircuitWitness(t *testing.T) {
	note := testDepositNote(t, 7)
	payload, err := BuildPreparedDepositProverPayload(note)
	require.NoError(t, err)
	require.NoError(t, ValidatePreparedDepositProverPayload(*payload))

	reconstructed, err := noteFromPreparedDepositProverPayload(*payload)
	require.NoError(t, err)
	require.True(t, note.ReceiverSpendPubKeyX.Equal(reconstructed.ReceiverSpendPubKeyX))
	require.True(t, note.ReceiverSpendPubKeyY.Equal(reconstructed.ReceiverSpendPubKeyY))
	require.True(t, note.ReceiverViewPubKeyX.Equal(reconstructed.ReceiverViewPubKeyX))
	require.True(t, note.ReceiverViewPubKeyY.Equal(reconstructed.ReceiverViewPubKeyY))
	require.Equal(t, note.Amount, reconstructed.Amount)
	require.True(t, note.AssetID.Equal(reconstructed.AssetID))
	require.True(t, note.Randomness.Equal(reconstructed.Randomness))
	commitment, err := note.CommitmentV1()
	require.NoError(t, err)
	reconstructedCommitment, err := reconstructed.CommitmentV1()
	require.NoError(t, err)
	require.True(t, commitment.Equal(reconstructedCommitment))
	require.Empty(t, reconstructed.Memo)
}

func TestBuildAuditV2WitnessUsesNormalDepositNote(t *testing.T) {
	for _, amount := range []int64{0, 7} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			note := testDepositNote(t, amount)
			key := depositAuditKey(t)
			commitment, err := note.CommitmentV1()
			require.NoError(t, err)
			commitmentBytes := commitment.Bytes()
			cipherSize, err := privacytypes.EncryptedEnvelopeV1Size(privacytypes.EnvelopeDepositNoteV1)
			require.NoError(t, err)
			cipher, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, make([]byte, cipherSize-privacytypes.EncryptedEnvelopeV1HeaderSize))
			require.NoError(t, err)
			prepared, err := PrepareAuditV2FromNormalNote(
				privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: [32]byte{2}},
				sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String(), fmt.Sprintf("%duclair", amount), note,
				&privacyv2.OutputEffect{Commitment: commitmentBytes[:], Ciphertext: cipher}, time.Now().Add(time.Hour).Unix(),
			)
			require.NoError(t, err)
			defer prepared.Clear()
			witness, err := BuildAuditV2Witness(prepared, note)
			require.NoError(t, err)
			public, err := witness.Public()
			require.NoError(t, err)
			require.Len(t, public.Vector().(fr.Vector), 23)
		})
	}
}

func TestAuditV2DepositWitnessSatisfiesP3R1CS(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to the verified P3 development bundle")
	}
	note := testDepositNote(t, 7)
	key := depositAuditKey(t)
	commitment, err := note.CommitmentV1()
	require.NoError(t, err)
	commitmentBytes := commitment.Bytes()
	cipherSize, err := privacytypes.EncryptedEnvelopeV1Size(privacytypes.EnvelopeDepositNoteV1)
	require.NoError(t, err)
	cipher, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, make([]byte, cipherSize-privacytypes.EncryptedEnvelopeV1HeaderSize))
	require.NoError(t, err)
	prepared, err := PrepareAuditV2FromNormalNote(privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: [32]byte{2}}, sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String(), "7uclair", note, &privacyv2.OutputEffect{Commitment: commitmentBytes[:], Ciphertext: cipher}, time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	defer prepared.Clear()
	full, err := BuildAuditV2Witness(prepared, note)
	require.NoError(t, err)
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: dir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	require.NoError(t, err)
	r1cs, err := registry.R1CS(privacyzk.CircuitDepositAuditField)
	require.NoError(t, err)
	require.NoError(t, r1cs.IsSolved(full))
}

func depositAuditKey(t *testing.T) auditfield.AuditKey {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = 23
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(raw)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return key
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

	assetBytes := note.AssetID.Bytes()
	randomnessBytes := note.Randomness.Bytes()
	require.Equal(t, hex.EncodeToString(assetBytes[:]), payload.AssetIDHex)
	require.Equal(t, hex.EncodeToString(randomnessBytes[:]), payload.RandomnessHex)
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

func testDepositNote(t testing.TB, amount int64) privacytypes.SecretNoteV1 {
	t.Helper()
	spend := testDepositPoint(11)
	view := testDepositPoint(13)
	spendX, spendY, err := privacycrypto.PublicPointFieldValues(spend)
	require.NoError(t, err)
	viewX, viewY, err := privacycrypto.PublicPointFieldValues(view)
	require.NoError(t, err)
	note, err := privacytypes.NewSecretNoteV1(spendX, spendY, viewX, viewY, uint64(amount), privacytypes.ComputeSecretAssetIDV1("uclair"), privacycrypto.FieldValueFromUint64(17), "")
	require.NoError(t, err)
	return *note
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
