package keeper

import (
	"bytes"
	"encoding/hex"
	"errors"
	"hash"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	fr_mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/signature/eddsa"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type deterministicBatchCoreFixture struct {
	msg              *privacytypes.MsgBatchTransfer
	inputCommitment  []byte
	outputCommitment []byte
	nullifier        []byte
	root             []byte
	ownerSpendScalar *big.Int
	ownerViewScalar  *big.Int
	outputRandomness *big.Int
}

type deterministicJoinSplitCoreFixture struct {
	msg               *privacytypes.MsgTransfer
	inputCommitments  [][]byte
	outputCommitments [][]byte
	nullifiers        [][]byte
	root              []byte
}

func TestBatchTransferDirectCoreIntegration(t *testing.T) {
	ensureDepositTestArtifacts(t)
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	ctx = ctx.WithBlockHeight(101).WithTxBytes([]byte("direct-batch-core-integration"))
	auditTarget := testKeeperDisclosurePubKey()
	require.NoError(t, k.SetAuditConfigV1(ctx, privacytypes.DefaultAuditKeyIDV1, privacytypes.DefaultAuditKeyEpochV1, auditTarget))
	fixture := buildDeterministicBatchCoreFixture(t, k, ctx, big.NewInt(23))

	rootBefore := append([]byte(nil), fixture.root...)
	response, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), fixture.msg)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.True(t, k.HasNullifier(ctx, fixture.nullifier))
	require.Equal(t, uint64(2), k.GetLeafCount(ctx))
	leafIndex, found, err := k.GetCommitmentIndex(ctx, fixture.outputCommitment)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(1), leafIndex)
	require.True(t, k.CheckHistoricalRoot(ctx, rootBefore))
	require.NotEqual(t, rootBefore, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))

	page, err := k.GetPrivacyScanPageV2(ctx, nil, 1, 1, MaxPrivacyScanByteLimit, nil)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	require.Len(t, page.Outputs, 1)
	require.Equal(t, privacytypes.EventTypeBatchTransferV1, page.Summaries[0].EventType)
	require.Equal(t, fixture.outputCommitment, page.Outputs[0].Commitment)
	require.Equal(t, fixture.msg.Outputs[0].Ciphertext, page.Outputs[0].Ciphertext)
	require.Equal(t, uint64(1), page.Outputs[0].LeafIndex)
	require.Len(t, ctx.EventManager().Events(), 1)
	require.Equal(t, privacytypes.EventTypeBatchTransferV1, ctx.EventManager().Events()[0].Type)
}

