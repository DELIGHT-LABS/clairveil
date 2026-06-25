package keeper

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func TestTreeStateQueryReturnsZeroRootWhenEmpty(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	resp, err := k.TreeState(sdk.WrapSDKContext(ctx), &privacytypes.QueryTreeStateRequest{})
	require.NoError(t, err)
	require.Equal(t, canonicalZeroFieldHex(), resp.Root)
	require.Equal(t, uint64(0), resp.LeafCount)
	require.Equal(t, uint32(MerkleDepth), resp.Depth)
	require.False(t, resp.Initialized)
	require.Equal(t, MaxMerkleLeaves, resp.MaxLeaves)
	require.Equal(t, MaxMerkleLeaves, resp.RemainingLeaves)
}

func TestTreeStateQueryReturnsCurrentRootWhenInitialized(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	commitment := fixedFieldBytes(7)
	require.NoError(t, k.AppendCommitment(ctx, commitment))

	resp, err := k.TreeState(sdk.WrapSDKContext(ctx), &privacytypes.QueryTreeStateRequest{})
	require.NoError(t, err)
	require.NotEqual(t, canonicalZeroFieldHex(), resp.Root)
	require.Equal(t, uint64(1), resp.LeafCount)
	require.Equal(t, uint32(MerkleDepth), resp.Depth)
	require.True(t, resp.Initialized)
	require.Equal(t, MaxMerkleLeaves, resp.MaxLeaves)
	require.Equal(t, MaxMerkleLeaves-1, resp.RemainingLeaves)
}

func TestTreeStateQueryRejectsOverflowTreeState(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves+1)

	resp, err := k.TreeState(sdk.WrapSDKContext(ctx), &privacytypes.QueryTreeStateRequest{})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "exceeds max capacity")
}

func TestTreeStateQueryRejectsLargeMissingRootTreeState(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleRebuildLeaves+1)

	resp, err := k.TreeState(sdk.WrapSDKContext(ctx), &privacytypes.QueryTreeStateRequest{})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "cached root is required")
}

func TestMerklePathQueryRejectsOverflowTreeStateAsInternal(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves+1)

	resp, err := k.MerklePath(sdk.WrapSDKContext(ctx), &privacytypes.QueryMerklePathRequest{
		CommitmentHex: hex.EncodeToString(fixedFieldBytes(70)),
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "exceeds max capacity")
}

func TestMerklePathQueryRejectsMissingRequiredNodeAsInternal(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	first := fixedFieldBytes(73)
	second := fixedFieldBytes(74)
	require.NoError(t, k.AppendCommitment(ctx, first))
	require.NoError(t, k.AppendCommitment(ctx, second))
	deleteMerkleNode(k, ctx, 0, 1)

	resp, err := k.MerklePath(sdk.WrapSDKContext(ctx), &privacytypes.QueryMerklePathRequest{
		CommitmentHex: hex.EncodeToString(first),
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "merkle tree node is missing")
}

func TestTreeStateQueryRejectsMissingLeafDuringSmallRebuild(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	k.SetLeaf(ctx, 0, fixedFieldBytes(75))
	k.SetLeafCount(ctx, 2)

	resp, err := k.TreeState(sdk.WrapSDKContext(ctx), &privacytypes.QueryTreeStateRequest{})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "merkle tree leaf is missing")
}

func TestCommitmentInfoQueryRejectsOverflowTreeState(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves+1)

	resp, err := k.CommitmentInfo(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentInfoRequest{
		CommitmentHex: hex.EncodeToString(fixedFieldBytes(71)),
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "exceeds max capacity")
}

func TestCommitmentInfoQueryRejectsLargeMissingRootTreeState(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	k.SetLeafCount(ctx, MaxMerkleRebuildLeaves+1)

	resp, err := k.CommitmentInfo(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentInfoRequest{
		CommitmentHex: hex.EncodeToString(fixedFieldBytes(72)),
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), "cached root is required")
}

