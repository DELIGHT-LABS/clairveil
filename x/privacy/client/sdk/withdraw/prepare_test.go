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
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestPrepareSpendWithdrawBuildsAssignment(t *testing.T) {
	spendPubKey := testPubKey(11)
	viewPubKey := testPubKey(13)
	note := privacyscan.FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(spendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(spendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(viewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(viewPubKey, false),
			Amount:               big.NewInt(7),
			AssetID:              privacycrypto.HashString("uclair"),
			Randomness:           big.NewInt(701),
		},
	}

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
	require.Equal(t, 0, prepared.Assignment.AssetID.(*big.Int).Cmp(privacycrypto.HashString("uclair")))
	require.Equal(t, 0, prepared.Assignment.Recipient.(*big.Int).Cmp(new(big.Int).SetBytes(recipientBytes)))
	require.Equal(t, 0, prepared.Assignment.PathHelper[0].(int))
	require.Equal(t, 1, prepared.Assignment.PathHelper[1].(int))
}

func TestPrepareSpendWithdrawPropagatesMerkleQueryError(t *testing.T) {
	note := privacyscan.FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: big.NewInt(1),
			ReceiverSpendPubKeyY: big.NewInt(2),
			ReceiverViewPubKeyX:  big.NewInt(3),
			ReceiverViewPubKeyY:  big.NewInt(4),
			Amount:               big.NewInt(7),
			AssetID:              privacycrypto.HashString("uclair"),
			Randomness:           big.NewInt(701),
		},
	}

	_, err := PrepareSpendWithdraw(
		context.Background(),
		&stubMerklePathProvider{returnErr: fmt.Errorf("boom")},
		&stubSpendNoteHashSigner{signature: testSignatureBytes()},
		PrepareSpendWithdrawInput{
			Note:           note,
			RecipientBytes: []byte{0x01},
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

func (s *stubSpendNoteHashSigner) SignSpendNoteHash(msgHash *big.Int) ([]byte, error) {
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