func TestBatchTransferCoreRejectionsAndAtomicScanFailure(t *testing.T) {
	ensureDepositTestArtifacts(t)
	baseKeeper, baseCtx, _ := setupRegisteredMsgServerKeeper(t)
	baseCtx = baseCtx.WithBlockHeight(102).WithTxBytes([]byte("batch-core-negative-fixture"))
	auditTarget := testKeeperDisclosurePubKey()
	require.NoError(t, baseKeeper.SetAuditConfigV1(baseCtx, privacytypes.DefaultAuditKeyIDV1, privacytypes.DefaultAuditKeyEpochV1, auditTarget))
	fixture := buildDeterministicBatchCoreFixture(t, baseKeeper, baseCtx, big.NewInt(23))

	newState := func(t testing.TB) (*Keeper, sdk.Context) {
		t.Helper()
		k, ctx, _ := setupRegisteredMsgServerKeeper(t)
		ctx = ctx.WithBlockHeight(102).WithTxBytes([]byte("batch-core-negative-fixture"))
		require.NoError(t, k.SetAuditConfigV1(ctx, privacytypes.DefaultAuditKeyIDV1, privacytypes.DefaultAuditKeyEpochV1, auditTarget))
		require.NoError(t, k.AppendCommitment(ctx, fixture.inputCommitment))
		return k, ctx
	}

	t.Run("global spent nullifier", func(t *testing.T) {
		k, ctx := newState(t)
		k.SetNullifier(ctx, fixture.nullifier)
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
		require.ErrorContains(t, err, "already used")
		require.Equal(t, uint64(1), k.GetLeafCount(ctx))
	})
	t.Run("audit identity must exactly match chain config", func(t *testing.T) {
		rotatedPoint := testKeeperScalarMulBase(big.NewInt(43))
		rotatedEncoded := rotatedPoint.Bytes()
		for _, tc := range []struct {
			name   string
			id     string
			epoch  uint64
			target []byte
		}{
			{name: "id", id: "audit-rotated", epoch: privacytypes.DefaultAuditKeyEpochV1, target: auditTarget},
			{name: "epoch", id: privacytypes.DefaultAuditKeyIDV1, epoch: 2, target: auditTarget},
			{name: "target", id: privacytypes.DefaultAuditKeyIDV1, epoch: privacytypes.DefaultAuditKeyEpochV1, target: rotatedEncoded[:]},
		} {
			t.Run(tc.name, func(t *testing.T) {
				k, ctx := newState(t)
				require.NoError(t, k.SetAuditConfigV1(ctx, tc.id, tc.epoch, tc.target))
				_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
				require.ErrorContains(t, err, "must exactly match active chain configuration")
				require.False(t, k.HasNullifier(ctx, fixture.nullifier))
				require.Equal(t, uint64(1), k.GetLeafCount(ctx))
			})
		}
	})
	t.Run("prior global commitment collision", func(t *testing.T) {
		k, ctx := newState(t)
		require.NoError(t, k.AppendCommitment(ctx, fixture.outputCommitment))
		countBefore := k.GetLeafCount(ctx)
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
		require.ErrorContains(t, err, "commitment 0 already exists")
		require.Equal(t, countBefore, k.GetLeafCount(ctx))
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
	})
	t.Run("wrong payload binding", func(t *testing.T) {
		k, ctx := newState(t)
		msg := cloneBatchTransferMessage(t, fixture.msg)
		msg.Outputs[0].Ciphertext[len(msg.Outputs[0].Ciphertext)-1] ^= 1
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), msg)
		require.ErrorContains(t, err, "proof verification failed")
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
		require.Equal(t, uint64(1), k.GetLeafCount(ctx))
	})
	t.Run("wrong chain domain", func(t *testing.T) {
		k, ctx := newState(t)
		ctx = ctx.WithChainID("wrong-chain-1")
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
		require.ErrorContains(t, err, "proof verification failed")
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
	})
	t.Run("expired", func(t *testing.T) {
		k, ctx := newState(t)
		ctx = ctx.WithBlockTime(time.Unix(fixture.msg.ExpiresAtUnix, 0))
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
		require.ErrorContains(t, err, "expired")
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
	})
	t.Run("invalid proof", func(t *testing.T) {
		k, ctx := newState(t)
		msg := cloneBatchTransferMessage(t, fixture.msg)
		msg.Proof[len(msg.Proof)-1] ^= 1
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), msg)
		require.ErrorContains(t, err, "proof verification failed")
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
	})
	t.Run("insufficient merkle capacity", func(t *testing.T) {
		k, ctx := newState(t)
		k.SetLeafCount(ctx, MaxMerkleLeaves)
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
		require.ErrorContains(t, err, "not enough merkle tree capacity")
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
	})
	t.Run("scan write failure rolls back nullifier and tree", func(t *testing.T) {
		k, ctx := newState(t)
		rootBefore := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
		k.batchTransitionHook = func(stage string) error {
			if stage == batchTransitionAfterCommitments {
				return errors.New("injected scan writer failure")
			}
			return nil
		}
		_, err := (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, fixture.msg))
		require.ErrorContains(t, err, "injected scan writer failure")
		require.False(t, k.HasNullifier(ctx, fixture.nullifier))
		require.Equal(t, uint64(1), k.GetLeafCount(ctx))
		require.Equal(t, rootBefore, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
		sequence, sequenceErr := k.GetPrivacyGlobalSequence(ctx)
		require.NoError(t, sequenceErr)
		require.Zero(t, sequence)
		require.Empty(t, ctx.EventManager().Events())
	})
}

func TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache(t *testing.T) {
	ensureDepositTestArtifacts(t)
	fixtureKeeper, fixtureCtx, _ := setupRegisteredMsgServerKeeper(t)
	fixtureCtx = fixtureCtx.WithBlockHeight(103).WithTxBytes([]byte("cross-message-rollback"))
	auditTarget := testKeeperDisclosurePubKey()
	require.NoError(t, fixtureKeeper.SetAuditConfigV1(fixtureCtx, privacytypes.DefaultAuditKeyIDV1, privacytypes.DefaultAuditKeyEpochV1, auditTarget))
	joinFixture := buildDeterministicJoinSplitCoreFixture(t, fixtureKeeper, fixtureCtx)
	batchA := buildDeterministicBatchCoreFixture(t, fixtureKeeper, fixtureCtx, big.NewInt(23))
	batchB := buildDeterministicBatchCoreFixture(t, fixtureKeeper, fixtureCtx, big.NewInt(41))
	require.Equal(t, joinFixture.nullifiers[0], batchA.nullifier)
	require.Equal(t, batchA.nullifier, batchB.nullifier)

	newState := func(t testing.TB) (*Keeper, sdk.Context) {
		t.Helper()
		k, ctx, _ := setupRegisteredMsgServerKeeper(t)
		ctx = ctx.WithBlockHeight(103).WithTxBytes([]byte("cross-message-rollback"))
		require.NoError(t, k.SetAuditConfigV1(ctx, privacytypes.DefaultAuditKeyIDV1, privacytypes.DefaultAuditKeyEpochV1, auditTarget))
		for _, commitment := range joinFixture.inputCommitments {
			require.NoError(t, k.AppendCommitment(ctx, commitment))
		}
		require.Equal(t, joinFixture.root, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
		return k, ctx
	}

	type call func(msgServer, sdk.Context) error
	batchCall := func(msg *privacytypes.MsgBatchTransfer) call {
		return func(server msgServer, ctx sdk.Context) error {
			_, err := server.BatchTransfer(sdk.WrapSDKContext(ctx), cloneBatchTransferMessage(t, msg))
			return err
		}
	}
	joinCall := func(server msgServer, ctx sdk.Context) error {
		_, err := server.Transfer(sdk.WrapSDKContext(ctx), cloneJoinSplitTransferMessage(t, joinFixture.msg))
		return err
	}

	for _, tc := range []struct {
		name          string
		first, second call
	}{
		{name: "2x2_then_batch", first: joinCall, second: batchCall(batchA.msg)},
		{name: "batch_then_2x2", first: batchCall(batchA.msg), second: joinCall},
		{name: "batch_a_then_batch_b", first: batchCall(batchA.msg), second: batchCall(batchB.msg)},
		{name: "batch_b_then_batch_a", first: batchCall(batchB.msg), second: batchCall(batchA.msg)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx := newState(t)
			server := msgServer{Keeper: *k}
			txCtx, _ := ctx.CacheContext()
			require.NoError(t, tc.first(server, txCtx))
			err := tc.second(server, txCtx)
			require.ErrorContains(t, err, "already used")

			// Discarding the outer tx cache must remove the first message too.
			require.Equal(t, uint64(2), k.GetLeafCount(ctx))
			require.Equal(t, joinFixture.root, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
			for _, nullifier := range append(cloneBatchByteSlices(joinFixture.nullifiers), batchA.nullifier, batchB.nullifier) {
				require.False(t, k.HasNullifier(ctx, nullifier))
			}
			for _, commitment := range append(append(cloneBatchByteSlices(joinFixture.outputCommitments), batchA.outputCommitment), batchB.outputCommitment) {
				found, lookupErr := k.HasCommitment(ctx, commitment)
				require.NoError(t, lookupErr)
				require.False(t, found)
			}
			sequence, sequenceErr := k.GetPrivacyGlobalSequence(ctx)
			require.NoError(t, sequenceErr)
			require.Zero(t, sequence)
			require.Empty(t, ctx.EventManager().Events())
		})
	}
}

func buildDeterministicJoinSplitCoreFixture(
	t testing.TB,
	k *Keeper,
	ctx sdk.Context,
) deterministicJoinSplitCoreFixture {
	t.Helper()
	ownerSpendScalar := big.NewInt(17)
	ownerSpendKey := testKeeperScalarMulBase(ownerSpendScalar)
	ownerViewKey := testKeeperScalarMulBase(big.NewInt(19))
	ownerSpendX, ownerSpendY := testKeeperPointBigInts(ownerSpendKey)
	ownerViewX, ownerViewY := testKeeperPointBigInts(ownerViewKey)
	assetID := privacytypes.ComputeAssetIDV1("uclair")
	inputAmounts := [2]*big.Int{big.NewInt(7), big.NewInt(8)}
	inputRandomness := [2]*big.Int{big.NewInt(13), big.NewInt(17)}
	inputCommitments := make([][]byte, 2)
	inputCommitmentFields := [2]*big.Int{}
	for i := range inputAmounts {
		inputCommitmentFields[i] = privacytypes.ComputeNoteCommitmentV1(
			ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
			inputAmounts[i], assetID, inputRandomness[i],
		)
		inputCommitments[i] = batchFixtureFieldBytes(inputCommitmentFields[i])
		exists, err := k.HasCommitment(ctx, inputCommitments[i])
		require.NoError(t, err)
		if !exists {
			require.NoError(t, k.AppendCommitment(ctx, inputCommitments[i]))
		}
	}

	paths := [2][]string{}
	helpers := [2][]uint32{}
	var root []byte
	for i := range inputCommitments {
		var err error
		paths[i], helpers[i], root, err = k.GetPath(ctx, inputCommitments[i])
		require.NoError(t, err)
	}

	outputAmounts := [2]*big.Int{big.NewInt(9), big.NewInt(6)}
	outputRandomness := [2]*big.Int{big.NewInt(31), big.NewInt(37)}
	outputCommitmentFields := [2]*big.Int{}
	outputCommitments := make([][]byte, 2)
	for i := range outputAmounts {
		outputCommitmentFields[i] = privacytypes.ComputeNoteCommitmentV1(
			ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
			outputAmounts[i], assetID, outputRandomness[i],
		)
		outputCommitments[i] = batchFixtureFieldBytes(outputCommitmentFields[i])
	}
	nullifierFields := [2]*big.Int{}
	nullifiers := make([][]byte, 2)
	for i := range inputAmounts {
		nullifierFields[i] = privacytypes.ComputeNoteNullifierV1(
			inputCommitmentFields[i], inputRandomness[i], ownerSpendX, ownerSpendY,
		)
		nullifiers[i] = batchFixtureFieldBytes(nullifierFields[i])
	}
	fullBlinding := big.NewInt(53)
	fullDigest, err := privacytypes.ComputeAuditTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		outputCommitments[0], outputAmounts[0], assetID,
		ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
		ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
		fullBlinding,
	)
	require.NoError(t, err)
	msg := privacytypes.NewMsgTransferWithDisclosure(
		testAddress(0x53), nil, root, nullifiers, outputCommitments,
		[][]byte{
			testKeeperEnvelopeTB(t, privacytypes.EnvelopeTransferNoteV1),
			testKeeperEnvelopeTB(t, privacytypes.EnvelopeTransferNoteV1),
		},
		[][]byte{{0x01, 0x02}, {0x03, 0x04}},
		privacytypes.TransferPrivacyPolicyAllPrivate,
		nil, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, nil, nil,
		fullDigest, testKeeperDisclosurePubKey(), testKeeperEnvelopeTB(t, privacytypes.EnvelopeAuditDisclosureV1),
		nil, nil, msgServerTestExpiry,
	)
	payloadDigest, err := privacytypes.ComputeTransferPayloadDigestV1(msg)
	require.NoError(t, err)
	chainDomain, err := privacytypes.ComputeChainDomainV1(ctx.ChainID(), privacytypes.ActiveCircuitSetID)
	require.NoError(t, err)
	assignment := &circuit.JoinSplitCircuit{
		MerkleRoot: rootAsBigInt(root), ChainDomainHi: chainDomain.Hi, ChainDomainLo: chainDomain.Lo,
		ExpiresAtUnix: big.NewInt(msg.ExpiresAtUnix), Nullifiers: [2]frontend.Variable{nullifierFields[0], nullifierFields[1]},
		Commitments:       [2]frontend.Variable{outputCommitmentFields[0], outputCommitmentFields[1]},
		UserPrivacyPolicy: big.NewInt(0), UserDisclosureDigest: big.NewInt(0),
		FullDisclosureDigest: new(big.Int).SetBytes(fullDigest),
		PayloadDigestHi:      payloadDigest.Hi, PayloadDigestLo: payloadDigest.Lo,
		AssetID: assetID, UserDisclosureBlinding: big.NewInt(0), FullDisclosureBlinding: fullBlinding,
	}
	for i := 0; i < 2; i++ {
		assignment.InputAmounts[i] = inputAmounts[i]
		assignment.InputRandomness[i] = inputRandomness[i]
		assignment.OutputAmounts[i] = outputAmounts[i]
		assignment.OutputRandomness[i] = outputRandomness[i]
		assignBatchFixturePublicKey(&assignment.InputSpendPubKeys[i], ownerSpendKey)
		assignBatchFixturePublicKey(&assignment.InputViewPubKeys[i], ownerViewKey)
		assignBatchFixturePublicKey(&assignment.OutputSpendPubKeys[i], ownerSpendKey)
		assignBatchFixturePublicKey(&assignment.OutputViewPubKeys[i], ownerViewKey)
		for level := 0; level < circuit.MerkleDepth; level++ {
			decoded, decodeErr := hex.DecodeString(paths[i][level])
			require.NoError(t, decodeErr)
			assignment.InputPaths[i][level] = new(big.Int).SetBytes(decoded)
			assignment.InputPathHelpers[i][level] = new(big.Int).SetUint64(uint64(helpers[i][level]))
		}
	}
	intent, err := privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi: chainDomain.Hi, ChainDomainLo: chainDomain.Lo,
		MerkleRoot: rootAsBigInt(root), AssetID: assetID,
		Nullifiers: nullifierFields, Commitments: outputCommitmentFields,
		UserDisclosureDigest: big.NewInt(0), FullDisclosureDigest: new(big.Int).SetBytes(fullDigest),
		PayloadDigestHi: payloadDigest.Hi, PayloadDigestLo: payloadDigest.Lo,
		ExpiresAtUnix: msg.ExpiresAtUnix,
	})
	require.NoError(t, err)
	assignment.OwnerSignature = signBatchCoreIntent(t, intent, ownerSpendScalar, ownerSpendKey)
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	proof, err := groth16.Prove(joinSplitTestR1CS, joinSplitTestPK, witness)
	require.NoError(t, err)
	var proofBytes bytes.Buffer
	_, err = proof.WriteTo(&proofBytes)
	require.NoError(t, err)
	msg.Proof = proofBytes.Bytes()
	require.NoError(t, msg.ValidateBasic())
	return deterministicJoinSplitCoreFixture{
		msg: msg, inputCommitments: inputCommitments, outputCommitments: outputCommitments,
		nullifiers: nullifiers, root: append([]byte(nil), root...),
	}
}

