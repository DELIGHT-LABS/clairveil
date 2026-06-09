package transfer

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

func TestPrepareJoinSplitTransferBuildsAssignmentAndOutputs(t *testing.T) {
	senderSpendScalar, senderSpendPubKey := testScalarAndPubKey(61)
	senderViewScalar, senderViewPubKey := testScalarAndPubKey(67)
	recipientSpendScalar, recipientSpendPubKey := testScalarAndPubKey(71)
	recipientViewScalar, recipientViewPubKey := testScalarAndPubKey(73)

	inputs := [2]privacyscan.FoundNote{
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(7),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(701),
				Memo:                 "input-1",
			},
		},
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(5),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(702),
				Memo:                 "input-2",
			},
		},
	}

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)

	merkleProvider := &stubMerklePathProvider{
		paths: map[string]*MerklePathResult{},
	}
	for _, input := range inputs {
		commitmentHex, err := privacyfield.CanonicalHexFromBigInt(input.Note.ComputeCommitment())
		require.NoError(t, err)
		merkleProvider.paths[commitmentHex] = &MerklePathResult{
			Root:       rootBytes,
			Path:       []string{"01", "02"},
			PathHelper: []uint32{0, 1},
		}
	}

	signature := testSignatureBytes(t)
	signer := &stubNoteHashSigner{signature: signature}

	prepared, err := PrepareJoinSplitTransfer(
		context.Background(),
		merkleProvider,
		signer,
		PrepareJoinSplitInput{
			Inputs:               inputs,
			RecipientSpendPubKey: recipientSpendPubKey,
			RecipientViewPubKey:  recipientViewPubKey,
			TransferAmount:       big.NewInt(7),
			SenderSpendPubKey:    senderSpendPubKey,
			SenderViewPubKey:     senderViewPubKey,
		},
	)
	require.NoError(t, err)
	require.Len(t, merkleProvider.requests, 2)
	require.Len(t, signer.hashes, 2)
	require.Equal(t, rootBytes, prepared.CommonRoot)
	require.Len(t, prepared.InputNullifiers, 2)
	require.Len(t, prepared.OutputCommitments, 2)
	require.Equal(t, int64(7), prepared.RecipientNote.Amount.Int64())
	require.Equal(t, int64(5), prepared.ChangeNote.Amount.Int64())
	assignmentAssetID, ok := prepared.Assignment.AssetID.(*big.Int)
	require.True(t, ok)
	require.Equal(t, 0, assignmentAssetID.Cmp(privacycrypto.HashString("uclair")))
	require.Equal(t, 0, prepared.Assignment.OutputAmounts[0].(*big.Int).Cmp(big.NewInt(7)))
	require.Equal(t, 0, prepared.Assignment.OutputAmounts[1].(*big.Int).Cmp(big.NewInt(5)))
	require.Equal(t, 0, prepared.Assignment.InputPathHelpers[0][0].(int))
	require.Equal(t, 1, prepared.Assignment.InputPathHelpers[0][1].(int))

	recipientPlainText, err := privacycrypto.AsymDecrypt(mustEncryptPreparedNote(t, prepared.RecipientNote), recipientViewScalar)
	require.NoError(t, err)
	require.NotEmpty(t, recipientPlainText)
	changePlainText, err := privacycrypto.AsymDecrypt(mustEncryptPreparedNote(t, prepared.ChangeNote), senderViewScalar)
	require.NoError(t, err)
	require.NotEmpty(t, changePlainText)

	require.NotNil(t, senderSpendScalar)
	require.NotNil(t, senderViewScalar)
	require.NotNil(t, recipientSpendScalar)
	require.NotNil(t, recipientViewScalar)
}