func TestCommitmentInfoQueryReturnsLeafIndex(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	first := fixedFieldBytes(11)
	second := fixedFieldBytes(12)
	require.NoError(t, k.AppendCommitment(ctx, first))
	require.NoError(t, k.AppendCommitment(ctx, second))

	resp, err := k.CommitmentInfo(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentInfoRequest{
		CommitmentHex: hex.EncodeToString(second),
	})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, uint64(1), resp.LeafIndex)
}

func TestCommitmentInfoQueryReturnsNotFoundForUnknownCommitment(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	resp, err := k.CommitmentInfo(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentInfoRequest{
		CommitmentHex: hex.EncodeToString(fixedFieldBytes(99)),
	})
	require.NoError(t, err)
	require.False(t, resp.Found)
	require.Equal(t, uint64(0), resp.LeafIndex)
}

func TestPrivacyEventsQueryReturnsIndexedEvents(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(12)

	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeDeposit, "aabb", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, "deadbeef"),
	}))

	ctx = ctx.WithBlockHeight(13)
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeShieldedTransfer, "ccdd", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText1, "c0ffee"),
	}))

	resp, err := k.PrivacyEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyEventsRequest{
		AfterHeight: 12,
		Page:        1,
		Limit:       10,
	})
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	require.Equal(t, uint64(1), resp.Page)
	require.Equal(t, uint64(10), resp.Limit)
	require.False(t, resp.HasMore)
	require.Equal(t, int64(13), resp.Events[0].Height)
	require.Equal(t, privacytypes.EventTypeShieldedTransfer, resp.Events[0].EventType)
	require.Equal(t, "CCDD", resp.Events[0].TxHashHex)
	require.Len(t, resp.Events[0].Attributes, 1)
	require.Equal(t, privacytypes.AttributeKeyCipherText1, resp.Events[0].Attributes[0].Key)
}

func TestPrivacyEventsQueryFiltersByType(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(21)

	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeDeposit, "aa", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, "01"),
	}))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeWithdraw, "bb", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier, "02"),
	}))

	resp, err := k.PrivacyEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryPrivacyEventsRequest{
		Page:       1,
		Limit:      10,
		EventTypes: []string{privacytypes.EventTypeWithdraw},
	})
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	require.Equal(t, privacytypes.EventTypeWithdraw, resp.Events[0].EventType)
}

