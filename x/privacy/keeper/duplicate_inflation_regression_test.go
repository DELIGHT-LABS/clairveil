package keeper

import (
	"math/big"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/stretchr/testify/require"
)

func TestMsgServerTransferRejectsExactDuplicateInputInflationBeforeProof(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	meter := newRecordingGasMeter()
	ctx = ctx.WithGasMeter(meter)

	ownerSpend := testKeeperScalarMulBase(big.NewInt(17))
	ownerView := testKeeperScalarMulBase(big.NewInt(19))
	ownerSpendX, ownerSpendY := testKeeperPointBigInts(ownerSpend)
	ownerViewX, ownerViewY := testKeeperPointBigInts(ownerView)
	assetID := privacytypes.ComputeAssetIDV1("uclair")
	note := privacytypes.Note{
		ReceiverSpendPubKeyX: ownerSpendX,
		ReceiverSpendPubKeyY: ownerSpendY,
		ReceiverViewPubKeyX:  ownerViewX,
		ReceiverViewPubKeyY:  ownerViewY,
		Amount:               big.NewInt(5),
		AssetID:              assetID,
		Randomness:           big.NewInt(31),
	}
	require.NoError(t, note.ValidateV1())
	inputCommitment := note.ComputeCommitment().FillBytes(make([]byte, 32))
	nullifier := note.ComputeNullifier().FillBytes(make([]byte, 32))
	require.NoError(t, k.AppendCommitment(ctx, inputCommitment))
	path, helpers, root, err := k.GetPath(ctx, inputCommitment)
	require.NoError(t, err)
	require.Len(t, path, MerkleDepth)
	require.Len(t, helpers, MerkleDepth)
	duplicatedPath := append([]string(nil), path...)
	duplicatedHelpers := append([]uint32(nil), helpers...)
	require.Equal(t, path, duplicatedPath)
	require.Equal(t, helpers, duplicatedHelpers)
	require.True(t, k.CheckHistoricalRoot(ctx, root))

	recipientSpend := testKeeperScalarMulBase(big.NewInt(23))
	recipientView := testKeeperScalarMulBase(big.NewInt(29))
	recipientSpendX, recipientSpendY := testKeeperPointBigInts(recipientSpend)
	recipientViewX, recipientViewY := testKeeperPointBigInts(recipientView)
	outputs := [2]privacytypes.Note{
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
	outputCommitments := make([][]byte, len(outputs))
	for i := range outputs {
		require.NoError(t, outputs[i].ValidateV1())
		outputCommitments[i] = outputs[i].ComputeCommitment().FillBytes(make([]byte, 32))
	}
	require.NotEqual(t, outputCommitments[0], outputCommitments[1])
	outputTotal := new(big.Int).Add(outputs[0].Amount, outputs[1].Amount)
	require.Equal(t, new(big.Int).Mul(note.Amount, big.NewInt(2)), outputTotal)

	fullDigest, err := privacytypes.ComputeAuditTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		outputCommitments[0],
		outputs[0].Amount,
		assetID,
		ownerSpendX,
		ownerSpendY,
		ownerViewX,
		ownerViewY,
		recipientSpendX,
		recipientSpendY,
		recipientViewX,
		recipientViewY,
		big.NewInt(53),
	)
	require.NoError(t, err)
	auditTarget := testKeeperDisclosurePubKey()
	k.SetAuditMasterPubkey(ctx, auditTarget)

	leafCountBefore := k.GetLeafCount(ctx)
	rootBefore := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	nullifiersBefore, err := k.ExportGenesisNullifiers(ctx)
	require.NoError(t, err)
	require.Empty(t, nullifiersBefore)

	err = (msgServer{Keeper: *k}).executeShieldedTransfer(ctx, shieldedTransferRequest{
		relayer:                     testAddress(0x91),
		proof:                       canonicalTestProofBytes(t),
		root:                        append([]byte(nil), root...),
		nullifiers:                  [][]byte{append([]byte(nil), nullifier...), append([]byte(nil), nullifier...)},
		newCommitments:              outputCommitments,
		cipherTexts:                 [][]byte{testKeeperEnvelopeTB(t, privacytypes.EnvelopeTransferNoteV1), testKeeperEnvelopeTB(t, privacytypes.EnvelopeTransferNoteV1)},
		viewTags:                    [][]byte{{0x01, 0x02}, {0x03, 0x04}},
		userPrivacyPolicy:           privacytypes.TransferPrivacyPolicyAllPrivate,
		userDisclosureMode:          privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		auditDisclosureDigest:       fullDigest,
		auditDisclosureTargetPubKey: auditTarget,
		auditDisclosurePayload:      testKeeperEnvelopeTB(t, privacytypes.EnvelopeAuditDisclosureV1),
		expiresAtUnix:               msgServerTestExpiry,
	})
	require.ErrorContains(t, err, "nullifier index 1 duplicates index 0")
	require.Zero(t, meter.consumed["privacy joinsplit proof verification"], "proof verification must not run")

	nullifiersAfter, exportErr := k.ExportGenesisNullifiers(ctx)
	require.NoError(t, exportErr)
	require.Empty(t, nullifiersAfter)
	require.Equal(t, leafCountBefore, k.GetLeafCount(ctx))
	require.Equal(t, rootBefore, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
	for _, commitment := range outputCommitments {
		exists, lookupErr := k.HasCommitment(ctx, commitment)
		require.NoError(t, lookupErr)
		require.False(t, exists)
	}
	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Zero(t, sequence)
	require.Empty(t, ctx.EventManager().Events())
}