func TestPrepareJoinSplitTransferRejectsMerkleRootMismatch(t *testing.T) {
	senderSpendScalar, senderSpendPubKey := testScalarAndPubKey(79)
	senderViewScalar, senderViewPubKey := testScalarAndPubKey(83)
	recipientSpendScalar, recipientSpendPubKey := testScalarAndPubKey(89)
	recipientViewScalar, recipientViewPubKey := testScalarAndPubKey(97)

	inputs := [2]privacyscan.FoundNote{
		{Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
			Amount:               big.NewInt(4),
			AssetID:              privacycrypto.HashString("uclair"),
			Randomness:           big.NewInt(801),
		}},
		{Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
			Amount:               big.NewInt(6),
			AssetID:              privacycrypto.HashString("uclair"),
			Randomness:           big.NewInt(802),
		}},
	}

	rootA, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(1001))
	require.NoError(t, err)
	rootB, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(1002))
	require.NoError(t, err)

	merkleProvider := &stubMerklePathProvider{paths: map[string]*MerklePathResult{}}
	for i, input := range inputs {
		commitmentHex, err := privacyfield.CanonicalHexFromBigInt(input.Note.ComputeCommitment())
		require.NoError(t, err)
		root := rootA
		if i == 1 {
			root = rootB
		}
		merkleProvider.paths[commitmentHex] = &MerklePathResult{
			Root:       root,
			Path:       []string{"03"},
			PathHelper: []uint32{1},
		}
	}

	_, err = PrepareJoinSplitTransfer(
		context.Background(),
		merkleProvider,
		&stubNoteHashSigner{signature: testSignatureBytes(t)},
		PrepareJoinSplitInput{
			Inputs:               inputs,
			RecipientSpendPubKey: recipientSpendPubKey,
			RecipientViewPubKey:  recipientViewPubKey,
			TransferAmount:       big.NewInt(4),
			SenderSpendPubKey:    senderSpendPubKey,
			SenderViewPubKey:     senderViewPubKey,
		},
	)
	require.ErrorContains(t, err, "merkle root mismatch")

	require.NotNil(t, senderSpendScalar)
	require.NotNil(t, senderViewScalar)
	require.NotNil(t, recipientSpendScalar)
	require.NotNil(t, recipientViewScalar)
}

func TestPrepareJoinSplitTransferRejectsOverTransfer(t *testing.T) {
	fixture := newPrepareJoinSplitFixture(t, []uint32{0, 1})

	_, err := PrepareJoinSplitTransfer(
		context.Background(),
		fixture.merkleProvider,
		&stubNoteHashSigner{signature: testSignatureBytes(t)},
		PrepareJoinSplitInput{
			Inputs:               fixture.inputs,
			RecipientSpendPubKey: fixture.recipientSpendPubKey,
			RecipientViewPubKey:  fixture.recipientViewPubKey,
			TransferAmount:       big.NewInt(13),
			SenderSpendPubKey:    fixture.senderSpendPubKey,
			SenderViewPubKey:     fixture.senderViewPubKey,
		},
	)
	require.ErrorContains(t, err, "transfer amount exceeds selected input total")
}

func TestPrepareJoinSplitTransferRejectsChangeAmountAboveShieldedLimit(t *testing.T) {
	fixture := newPrepareJoinSplitFixture(t, []uint32{0, 1})
	maxAmount := privacytypes.MaxShieldedAmount()
	fixture.inputs[0].Note.Amount = new(big.Int).Set(maxAmount)
	fixture.inputs[1].Note.Amount = new(big.Int).Set(maxAmount)

	_, err := PrepareJoinSplitTransfer(
		context.Background(),
		fixture.merkleProvider,
		&stubNoteHashSigner{signature: testSignatureBytes(t)},
		PrepareJoinSplitInput{
			Inputs:               fixture.inputs,
			RecipientSpendPubKey: fixture.recipientSpendPubKey,
			RecipientViewPubKey:  fixture.recipientViewPubKey,
			TransferAmount:       big.NewInt(1),
			SenderSpendPubKey:    fixture.senderSpendPubKey,
			SenderViewPubKey:     fixture.senderViewPubKey,
		},
	)
	require.ErrorContains(t, err, "change amount exceeds 64-bit shielded amount limit")
	require.Empty(t, fixture.merkleProvider.requests)
}