func TestCheckNullifiersQueryReturnsBatchStatuses(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	usedNullifier := fixedFieldBytes(31)
	unusedNullifier := fixedFieldBytes(32)
	k.SetNullifier(ctx, usedNullifier)

	resp, err := k.CheckNullifiers(sdk.WrapSDKContext(ctx), &privacytypes.QueryCheckNullifiersRequest{
		Nullifiers: []string{
			hex.EncodeToString(usedNullifier),
			hex.EncodeToString(unusedNullifier),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Statuses, 2)
	require.Equal(t, hex.EncodeToString(usedNullifier), resp.Statuses[0].Nullifier)
	require.True(t, resp.Statuses[0].Used)
	require.Equal(t, hex.EncodeToString(unusedNullifier), resp.Statuses[1].Nullifier)
	require.False(t, resp.Statuses[1].Used)
}

func TestScanEventsQueryReturnsProjectionAndCursor(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	depositCommitment := fixedFieldBytes(41)
	transferCommitment1 := fixedFieldBytes(42)
	transferCommitment2 := fixedFieldBytes(43)

	ctx = ctx.WithBlockHeight(10)
	require.NoError(t, k.AppendCommitment(ctx, depositCommitment))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeDeposit, "aabb", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment, fmt.Sprintf("%x", depositCommitment)),
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, "deadbeef"),
	}))

	ctx = ctx.WithBlockHeight(11)
	require.NoError(t, k.AppendCommitment(ctx, transferCommitment1))
	require.NoError(t, k.AppendCommitment(ctx, transferCommitment2))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeShieldedTransfer, "ccdd", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier1, fmt.Sprintf("%x", fixedFieldBytes(44))),
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier2, fmt.Sprintf("%x", fixedFieldBytes(45))),
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment1, fmt.Sprintf("%x", transferCommitment1)),
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment2, fmt.Sprintf("%x", transferCommitment2)),
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText1, "c0ffee"),
		sdk.NewAttribute(privacytypes.AttributeKeyCipherText2, "decafbad"),
		sdk.NewAttribute(privacytypes.AttributeKeyViewTag1, "0102"),
		sdk.NewAttribute(privacytypes.AttributeKeyViewTag2, "0304"),
	}))

	firstResp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{
		Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, firstResp.Events, 1)
	require.True(t, firstResp.HasMore)
	require.Equal(t, uint64(1), firstResp.Limit)
	require.Equal(t, privacytypes.ScanFormatVersion, firstResp.ScanFormatVersion)
	require.Equal(t, privacytypes.ViewTagVersion, firstResp.ViewTagVersion)
	require.Equal(t, int64(10), firstResp.NextHeight)
	require.Equal(t, uint64(1), firstResp.NextSequence)
	require.Equal(t, privacytypes.EventTypeDeposit, firstResp.Events[0].EventType)
	require.Len(t, firstResp.Events[0].Outputs, 1)
	require.Equal(t, "deadbeef", firstResp.Events[0].Outputs[0].EncryptedNoteHex)
	require.True(t, firstResp.Events[0].Outputs[0].LeafIndexFound)
	require.Equal(t, uint64(0), firstResp.Events[0].Outputs[0].LeafIndex)

	secondResp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{
		AfterHeight:   firstResp.NextHeight,
		AfterSequence: firstResp.NextSequence,
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, secondResp.Events, 1)
	require.False(t, secondResp.HasMore)
	require.Equal(t, int64(11), secondResp.NextHeight)
	require.Equal(t, uint64(2), secondResp.NextSequence)
	event := secondResp.Events[0]
	require.Equal(t, privacytypes.EventTypeShieldedTransfer, event.EventType)
	require.Equal(t, []string{fmt.Sprintf("%x", fixedFieldBytes(44)), fmt.Sprintf("%x", fixedFieldBytes(45))}, event.NullifierHexes)
	require.Len(t, event.Outputs, 2)
	require.Equal(t, uint32(0), event.Outputs[0].OutputIndex)
	require.Equal(t, "c0ffee", event.Outputs[0].CipherTextHex)
	require.Equal(t, "0102", event.Outputs[0].ViewTagHex)
	require.True(t, event.Outputs[0].LeafIndexFound)
	require.Equal(t, uint64(1), event.Outputs[0].LeafIndex)
	require.Equal(t, uint32(1), event.Outputs[1].OutputIndex)
	require.Equal(t, "decafbad", event.Outputs[1].CipherTextHex)
	require.Equal(t, "0304", event.Outputs[1].ViewTagHex)
	require.True(t, event.Outputs[1].LeafIndexFound)
	require.Equal(t, uint64(2), event.Outputs[1].LeafIndex)
}

func TestScanEventsQueryDefaultsToDepositAndTransfer(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithBlockHeight(20)

	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeWithdraw, "aa", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier, fmt.Sprintf("%x", fixedFieldBytes(46))),
	}))

	resp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Events)

	blankFilterResp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{
		EventTypes: []string{"", " "},
	})
	require.NoError(t, err)
	require.Empty(t, blankFilterResp.Events)

	withdrawResp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{
		EventTypes: []string{privacytypes.EventTypeWithdraw},
	})
	require.NoError(t, err)
	require.Len(t, withdrawResp.Events, 1)
	require.Equal(t, privacytypes.EventTypeWithdraw, withdrawResp.Events[0].EventType)
	require.Equal(t, []string{fmt.Sprintf("%x", fixedFieldBytes(46))}, withdrawResp.Events[0].NullifierHexes)
}

