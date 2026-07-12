package types

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

const duplicateInflationRegressionMerkleDepth = 32

type duplicateInflationNativeFixture struct {
	note              Note
	commitment        *big.Int
	nullifier         *big.Int
	path              [duplicateInflationRegressionMerkleDepth]*big.Int
	pathHelpers       [duplicateInflationRegressionMerkleDepth]uint32
	root              *big.Int
	outputNotes       [2]Note
	outputCommitments [2]*big.Int
}

func TestMsgTransferValidateBasicRejectsExactDuplicateInputInflation(t *testing.T) {
	fixture := buildDuplicateInflationNativeFixture(t)
	msg := duplicateInflationFixtureMessage(t, fixture)

	// The types boundary does not carry private paths or amounts. Keep and
	// validate the exact native witness beside the message so its public fields
	// are demonstrably derived from one duplicated NoteV1 membership and outputs
	// totaling twice that note's amount.
	assertDuplicateInflationNativeFixture(t, fixture)

	control := *msg
	control.Nullifiers = cloneBatchPayloadByteSlices(msg.Nullifiers)
	decoy := fixture.note
	decoy.Randomness = big.NewInt(37)
	control.Nullifiers[1] = decoy.ComputeNullifier().FillBytes(make([]byte, expectedFieldElementBytes))
	require.NoError(t, control.ValidateBasic(), "all non-distinctness wire checks must pass")

	err := msg.ValidateBasic()
	require.ErrorContains(t, err, "nullifier index 1 duplicates index 0")
}

func buildDuplicateInflationNativeFixture(t testing.TB) duplicateInflationNativeFixture {
	t.Helper()
	ownerSpend := noteV1TestPoint(big.NewInt(17))
	ownerView := noteV1TestPoint(big.NewInt(19))
	ownerSpendX, ownerSpendY := noteV1TestPointCoordinates(ownerSpend)
	ownerViewX, ownerViewY := noteV1TestPointCoordinates(ownerView)
	assetID := ComputeAssetIDV1("uclair")

	fixture := duplicateInflationNativeFixture{
		note: Note{
			ReceiverSpendPubKeyX: ownerSpendX,
			ReceiverSpendPubKeyY: ownerSpendY,
			ReceiverViewPubKeyX:  ownerViewX,
			ReceiverViewPubKeyY:  ownerViewY,
			Amount:               big.NewInt(5),
			AssetID:              assetID,
			Randomness:           big.NewInt(31),
		},
	}
	require.NoError(t, fixture.note.ValidateV1())
	fixture.commitment = fixture.note.ComputeCommitment()
	fixture.nullifier = fixture.note.ComputeNullifier()
	fixture.root = new(big.Int).Set(fixture.commitment)
	for level := 0; level < duplicateInflationRegressionMerkleDepth; level++ {
		fixture.path[level] = EmptyNoteTreeRootV1(uint32(level))
		fixture.pathHelpers[level] = 0
		fixture.root = ComputeNoteTreeNodeV1(uint32(level), fixture.root, fixture.path[level])
	}

	recipientSpend := noteV1TestPoint(big.NewInt(23))
	recipientView := noteV1TestPoint(big.NewInt(29))
	recipientSpendX, recipientSpendY := noteV1TestPointCoordinates(recipientSpend)
	recipientViewX, recipientViewY := noteV1TestPointCoordinates(recipientView)
	fixture.outputNotes = [2]Note{
		{
			ReceiverSpendPubKeyX: recipientSpendX,
			ReceiverSpendPubKeyY: recipientSpendY,
			ReceiverViewPubKeyX:  recipientViewX,
			ReceiverViewPubKeyY:  recipientViewY,
			Amount:               big.NewInt(6),
			AssetID:              assetID,
			Randomness:           big.NewInt(41),
		},
		{
			ReceiverSpendPubKeyX: ownerSpendX,
			ReceiverSpendPubKeyY: ownerSpendY,
			ReceiverViewPubKeyX:  ownerViewX,
			ReceiverViewPubKeyY:  ownerViewY,
			Amount:               big.NewInt(4),
			AssetID:              assetID,
			Randomness:           big.NewInt(43),
		},
	}
	for i := range fixture.outputNotes {
		require.NoError(t, fixture.outputNotes[i].ValidateV1())
		fixture.outputCommitments[i] = fixture.outputNotes[i].ComputeCommitment()
	}
	return fixture
}

func duplicateInflationFixtureMessage(t *testing.T, fixture duplicateInflationNativeFixture) *MsgTransfer {
	t.Helper()
	fullDigest, err := ComputeAuditTransferDisclosureDigestBytes(
		TransferDisclosureRecipientOutputIndex,
		fixture.outputCommitments[0].FillBytes(make([]byte, expectedFieldElementBytes)),
		fixture.outputNotes[0].Amount,
		fixture.note.AssetID,
		fixture.note.ReceiverSpendPubKeyX,
		fixture.note.ReceiverSpendPubKeyY,
		fixture.note.ReceiverViewPubKeyX,
		fixture.note.ReceiverViewPubKeyY,
		fixture.outputNotes[0].ReceiverSpendPubKeyX,
		fixture.outputNotes[0].ReceiverSpendPubKeyY,
		fixture.outputNotes[0].ReceiverViewPubKeyX,
		fixture.outputNotes[0].ReceiverViewPubKeyY,
		big.NewInt(53),
	)
	require.NoError(t, err)
	nullifier := fixture.nullifier.FillBytes(make([]byte, expectedFieldElementBytes))
	return NewMsgTransferWithDisclosure(
		testCreatorAddress(),
		[]byte{1},
		fixture.root.FillBytes(make([]byte, expectedFieldElementBytes)),
		[][]byte{append([]byte(nil), nullifier...), append([]byte(nil), nullifier...)},
		[][]byte{
			fixture.outputCommitments[0].FillBytes(make([]byte, expectedFieldElementBytes)),
			fixture.outputCommitments[1].FillBytes(make([]byte, expectedFieldElementBytes)),
		},
		validTransferCipherTexts(t),
		validViewTags(),
		TransferPrivacyPolicyAllPrivate,
		nil,
		UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		nil,
		nil,
		fullDigest,
		validDisclosurePubKeyBytes(t),
		validEnvelopeBytes(t, EnvelopeAuditDisclosureV1),
		nil,
		nil,
		testExpiresAtUnix,
	)
}

func assertDuplicateInflationNativeFixture(t testing.TB, fixture duplicateInflationNativeFixture) {
	t.Helper()
	root := new(big.Int).Set(fixture.commitment)
	for level := 0; level < duplicateInflationRegressionMerkleDepth; level++ {
		require.Zero(t, fixture.pathHelpers[level])
		root = ComputeNoteTreeNodeV1(uint32(level), root, fixture.path[level])
	}
	require.Equal(t, fixture.root, root)
	require.Equal(t, fixture.commitment, fixture.note.ComputeCommitment())
	require.Equal(t, fixture.nullifier, fixture.note.ComputeNullifier())
	require.NotEqual(t, fixture.outputCommitments[0], fixture.outputCommitments[1])

	outputTotal := new(big.Int).Add(fixture.outputNotes[0].Amount, fixture.outputNotes[1].Amount)
	require.Equal(t, new(big.Int).Mul(fixture.note.Amount, big.NewInt(2)), outputTotal)
}