func buildDeterministicBatchCoreFixture(
	t testing.TB,
	k *Keeper,
	ctx sdk.Context,
	outputRandomness *big.Int,
) deterministicBatchCoreFixture {
	t.Helper()
	ownerSpendScalar := big.NewInt(17)
	ownerViewScalar := big.NewInt(19)
	ownerSpendKey := testKeeperScalarMulBase(ownerSpendScalar)
	ownerViewKey := testKeeperScalarMulBase(ownerViewScalar)
	ownerSpendX, ownerSpendY := testKeeperPointBigInts(ownerSpendKey)
	ownerViewX, ownerViewY := testKeeperPointBigInts(ownerViewKey)
	assetID := privacytypes.ComputeAssetIDV1("uclair")
	amount := big.NewInt(7)
	inputRandomness := big.NewInt(13)
	inputCommitment := privacytypes.ComputeNoteCommitmentV1(
		ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
		amount, assetID, inputRandomness,
	)
	inputCommitmentBytes := batchFixtureFieldBytes(inputCommitment)
	exists, err := k.HasCommitment(ctx, inputCommitmentBytes)
	require.NoError(t, err)
	if !exists {
		require.NoError(t, k.AppendCommitment(ctx, inputCommitmentBytes))
	}
	path, helpers, root, err := k.GetPath(ctx, inputCommitmentBytes)
	require.NoError(t, err)

	nullifier := privacytypes.ComputeNoteNullifierV1(inputCommitment, inputRandomness, ownerSpendX, ownerSpendY)
	outputCommitment := privacytypes.ComputeNoteCommitmentV1(
		ownerSpendX, ownerSpendY, ownerViewX, ownerViewY,
		amount, assetID, outputRandomness,
	)
	fullBlinding := big.NewInt(29)
	fullDigest, err := privacytypes.ComputeBatchFullDisclosureDigestV1(privacytypes.BatchFullDisclosureV1Input{
		OutputIndex: 0, Commitment: outputCommitment, Amount: amount, AssetID: assetID,
		SenderSpendKeyX: ownerSpendX, SenderSpendKeyY: ownerSpendY,
		SenderViewKeyX: ownerViewX, SenderViewKeyY: ownerViewY,
		RecipientSpendKeyX: ownerSpendX, RecipientSpendKeyY: ownerSpendY,
		RecipientViewKeyX: ownerViewX, RecipientViewKeyY: ownerViewY,
		FullDisclosureBlinding: fullBlinding,
	})
	require.NoError(t, err)

	msg := &privacytypes.MsgBatchTransfer{
		Creator:    testAddress(0x52),
		Root:       append([]byte(nil), root...),
		Nullifiers: [][]byte{batchFixtureFieldBytes(nullifier)},
		Outputs: []*privacytypes.BatchTransferOutput{{
			Commitment:             batchFixtureFieldBytes(outputCommitment),
			Ciphertext:             testKeeperEnvelopeTB(t, privacytypes.EnvelopeTransferNoteV1),
			ViewTag:                []byte{0x01, 0x02},
			UserPrivacyPolicy:      privacytypes.TransferPrivacyPolicyAllPrivate,
			UserDisclosureMode:     privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			FullDisclosureDigest:   batchFixtureFieldBytes(fullDigest),
			AuditDisclosurePayload: testKeeperEnvelopeTB(t, privacytypes.EnvelopeAuditDisclosureV1),
		}},
		AuditKeyId:                  privacytypes.DefaultAuditKeyIDV1,
		AuditKeyEpoch:               privacytypes.DefaultAuditKeyEpochV1,
		AuditDisclosureTargetPubkey: testKeeperDisclosurePubKey(),
		ExpiresAtUnix:               msgServerTestExpiry,
	}

	nullifierValues := zeroBatchFieldVector(int(privacytypes.BatchJoinSplitV1MaxInputs))
	nullifierValues[0] = nullifier
	commitmentValues := zeroBatchFieldVector(int(privacytypes.BatchJoinSplitV1MaxOutputs))
	commitmentValues[0] = outputCommitment
	fullValues := zeroBatchFieldVector(int(privacytypes.BatchJoinSplitV1MaxOutputs))
	fullValues[0] = fullDigest
	policies := make([]uint32, privacytypes.BatchJoinSplitV1MaxOutputs)
	userRaw := zeroBatchFieldVector(int(privacytypes.BatchJoinSplitV1MaxOutputs))
	nullifierRoot, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorNullifierV1, 1, nullifierValues)
	require.NoError(t, err)
	commitmentRoot, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorCommitmentV1, 1, commitmentValues)
	require.NoError(t, err)
	userRoot, err := privacytypes.ComputeBatchUserDisclosureVectorRootV1(1, policies, userRaw)
	require.NoError(t, err)
	fullRoot, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorFullDisclosureV1, 1, fullValues)
	require.NoError(t, err)
	payloadDigest, err := privacytypes.ComputeMsgBatchTransferPayloadDigestV1(msg)
	require.NoError(t, err)
	chainDomain, err := privacytypes.ComputeChainDomainV1(ctx.ChainID(), privacytypes.ActiveCircuitSetID)
	require.NoError(t, err)

	assignment := &circuit.BatchJoinSplit16x32{
		MerkleRoot: rootAsBigInt(root), ChainDomainHi: chainDomain.Hi, ChainDomainLo: chainDomain.Lo,
		ExpiresAtUnix: big.NewInt(msg.ExpiresAtUnix), InputCount: big.NewInt(1), OutputCount: big.NewInt(1),
		NullifierRoot: nullifierRoot, CommitmentRoot: commitmentRoot,
		UserDisclosureRoot: userRoot, FullDisclosureRoot: fullRoot,
		PayloadDigestHi: payloadDigest.Hi, PayloadDigestLo: payloadDigest.Lo,
		AssetID: assetID,
	}
	assignBatchFixturePublicKey(&assignment.OwnerSpendPubKey, ownerSpendKey)
	assignBatchFixturePublicKey(&assignment.OwnerViewPubKey, ownerViewKey)
	for i := 0; i < circuit.MaxBatchJoinSplitInputs; i++ {
		assignBatchFixturePublicKey(&assignment.InputSpendPubKeys[i], ownerSpendKey)
		assignBatchFixturePublicKey(&assignment.InputViewPubKeys[i], ownerViewKey)
		assignment.InputAmounts[i] = big.NewInt(0)
		assignment.InputRandomness[i] = big.NewInt(0)
		for level := 0; level < circuit.MerkleDepth; level++ {
			assignment.InputPaths[i][level] = big.NewInt(0)
			assignment.InputPathHelpers[i][level] = big.NewInt(0)
		}
	}
	assignment.InputAmounts[0] = amount
	assignment.InputRandomness[0] = inputRandomness
	for level := 0; level < circuit.MerkleDepth; level++ {
		decoded, decodeErr := hex.DecodeString(path[level])
		require.NoError(t, decodeErr)
		assignment.InputPaths[0][level] = new(big.Int).SetBytes(decoded)
		assignment.InputPathHelpers[0][level] = new(big.Int).SetUint64(uint64(helpers[level]))
	}
	for i := 0; i < circuit.MaxBatchJoinSplitOutputs; i++ {
		assignBatchFixturePublicKey(&assignment.OutputSpendPubKeys[i], ownerSpendKey)
		assignBatchFixturePublicKey(&assignment.OutputViewPubKeys[i], ownerViewKey)
		assignment.OutputAmounts[i] = big.NewInt(0)
		assignment.OutputRandomness[i] = big.NewInt(0)
		assignment.OutputPrivacyPolicies[i] = big.NewInt(0)
		assignment.UserDisclosureBlindings[i] = big.NewInt(0)
		assignment.FullDisclosureBlindings[i] = big.NewInt(0)
	}
	assignment.OutputAmounts[0] = amount
	assignment.OutputRandomness[0] = outputRandomness
	assignment.FullDisclosureBlindings[0] = fullBlinding
	intent, err := privacytypes.ComputeBatchTransferIntentV1(privacytypes.BatchTransferIntentV1Input{
		ChainDomainHi: chainDomain.Hi, ChainDomainLo: chainDomain.Lo,
		MerkleRoot: rootAsBigInt(root), InputCount: 1, OutputCount: 1, AssetID: assetID,
		NullifierRoot: nullifierRoot, CommitmentRoot: commitmentRoot,
		UserDisclosureRoot: userRoot, FullDisclosureRoot: fullRoot,
		PayloadDigestHi: payloadDigest.Hi, PayloadDigestLo: payloadDigest.Lo,
		ExpiresAtUnix: msg.ExpiresAtUnix,
	})
	require.NoError(t, err)
	assignment.OwnerSignature = signBatchCoreIntent(t, intent, ownerSpendScalar, ownerSpendKey)

	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	proof, err := groth16.Prove(batchTestR1CS, batchTestPK, witness)
	require.NoError(t, err)
	var proofBytes bytes.Buffer
	_, err = proof.WriteTo(&proofBytes)
	require.NoError(t, err)
	msg.Proof = proofBytes.Bytes()
	require.Len(t, msg.Proof, privacytypes.BatchTransferProofSizeV1)
	require.NoError(t, msg.ValidateBasic())

	return deterministicBatchCoreFixture{
		msg: msg, inputCommitment: inputCommitmentBytes,
		outputCommitment: batchFixtureFieldBytes(outputCommitment),
		nullifier:        batchFixtureFieldBytes(nullifier), root: append([]byte(nil), root...),
		ownerSpendScalar: ownerSpendScalar, ownerViewScalar: ownerViewScalar,
		outputRandomness: new(big.Int).Set(outputRandomness),
	}
}

