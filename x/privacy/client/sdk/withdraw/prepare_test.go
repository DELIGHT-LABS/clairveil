package withdraw

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestPrepareSpendWithdrawBuildsAssignment(t *testing.T) {
	spendPubKey := testPubKey(11)
	viewPubKey := testPubKey(13)
	note := privacyscan.FoundNote{
		VerifiedUnspent: true,
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(spendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(spendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(viewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(viewPubKey, false),
			Amount:               big.NewInt(7),
			AssetID:              privacytypes.ComputeAssetIDV1("uclair"),
			Randomness:           big.NewInt(701),
		},
	}
	nullifier, err := privacyfield.CanonicalHexFromBigInt(note.Note.ComputeNullifier())
	require.NoError(t, err)
	note.Nullifier = nullifier

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.Note.ComputeCommitment())
	require.NoError(t, err)

	provider := &stubMerklePathProvider{
		paths: map[string]*MerklePathResult{
			commitmentHex: {
				Root:       rootBytes,
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}
	signer := &stubSpendNoteHashSigner{signature: testSignatureBytes()}
	recipientBytes := []byte{0x01, 0x02, 0x03}

	prepared, err := PrepareSpendWithdraw(
		context.Background(),
		provider,
		signer,
		PrepareSpendWithdrawInput{
			Note:           note,
			RecipientBytes: recipientBytes,
			ChainID:        "clairveil-test-1",
			ExpiresAtUnix:  2_000_000_000,
		},
	)
	require.NoError(t, err)
	require.Equal(t, rootBytes, prepared.RootBytes)
	require.Len(t, prepared.NullifierBytes, 32)
	require.Equal(t, []string{"01", "02"}, prepared.MerklePath)
	require.Equal(t, []uint32{0, 1}, prepared.PathHelper)
	require.Equal(t, testSignatureBytes(), prepared.Signature)
	require.Len(t, provider.requests, 1)
	require.Len(t, signer.hashes, 1)
	require.Equal(t, 0, prepared.Assignment.Amount.(*big.Int).Cmp(big.NewInt(7)))
	require.Equal(t, 0, prepared.Assignment.AssetID.(*big.Int).Cmp(privacytypes.ComputeAssetIDV1("uclair")))
	recipientDigest, err := privacytypes.ComputeWithdrawRecipientDigestV1(recipientBytes)
	require.NoError(t, err)
	require.Equal(t, 0, prepared.Assignment.RecipientDigestHi.(*big.Int).Cmp(recipientDigest.Hi))
	require.Equal(t, 0, prepared.Assignment.RecipientDigestLo.(*big.Int).Cmp(recipientDigest.Lo))
	expectedIntent, err := privacytypes.ComputeSpendIntentV2(privacytypes.SpendIntentV2Input{
		ChainDomainHi:     prepared.Assignment.ChainDomainHi.(*big.Int),
		ChainDomainLo:     prepared.Assignment.ChainDomainLo.(*big.Int),
		MerkleRoot:        prepared.Assignment.MerkleRoot.(*big.Int),
		Nullifier:         prepared.Assignment.Nullifier.(*big.Int),
		Amount:            prepared.Assignment.Amount.(*big.Int),
		AssetID:           prepared.Assignment.AssetID.(*big.Int),
		RecipientDigestHi: prepared.Assignment.RecipientDigestHi.(*big.Int),
		RecipientDigestLo: prepared.Assignment.RecipientDigestLo.(*big.Int),
		ExpiresAtUnix:     prepared.Assignment.ExpiresAtUnix.(*big.Int).Int64(),
	})
	require.NoError(t, err)
	require.Equal(t, 0, signer.hashes[0].Cmp(expectedIntent))
	require.Equal(t, 0, prepared.Assignment.PathHelper[0].(int))
	require.Equal(t, 1, prepared.Assignment.PathHelper[1].(int))
}

func TestPrepareSpendWithdrawPropagatesMerkleQueryError(t *testing.T) {
	spendPubKey := testPubKey(11)
	viewPubKey := testPubKey(13)
	note := privacyscan.FoundNote{
		VerifiedUnspent: true,
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(spendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(spendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(viewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(viewPubKey, false),
			Amount:               big.NewInt(7),
			AssetID:              privacytypes.ComputeAssetIDV1("uclair"),
			Randomness:           big.NewInt(701),
		},
	}
	nullifier, err := privacyfield.CanonicalHexFromBigInt(note.Note.ComputeNullifier())
	require.NoError(t, err)
	note.Nullifier = nullifier

	_, err = PrepareSpendWithdraw(
		context.Background(),
		&stubMerklePathProvider{returnErr: fmt.Errorf("boom")},
		&stubSpendNoteHashSigner{signature: testSignatureBytes()},
		PrepareSpendWithdrawInput{
			Note:           note,
			RecipientBytes: []byte{0x01},
			ChainID:        "clairveil-test-1",
			ExpiresAtUnix:  2_000_000_000,
		},
	)
	require.ErrorContains(t, err, "merkle path query failed for the selected note")
	require.ErrorContains(t, err, "boom")
}

type stubMerklePathProvider struct {
	paths     map[string]*MerklePathResult
	requests  []string
	returnErr error
}

func (s *stubMerklePathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*MerklePathResult, error) {
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	s.requests = append(s.requests, commitmentHex)
	result, ok := s.paths[commitmentHex]
	if !ok {
		return nil, fmt.Errorf("missing path for %s", commitmentHex)
	}
	return result, nil
}

type stubSpendNoteHashSigner struct {
	signature []byte
	hashes    []*big.Int
	returnErr error
}

func (s *stubSpendNoteHashSigner) SignSpendIntent(msgHash *big.Int) ([]byte, error) {
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	s.hashes = append(s.hashes, new(big.Int).Set(msgHash))
	return append([]byte(nil), s.signature...), nil
}

func testPubKey(value int64) *crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&curve.Base, big.NewInt(value))
	return &pubKey
}

func pointCoordinate(point *crypto_tedwards.PointAffine, x bool) *big.Int {
	value := new(big.Int)
	if x {
		point.X.BigInt(value)
		return value
	}
	point.Y.BigInt(value)
	return value
}

func testSignatureBytes() []byte {
	signaturePubKey := testPubKey(17)
	pointBytes := signaturePubKey.Bytes()
	signatureBytes := make([]byte, 64)
	copy(signatureBytes[:32], pointBytes[:])

	sValue := big.NewInt(19).Bytes()
	copy(signatureBytes[64-len(sValue):], sValue)
	return signatureBytes
}