func TestScanEventsQueryAdvancesCursorAcrossFilteredEvents(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	depositCommitment := fixedFieldBytes(47)

	ctx = ctx.WithBlockHeight(20)
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeWithdraw, "aa", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier, fmt.Sprintf("%x", fixedFieldBytes(48))),
	}))

	ctx = ctx.WithBlockHeight(21)
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeWithdraw, "bb", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyNullifier, fmt.Sprintf("%x", fixedFieldBytes(49))),
	}))

	ctx = ctx.WithBlockHeight(22)
	require.NoError(t, k.AppendCommitment(ctx, depositCommitment))
	require.NoError(t, k.indexPrivacyEvent(ctx, privacytypes.EventTypeDeposit, "cc", []sdk.Attribute{
		sdk.NewAttribute(privacytypes.AttributeKeyCommitment, fmt.Sprintf("%x", depositCommitment)),
		sdk.NewAttribute(privacytypes.AttributeKeyEncryptedNote, "deadbeef"),
	}))

	firstResp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{
		Limit: 2,
	})
	require.NoError(t, err)
	require.Empty(t, firstResp.Events)
	require.True(t, firstResp.HasMore)
	require.Equal(t, int64(21), firstResp.NextHeight)
	require.Equal(t, uint64(2), firstResp.NextSequence)

	secondResp, err := k.ScanEvents(sdk.WrapSDKContext(ctx), &privacytypes.QueryScanEventsRequest{
		AfterHeight:   firstResp.NextHeight,
		AfterSequence: firstResp.NextSequence,
		Limit:         2,
	})
	require.NoError(t, err)
	require.Len(t, secondResp.Events, 1)
	require.False(t, secondResp.HasMore)
	require.Equal(t, privacytypes.EventTypeDeposit, secondResp.Events[0].EventType)
	require.Equal(t, int64(22), secondResp.NextHeight)
	require.Equal(t, uint64(3), secondResp.NextSequence)
}

func TestDisclosureConfigQueryReturnsCurrentContract(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	resp, err := k.DisclosureConfig(sdk.WrapSDKContext(ctx), &privacytypes.QueryDisclosureConfigRequest{})
	require.NoError(t, err)
	require.Equal(t, privacytypes.DisclosurePayloadVersion, resp.PayloadVersion)
	require.True(t, resp.AuditDisclosureRequired)
	require.Equal(t, privacytypes.SupportedUserDisclosurePolicies(), resp.SupportedUserPolicies)
	require.Equal(t, privacytypes.SupportedUserDisclosureModes(), resp.SupportedUserModes)
}

func TestCircuitConfigQueryReturnsRuntimeManifest(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	dir := t.TempDir()
	t.Setenv(zk.ZKArtifactDirEnv, dir)

	manifest := zk.RuntimeArtifactManifest{
		SchemaVersion: zk.CircuitConfigSchemaVersion,
		GeneratedAt:   "2026-04-15T00:00:00Z",
		Curve:         zk.CircuitCurve,
		ActiveSetID:   zk.ActiveCircuitSetID,
		ArtifactDir:   dir,
		Artifacts: []zk.ArtifactDescriptor{
			{
				CircuitID:    "spend",
				ArtifactType: "r1cs",
				Filename:     zk.SpendR1CSFile,
				ChecksumEnv:  zk.SpendR1CSSHA256Env,
				SHA256:       "abcd",
			},
		},
	}
	bz, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, zk.ArtifactManifestFile), bz, 0o600))

	resp, err := k.CircuitConfig(sdk.WrapSDKContext(ctx), &privacytypes.QueryCircuitConfigRequest{})
	require.NoError(t, err)
	require.Equal(t, zk.CircuitConfigSchemaVersion, resp.SchemaVersion)
	require.Equal(t, zk.ActiveCircuitSetID, resp.ActiveSetId)
	require.Equal(t, zk.CircuitCurve, resp.Curve)
	require.Equal(t, zk.ArtifactManifestFile, resp.ManifestFile)
	require.True(t, resp.ManifestAvailable)
	require.Equal(t, zk.ChecksumSourceManifest, resp.ChecksumSource)
	require.Equal(t, "2026-04-15T00:00:00Z", resp.GeneratedAt)
	require.Len(t, resp.Artifacts, 1)
	require.Equal(t, "spend", resp.Artifacts[0].CircuitId)
	require.Equal(t, "abcd", resp.Artifacts[0].Sha256)
}

