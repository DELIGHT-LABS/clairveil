package transfer

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestRecursivePlannerDecisionReturnsFinalTransfer(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(131)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(137)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(139)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(149)

	runtime := NewRecursivePlannerRuntime()
	decision, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes: []privacyscan.FoundNote{
			{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
			{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		},
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(12),
		Step:                      1,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.NoError(t, err)
	require.Equal(t, RecursivePlannerActionFinalTransfer, decision.Action)
	require.True(t, decision.IsFinal)
	require.Equal(t, int64(12), decision.SendAmount.Int64())
	require.Equal(t, finalSpendPubKey.Bytes(), decision.RecipientSpendPubKey.Bytes())
	require.Equal(t, finalViewPubKey.Bytes(), decision.RecipientViewPubKey.Bytes())

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}

func TestRecursivePlannerDecisionReturnsSelfMerge(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(151)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(157)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(163)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(167)

	runtime := NewRecursivePlannerRuntime()
	decision, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes: []privacyscan.FoundNote{
			{Note: privacytypes.Note{Amount: big.NewInt(2), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
			{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
			{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "c", Height: 3, IsSpent: false},
		},
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(20),
		Step:                      1,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.NoError(t, err)
	require.Equal(t, RecursivePlannerActionSelfMerge, decision.Action)
	require.False(t, decision.IsFinal)
	require.Equal(t, int64(12), decision.SendAmount.Int64())
	require.Equal(t, selfSpendPubKey.Bytes(), decision.RecipientSpendPubKey.Bytes())
	require.Equal(t, selfViewPubKey.Bytes(), decision.RecipientViewPubKey.Bytes())

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}

func TestRecursivePlannerDecisionRequestsDummyWhenAllowed(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(173)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(179)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(181)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(191)

	runtime := NewRecursivePlannerRuntime()
	decision, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes: []privacyscan.FoundNote{
			{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
			{Note: privacytypes.Note{Amount: big.NewInt(0), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
		},
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(10),
		Step:                      1,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.NoError(t, err)
	require.Equal(t, RecursivePlannerActionPrepareDummy, decision.Action)

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}

func TestRecursivePlannerDecisionRejectsDummyWhenDisabled(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(193)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(197)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(199)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(211)

	runtime := NewRecursivePlannerRuntime()
	_, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes: []privacyscan.FoundNote{
			{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
			{Note: privacytypes.Note{Amount: big.NewInt(0), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
		},
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(10),
		Step:                      1,
		AutoDummy:                 false,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.ErrorContains(t, err, "explicit zero-value dummy note")
	require.ErrorContains(t, err, "two-input transfer circuit")
	require.ErrorContains(t, err, "Retry with --auto-dummy=true")

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}

func TestRecursivePlannerDecisionRejectsInsufficientFundsWithSummary(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(239)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(241)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(251)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(257)

	runtime := NewRecursivePlannerRuntime()
	_, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes: []privacyscan.FoundNote{
			{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		},
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(10),
		Step:                      1,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.ErrorContains(t, err, "insufficient shielded funds for 10uclair")
	require.ErrorContains(t, err, "spendable uclair total is only 5uclair across 1 notes")
	require.ErrorContains(t, err, "Run list-notes")

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}

func TestRecursivePlannerDecisionRejectsInsufficientFundsWithoutSameDenomNotes(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(263)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(269)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(271)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(277)

	runtime := NewRecursivePlannerRuntime()
	_, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes: []privacyscan.FoundNote{
			{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
		},
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(10),
		Step:                      1,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.ErrorContains(t, err, "insufficient shielded funds for 10uclair")
	require.ErrorContains(t, err, "no spendable uclair notes were found")
	require.ErrorContains(t, err, "deposit more shielded funds")

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}

func TestRecursivePlannerDecisionRejectsRepeatedFingerprint(t *testing.T) {
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(223)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(227)
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(229)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(233)

	foundNotes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(2), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "c", Height: 3, IsSpent: false},
	}

	runtime := NewRecursivePlannerRuntime()
	_, err := runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes:                foundNotes,
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(20),
		Step:                      1,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.NoError(t, err)

	_, err = runtime.DecideNextStep(RecursivePlannerInput{
		FoundNotes:                foundNotes,
		TargetDenom:               "uclair",
		TargetAmount:              big.NewInt(20),
		Step:                      2,
		AutoDummy:                 true,
		FinalRecipientSpendPubKey: finalSpendPubKey,
		FinalRecipientViewPubKey:  finalViewPubKey,
		SelfSpendPubKey:           selfSpendPubKey,
		SelfViewPubKey:            selfViewPubKey,
	})
	require.ErrorContains(t, err, "planner state repeated")
	require.ErrorContains(t, err, "first seen at step 1")

	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
}