func TestPrepareJoinSplitTransferRejectsInvalidPathHelper(t *testing.T) {
	fixture := newPrepareJoinSplitFixture(t, []uint32{0, 2})

	_, err := PrepareJoinSplitTransfer(
		context.Background(),
		fixture.merkleProvider,
		&stubNoteHashSigner{signature: testSignatureBytes(t)},
		PrepareJoinSplitInput{
			Inputs:               fixture.inputs,
			RecipientSpendPubKey: fixture.recipientSpendPubKey,
			RecipientViewPubKey:  fixture.recipientViewPubKey,
			TransferAmount:       big.NewInt(7),
			SenderSpendPubKey:    fixture.senderSpendPubKey,
			SenderViewPubKey:     fixture.senderViewPubKey,
		},
	)
	require.ErrorContains(t, err, "path helper 1 must be 0 or 1")
}

type prepareJoinSplitFixture struct {
	inputs               [2]privacyscan.FoundNote
	merkleProvider       *stubMerklePathProvider
	senderSpendPubKey    *crypto_tedwards.PointAffine
	senderViewPubKey     *crypto_tedwards.PointAffine
	recipientSpendPubKey *crypto_tedwards.PointAffine
	recipientViewPubKey  *crypto_tedwards.PointAffine
}

func newPrepareJoinSplitFixture(t *testing.T, pathHelper []uint32) prepareJoinSplitFixture {
	t.Helper()

	_, senderSpendPubKey := testScalarAndPubKey(61)
	_, senderViewPubKey := testScalarAndPubKey(67)
	_, recipientSpendPubKey := testScalarAndPubKey(71)
	_, recipientViewPubKey := testScalarAndPubKey(73)

	inputs := [2]privacyscan.FoundNote{
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(7),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(701),
			},
		},
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(5),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(702),
			},
		},
	}

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)

	merkleProvider := &stubMerklePathProvider{paths: map[string]*MerklePathResult{}}
	for _, input := range inputs {
		commitmentHex, err := privacyfield.CanonicalHexFromBigInt(input.Note.ComputeCommitment())
		require.NoError(t, err)
		merkleProvider.paths[commitmentHex] = &MerklePathResult{
			Root:       rootBytes,
			Path:       []string{"01", "02"},
			PathHelper: append([]uint32(nil), pathHelper...),
		}
	}

	return prepareJoinSplitFixture{
		inputs:               inputs,
		merkleProvider:       merkleProvider,
		senderSpendPubKey:    senderSpendPubKey,
		senderViewPubKey:     senderViewPubKey,
		recipientSpendPubKey: recipientSpendPubKey,
		recipientViewPubKey:  recipientViewPubKey,
	}
}

type stubMerklePathProvider struct {
	paths    map[string]*MerklePathResult
	requests []string
}

func (s *stubMerklePathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*MerklePathResult, error) {
	s.requests = append(s.requests, commitmentHex)
	result, ok := s.paths[commitmentHex]
	if !ok {
		return nil, fmt.Errorf("missing stub merkle path for %s", commitmentHex)
	}
	return result, nil
}

type stubNoteHashSigner struct {
	hashes    []*big.Int
	signature []byte
	returnErr error
}

func (s *stubNoteHashSigner) SignNoteHash(msgHash *big.Int) ([]byte, error) {
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	s.hashes = append(s.hashes, new(big.Int).Set(msgHash))
	return append([]byte(nil), s.signature...), nil
}

func testSignatureBytes(t *testing.T) []byte {
	t.Helper()

	signatureScalar, signaturePubKey := testScalarAndPubKey(103)
	require.NotNil(t, signatureScalar)

	pointBytes := signaturePubKey.Bytes()
	signatureBytes := make([]byte, 64)
	copy(signatureBytes[:32], pointBytes[:])

	sValue := big.NewInt(107).Bytes()
	copy(signatureBytes[64-len(sValue):], sValue)
	return signatureBytes
}

func mustEncryptPreparedNote(t *testing.T, note privacytypes.Note) []byte {
	t.Helper()

	cipherTexts, err := EncryptOutputNotes(note, note)
	require.NoError(t, err)
	return cipherTexts[0]
}