func assignBatchFixturePublicKey(target *eddsa.PublicKey, point crypto_tedwards.PointAffine) {
	x, y := testKeeperPointBigInts(point)
	target.A.X = x
	target.A.Y = y
}

func signBatchCoreIntent(
	t testing.TB,
	message, scalar *big.Int,
	publicKey crypto_tedwards.PointAffine,
) eddsa.Signature {
	t.Helper()
	curve := crypto_tedwards.GetEdwardsCurve()
	nonce := big.NewInt(19)
	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)
	var pointR crypto_tedwards.PointAffine
	pointR.ScalarMultiplication(&base, nonce)
	rx, ry := testKeeperPointBigInts(pointR)
	ax, ay := testKeeperPointBigInts(publicKey)
	hasher := fr_mimc.NewMiMC()
	for _, value := range []*big.Int{rx, ry, ax, ay, message} {
		writeBatchFixturePadded(hasher, value)
	}
	hRAM := new(big.Int).SetBytes(hasher.Sum(nil))
	s := new(big.Int).Mul(hRAM, scalar)
	s.Add(s, nonce)
	s.Mod(s, &curve.Order)
	signature := eddsa.Signature{S: s}
	signature.R.X = rx
	signature.R.Y = ry
	return signature
}

func writeBatchFixturePadded(hasher hash.Hash, value *big.Int) {
	encoded := value.FillBytes(make([]byte, 32))
	_, _ = hasher.Write(encoded)
}

func rootAsBigInt(root []byte) *big.Int {
	return new(big.Int).SetBytes(root)
}

func batchFixtureFieldBytes(value *big.Int) []byte {
	return value.FillBytes(make([]byte, 32))
}

func cloneBatchTransferMessage(t testing.TB, msg *privacytypes.MsgBatchTransfer) *privacytypes.MsgBatchTransfer {
	t.Helper()
	encoded, err := msg.Marshal()
	require.NoError(t, err)
	var cloned privacytypes.MsgBatchTransfer
	require.NoError(t, cloned.Unmarshal(encoded))
	return &cloned
}

func cloneJoinSplitTransferMessage(t testing.TB, msg *privacytypes.MsgTransfer) *privacytypes.MsgTransfer {
	t.Helper()
	encoded, err := msg.Marshal()
	require.NoError(t, err)
	var cloned privacytypes.MsgTransfer
	require.NoError(t, cloned.Unmarshal(encoded))
	return &cloned
}