func TestReserveQueryReturnsAccountingSnapshot(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	coin := sdk.NewInt64Coin("uclair", 10)

	require.NoError(t, k.RecordReserveDeposit(ctx, coin))
	bankKeeper.moduleBalances = bankKeeper.moduleBalances.Add(coin)

	resp, err := k.Reserve(sdk.WrapSDKContext(ctx), &privacytypes.QueryReserveRequest{
		Denom: " uclair ",
	})
	require.NoError(t, err)
	require.Equal(t, "uclair", resp.Denom)
	require.Equal(t, "10", resp.ModuleBalance)
	require.Equal(t, "10", resp.TotalDeposited)
	require.Equal(t, "0", resp.TotalWithdrawn)
	require.Equal(t, "10", resp.ExpectedModuleBalance)
	require.True(t, resp.InvariantHolds)
}

func TestReserveAccountingAllowsZeroValueDummyDeposit(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	coin := sdk.NewInt64Coin("uclair", 0)

	require.NoError(t, k.RecordReserveDeposit(ctx, coin))

	resp, err := k.Reserve(sdk.WrapSDKContext(ctx), &privacytypes.QueryReserveRequest{
		Denom: "uclair",
	})
	require.NoError(t, err)
	require.Equal(t, "0", resp.ModuleBalance)
	require.Equal(t, "0", resp.TotalDeposited)
	require.Equal(t, "0", resp.ExpectedModuleBalance)
	require.True(t, resp.InvariantHolds)
}

func TestReserveQueryDetectsDirectTopUp(t *testing.T) {
	k, ctx, bankKeeper := setupMsgServerKeeper()
	expectedCoin := sdk.NewInt64Coin("uclair", 10)
	extraCoin := sdk.NewInt64Coin("uclair", 1)

	require.NoError(t, k.RecordReserveDeposit(ctx, expectedCoin))
	bankKeeper.moduleBalances = bankKeeper.moduleBalances.Add(expectedCoin, extraCoin)

	resp, err := k.Reserve(sdk.WrapSDKContext(ctx), &privacytypes.QueryReserveRequest{
		Denom: "uclair",
	})
	require.NoError(t, err)
	require.Equal(t, "11", resp.ModuleBalance)
	require.Equal(t, "10", resp.ExpectedModuleBalance)
	require.False(t, resp.InvariantHolds)
}

func TestReserveQueryRejectsInvalidDenom(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	resp, err := k.Reserve(sdk.WrapSDKContext(ctx), &privacytypes.QueryReserveRequest{
		Denom: "bad denom",
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryMethodsRejectNilRequests(t *testing.T) {
	k, _, _ := setupMsgServerKeeper()

	treeResp, treeErr := k.TreeState(context.Background(), nil)
	require.Nil(t, treeResp)
	require.Error(t, treeErr)

	commitResp, commitErr := k.CommitmentInfo(context.Background(), nil)
	require.Nil(t, commitResp)
	require.Error(t, commitErr)

	disclosureResp, disclosureErr := k.DisclosureConfig(context.Background(), nil)
	require.Nil(t, disclosureResp)
	require.Error(t, disclosureErr)

	eventsResp, eventsErr := k.PrivacyEvents(context.Background(), nil)
	require.Nil(t, eventsResp)
	require.Error(t, eventsErr)

	scanEventsResp, scanEventsErr := k.ScanEvents(context.Background(), nil)
	require.Nil(t, scanEventsResp)
	require.Error(t, scanEventsErr)

	nullifiersResp, nullifiersErr := k.CheckNullifiers(context.Background(), nil)
	require.Nil(t, nullifiersResp)
	require.Error(t, nullifiersErr)

	circuitResp, circuitErr := k.CircuitConfig(context.Background(), nil)
	require.Nil(t, circuitResp)
	require.Error(t, circuitErr)

	reserveResp, reserveErr := k.Reserve(context.Background(), nil)
	require.Nil(t, reserveResp)
	require.Error(t, reserveErr)
}
