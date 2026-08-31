package scan

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestSyncNotesFindsNotesAndMarksSpent(t *testing.T) {
	rootSeed := []byte("scan-service-root-seed")
	txNote, txRes := newScanServiceDepositTx(t, rootSeed, big.NewInt(7), "uclair", 11)
	existingFound := BuildFoundNote(txNote, txRes)
	txSource := stubPrivacyTxSource{
		latestBlockHeight: 11,
		searchResults: map[int][]*cmttypes.ResultTx{
			1: {txRes},
		},
	}
	nullifierChecker := &stubNullifierUsageChecker{
		used: map[string]bool{
			existingFound.Nullifier: true,
		},
	}
	observer := &stubSyncObserver{}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		observer,
		SyncInput{
			UserAddress: "clair1scanservice",
			RootSeed:    rootSeed,
			Wallet: &LocalWalletData{
				LastHeight: 3,
				Notes: []FoundNote{
					existingFound,
				},
			},
		},
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.WalletChanged)
	require.Equal(t, int64(11), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
	require.Len(t, result.Notes, 1)
	require.Equal(t, "7", result.Notes[0].Note.Amount.String())
	require.True(t, result.Notes[0].IsSpent)
	require.True(t, result.Wallet.Notes[0].IsSpent)
	require.Equal(t, int64(3), result.Diagnostics.LoadedLastHeight)
	require.Equal(t, 1, result.Diagnostics.LoadedNoteCount)
	require.Equal(t, int64(4), result.Diagnostics.ScannedFromHeight)
	require.Equal(t, int64(11), result.Diagnostics.ScannedToHeight)
	require.False(t, result.Diagnostics.NormalizedCache)
	require.Equal(t, 0, result.Diagnostics.NewNotesFound)
	require.Equal(t, 1, result.Diagnostics.FinalNoteCount)
	require.Equal(t, [][2]int64{{4, 11}}, observer.syncRanges)
	require.Empty(t, observer.notesFound)
}

func TestCopyNullifierVerificationDowngradesUnmatchedCachedNotes(t *testing.T) {
	cached := &LocalWalletData{Notes: []FoundNote{
		{Nullifier: "aa", VerifiedUnspent: true},
		{Nullifier: "bb", VerifiedUnspent: true},
	}}
	refreshed := []FoundNote{{Nullifier: "aa", VerifiedUnspent: true}}

	changed := copyNullifierVerificationToCachedNotes(cached, refreshed)

	require.True(t, changed)
	require.True(t, cached.Notes[0].VerifiedUnspent)
	require.False(t, cached.Notes[1].VerifiedUnspent)
}

func TestSyncNotesDoesNotMarkWalletChangedWhenOnlySpentStatusChanges(t *testing.T) {
	rootSeed := []byte("scan-service-spent-only-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	wallet := &LocalWalletData{
		LastHeight:   7,
		LastSequence: ^uint64(0),
		Notes:        []FoundNote{found},
	}
	checker := &stubNullifierUsageChecker{
		used: map[string]bool{found.Nullifier: true},
	}

	result, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{
			UserAddress: "clair1spentstatus",
			RootSeed:    rootSeed,
			Wallet:      wallet,
		},
	)

	require.NoError(t, err)
	require.False(t, result.WalletChanged)
	require.True(t, result.Wallet.Notes[0].IsSpent)
	require.True(t, wallet.Notes[0].IsSpent)
}

func TestSyncNotesDoesNotMarkWalletChangedWhenNullifierStatusIsUnchanged(t *testing.T) {
	rootSeed := []byte("scan-service-unchanged-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	found.VerifiedUnspent = true
	wallet := &LocalWalletData{
		LastHeight:   7,
		LastSequence: ^uint64(0),
		Notes:        []FoundNote{found},
	}
	checker := &stubNullifierUsageChecker{
		used: map[string]bool{found.Nullifier: false},
	}

	result, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{
			UserAddress: "clair1unchangedstatus",
			RootSeed:    rootSeed,
			Wallet:      wallet,
		},
	)

	require.NoError(t, err)
	require.False(t, result.WalletChanged)
	require.True(t, result.Wallet.Notes[0].VerifiedUnspent)
}

func TestSyncNotesFailsWhenNullifierStatusCannotBeVerified(t *testing.T) {
	rootSeed := []byte("scan-service-unverified-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	found.VerifiedUnspent = true
	wallet := &LocalWalletData{LastHeight: 7, LastSequence: 9, Notes: []FoundNote{found}}
	checkerCause := errors.New(wallet.Notes[0].Nullifier + ": query unavailable")
	checker := &stubNullifierUsageChecker{err: checkerCause}
	result, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{
			UserAddress: "clair1scanunverified",
			RootSeed:    rootSeed,
			Wallet:      wallet,
		},
	)
	require.ErrorContains(t, err, "failed to verify note nullifier status")
	require.NotContains(t, err.Error(), wallet.Notes[0].Nullifier)
	require.ErrorIs(t, err, ErrNullifierStatusUnavailable)
	require.NotNil(t, result)
	require.True(t, result.WalletChanged)
	require.False(t, result.Wallet.Notes[0].VerifiedUnspent)
	require.NotErrorIs(t, err, checkerCause)
	require.Equal(t, int64(7), wallet.LastHeight)
	require.Equal(t, uint64(9), wallet.LastSequence)
	require.False(t, wallet.Notes[0].IsSpent)
	require.False(t, wallet.Notes[0].VerifiedUnspent)
}

func TestSyncNotesReturnsNewNotesAsUnverifiedWhenNullifierLookupFails(t *testing.T) {
	rootSeed := []byte("scan-service-new-partial-note")
	_, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(9), "uclair", 12, 1)
	source := &stubPrivacyScanEventSource{
		latestBlockHeight: 12,
		responses: []*privacytypes.QueryScanEventsResponse{{
			Events:            []*privacytypes.QueryScanEvent{scanEvent},
			NextHeight:        12,
			NextSequence:      1,
			Limit:             1,
			HasMore:           false,
			ScanFormatVersion: privacytypes.ScanFormatVersion,
			ViewTagVersion:    privacytypes.ViewTagVersion,
		}},
	}
	wallet := &LocalWalletData{}

	result, err := SyncNotes(
		context.Background(),
		source,
		&stubNullifierUsageChecker{err: errors.New("query unavailable")},
		nil,
		SyncInput{UserAddress: "clair1partialnotes", RootSeed: rootSeed, PageLimit: 1, Wallet: wallet},
	)

	require.ErrorContains(t, err, "failed to verify note nullifier status")
	require.NotNil(t, result)
	require.Len(t, result.Notes, 1)
	require.False(t, result.Notes[0].IsSpent)
	require.False(t, result.Notes[0].VerifiedUnspent)
	require.Equal(t, 1, result.Diagnostics.NewNotesFound)
	require.Equal(t, 1, result.Diagnostics.FinalNoteCount)
	require.Empty(t, wallet.Notes)
	require.Zero(t, wallet.LastHeight)
}

func TestSyncNotesPreservesRedactedContextCancellation(t *testing.T) {
	checkerCause := fmt.Errorf("private upstream detail: %w", context.Canceled)
	rootSeed := []byte("scan-service-canceled-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	found.VerifiedUnspent = true
	wallet := &LocalWalletData{LastHeight: 7, Notes: []FoundNote{found}}
	_, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		&stubNullifierUsageChecker{err: checkerCause},
		nil,
		SyncInput{
			UserAddress: "clair1canceledscan",
			RootSeed:    rootSeed,
			Wallet:      wallet,
		},
	)

	require.ErrorIs(t, err, ErrNullifierStatusUnavailable)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, checkerCause)
	require.NotContains(t, err.Error(), "private upstream detail")
	require.NotContains(t, err.Error(), wallet.Notes[0].Nullifier)
}

func TestSyncNotesResetsWalletWhenCachedHeightRollsBack(t *testing.T) {
	rootSeed := []byte("scan-service-rollback-seed")
	txSource := stubPrivacyTxSource{latestBlockHeight: 5}
	checker := &stubNullifierUsageChecker{}
	observer := &stubSyncObserver{}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		checker,
		observer,
		SyncInput{
			UserAddress: "clair1scanrollback",
			RootSeed:    rootSeed,
			Wallet: &LocalWalletData{
				LastHeight:   8,
				LastSequence: 3,
				Notes: []FoundNote{
					{Nullifier: "aa"},
				},
			},
		},
	)

	require.NoError(t, err)
	require.Equal(t, int64(5), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
	require.Empty(t, result.Notes)
	require.True(t, result.Diagnostics.RollbackReset)
	require.Equal(t, [][2]int64{{8, 5}}, observer.rollbackResets)
}

func TestSyncNotesUsesScanEventsCursorAndBatchNullifiers(t *testing.T) {
	rootSeed := []byte("scan-service-cursor-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(9), "uclair", 12, 10)
	foundNote := BuildFoundNoteFromScanEvent(note, scanEvent)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 20,
		responses: []*privacytypes.QueryScanEventsResponse{
			{
				Events:            []*privacytypes.QueryScanEvent{scanEvent},
				NextHeight:        12,
				NextSequence:      10,
				Limit:             1,
				HasMore:           false,
				ScanFormatVersion: privacytypes.ScanFormatVersion,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
		},
	}
	nullifierChecker := &stubBatchNullifierUsageChecker{
		batchUsed: map[string]bool{
			foundNote.Nullifier: true,
		},
	}
	observer := &stubSyncObserver{}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		observer,
		SyncInput{
			UserAddress: "clair1scancursor",
			RootSeed:    rootSeed,
			PageLimit:   1,
			Wallet: &LocalWalletData{
				LastHeight:   3,
				LastSequence: 7,
				Notes:        []FoundNote{},
			},
		},
	)

	require.NoError(t, err)
	require.Len(t, txSource.scanRequests, 1)
	require.Equal(t, scanRequest{afterHeight: 3, afterSequence: 7, limit: 1}, txSource.scanRequests[0])
	require.Equal(t, int64(20), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
	require.Len(t, result.Notes, 1)
	require.Equal(t, "9", result.Notes[0].Note.Amount.String())
	require.True(t, result.Notes[0].IsSpent)
	require.Equal(t, 1, result.Diagnostics.NewNotesFound)
	require.Equal(t, []noteFoundEvent{{txHash: "CCDD", count: 1}}, observer.notesFound)
	require.Equal(t, [][]string{{foundNote.Nullifier}}, nullifierChecker.batchRequests)
	require.Empty(t, nullifierChecker.singleRequests)
}

func TestSyncNotesContinuesAcrossEmptyScanEventPages(t *testing.T) {
	rootSeed := []byte("scan-service-empty-page-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(10), "uclair", 14, 4)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 20,
		responses: []*privacytypes.QueryScanEventsResponse{
			{
				Events:            nil,
				NextHeight:        13,
				NextSequence:      3,
				Limit:             2,
				HasMore:           true,
				ScanFormatVersion: privacytypes.ScanFormatVersion,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
			{
				Events:            []*privacytypes.QueryScanEvent{scanEvent},
				NextHeight:        14,
				NextSequence:      4,
				Limit:             2,
				HasMore:           false,
				ScanFormatVersion: privacytypes.ScanFormatVersion,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
		},
	}
	nullifierChecker := &stubBatchNullifierUsageChecker{}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		nil,
		SyncInput{
			UserAddress: "clair1scanemptypage",
			RootSeed:    rootSeed,
			PageLimit:   2,
			Wallet: &LocalWalletData{
				LastHeight:   3,
				LastSequence: 0,
				Notes:        []FoundNote{},
			},
		},
	)

	require.NoError(t, err)
	require.Equal(t, []scanRequest{
		{afterHeight: 3, afterSequence: 0, limit: 2},
		{afterHeight: 13, afterSequence: 3, limit: 2},
	}, txSource.scanRequests)
	require.Len(t, result.Notes, 1)
	require.Equal(t, note.Amount.String(), result.Notes[0].Note.Amount.String())
	require.Equal(t, int64(20), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
}

func TestSyncNotesForceRescanIgnoresMismatchedViewTag(t *testing.T) {
	rootSeed := []byte("scan-service-force-rescan-view-tag-seed")
	note, scanEvent := newScanServiceTransferScanEventWithMismatchedViewTag(t, rootSeed, big.NewInt(12), "uclair", 15, 6)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 20,
		responses: []*privacytypes.QueryScanEventsResponse{
			{
				Events:            []*privacytypes.QueryScanEvent{scanEvent},
				NextHeight:        15,
				NextSequence:      6,
				Limit:             1,
				HasMore:           false,
				ScanFormatVersion: privacytypes.ScanFormatVersion,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
		},
	}
	nullifierChecker := &stubBatchNullifierUsageChecker{}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		nil,
		SyncInput{
			UserAddress: "clair1scanforceviewtag",
			RootSeed:    rootSeed,
			ForceRescan: true,
			PageLimit:   1,
			Wallet: &LocalWalletData{
				LastHeight:   20,
				LastSequence: ^uint64(0),
				Notes:        []FoundNote{},
			},
		},
	)

	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Equal(t, note.Amount.String(), result.Notes[0].Note.Amount.String())
	require.True(t, result.Diagnostics.ForcedRescan)
	require.Equal(t, int64(20), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
}

func TestSyncNotesFallsBackToTxSearchWhenScanEventsUnavailable(t *testing.T) {
	rootSeed := []byte("scan-service-unavailable-seed")
	txNote, txRes := newScanServiceDepositTx(t, rootSeed, big.NewInt(15), "uclair", 18)
	foundNote := BuildFoundNote(txNote, txRes)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 18,
		scanErr:           status.Error(codes.Unimplemented, "method ScanEvents not implemented"),
		searchResults: map[int][]*cmttypes.ResultTx{
			1: {txRes},
		},
	}
	nullifierChecker := &stubBatchNullifierUsageChecker{
		batchUsed: map[string]bool{
			foundNote.Nullifier: false,
		},
	}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		nil,
		SyncInput{
			UserAddress: "clair1scanfallback",
			RootSeed:    rootSeed,
			Wallet: &LocalWalletData{
				LastHeight:   3,
				LastSequence: 5,
				Notes:        []FoundNote{},
			},
		},
	)

	require.NoError(t, err)
	require.Len(t, txSource.scanRequests, 1)
	require.Equal(t, []int{1}, txSource.searchPages)
	require.Equal(t, int64(18), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
	require.Len(t, result.Notes, 1)
	require.Equal(t, "15", result.Notes[0].Note.Amount.String())
	require.False(t, result.Notes[0].IsSpent)
	require.True(t, result.Notes[0].VerifiedUnspent)
}

func TestSyncNotesLegacyFallbackRewindsSequenceCursorAndDeduplicatesBoundaryHeight(t *testing.T) {
	rootSeed := []byte("scan-service-sequence-fallback-seed")
	firstNote, firstTx := newScanServiceDepositTx(t, rootSeed, big.NewInt(15), "uclair", 100)
	secondNote, secondTx := newScanServiceDepositTx(t, rootSeed, big.NewInt(16), "uclair", 100)
	firstFound := BuildFoundNote(firstNote, firstTx)
	secondFound := BuildFoundNote(secondNote, secondTx)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 101,
		scanErr:           status.Error(codes.Unimplemented, "method ScanEvents not implemented"),
		searchResults: map[int][]*cmttypes.ResultTx{
			1: {firstTx, secondTx},
		},
	}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		&stubBatchNullifierUsageChecker{batchUsed: map[string]bool{
			firstFound.Nullifier:  false,
			secondFound.Nullifier: false,
		}},
		nil,
		SyncInput{
			UserAddress: "clair1scanfallbacksequence",
			RootSeed:    rootSeed,
			Wallet: &LocalWalletData{
				LastHeight:   100,
				LastSequence: 5,
				Notes:        []FoundNote{firstFound},
			},
		},
	)

	require.NoError(t, err)
	require.Equal(t, []txSearchRequest{{afterHeight: 99, page: 1}}, txSource.searchRequests)
	require.Len(t, result.Notes, 2)
	require.True(t, result.Notes[0].VerifiedUnspent)
	require.True(t, result.Notes[1].VerifiedUnspent)
}

func TestSyncNotesFallsBackToTxSearchWhenScanEventVersionUnsupported(t *testing.T) {
	rootSeed := []byte("scan-service-version-fallback-seed")
	txNote, txRes := newScanServiceDepositTx(t, rootSeed, big.NewInt(16), "uclair", 19)
	foundNote := BuildFoundNote(txNote, txRes)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 19,
		responses: []*privacytypes.QueryScanEventsResponse{
			{
				ScanFormatVersion: privacytypes.ScanFormatVersion + 1,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
		},
		searchResults: map[int][]*cmttypes.ResultTx{
			1: {txRes},
		},
	}
	nullifierChecker := &stubBatchNullifierUsageChecker{
		batchUsed: map[string]bool{
			foundNote.Nullifier: false,
		},
	}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		nil,
		SyncInput{
			UserAddress: "clair1scanversionfallback",
			RootSeed:    rootSeed,
			Wallet: &LocalWalletData{
				LastHeight:   3,
				LastSequence: 5,
				Notes:        []FoundNote{},
			},
		},
	)

	require.NoError(t, err)
	require.Len(t, txSource.scanRequests, 1)
	require.Equal(t, []int{1}, txSource.searchPages)
	require.Equal(t, int64(19), result.Wallet.LastHeight)
	require.Equal(t, ^uint64(0), result.Wallet.LastSequence)
	require.Len(t, result.Notes, 1)
	require.Equal(t, txNote.Amount.String(), result.Notes[0].Note.Amount.String())
}

func TestSyncNotesFallsBackWhenBatchNullifierResponseIsIncomplete(t *testing.T) {
	rootSeed := []byte("scan-service-incomplete-batch-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(13), "uclair", 12, 10)
	foundNote := BuildFoundNoteFromScanEvent(note, scanEvent)
	txSource := &stubPrivacyScanEventSource{
		latestBlockHeight: 12,
		responses: []*privacytypes.QueryScanEventsResponse{
			{
				Events:            []*privacytypes.QueryScanEvent{scanEvent},
				NextHeight:        12,
				NextSequence:      10,
				HasMore:           false,
				ScanFormatVersion: privacytypes.ScanFormatVersion,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
		},
	}
	nullifierChecker := &stubBatchNullifierUsageChecker{
		stubNullifierUsageChecker: stubNullifierUsageChecker{
			used: map[string]bool{
				foundNote.Nullifier: true,
			},
		},
		batchUsed: map[string]bool{},
	}

	result, err := SyncNotes(
		context.Background(),
		txSource,
		nullifierChecker,
		nil,
		SyncInput{
			UserAddress: "clair1scanbatchfallback",
			RootSeed:    rootSeed,
			Wallet:      &LocalWalletData{},
		},
	)

	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.True(t, result.Notes[0].IsSpent)
	require.Equal(t, [][]string{{foundNote.Nullifier}}, nullifierChecker.batchRequests)
	require.Equal(t, []string{foundNote.Nullifier}, nullifierChecker.singleRequests)
}

func TestSyncNotesUsesCanonicalKeysWhenApplyingBatchNullifierStatuses(t *testing.T) {
	rootSeed := []byte("scan-service-canonical-nullifier-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	canonicalNullifier := found.Nullifier
	checker := &stubBatchNullifierUsageChecker{
		batchUsed: map[string]bool{canonicalNullifier: true},
	}
	wallet := &LocalWalletData{
		LastHeight:   7,
		LastSequence: ^uint64(0),
		Notes:        []FoundNote{found},
	}

	result, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{
			UserAddress: "clair1canonicalnullifier",
			RootSeed:    rootSeed,
			Wallet:      wallet,
		},
	)

	require.NoError(t, err)
	require.Equal(t, [][]string{{canonicalNullifier}}, checker.batchRequests)
	require.Empty(t, checker.singleRequests)
	require.True(t, result.Wallet.Notes[0].IsSpent)
}

func TestSyncNotesDeduplicatesCanonicalEquivalentCachedNullifiers(t *testing.T) {
	rootSeed := []byte("scan-service-equivalent-nullifier-seed")
	note, scanEvent := newScanServiceDepositScanEventWithLeadingZeroNullifier(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	require.True(t, strings.HasPrefix(found.Nullifier, "00"))
	short := found
	short.Nullifier = found.Nullifier[2:]
	checker := &stubBatchNullifierUsageChecker{
		batchUsed: map[string]bool{found.Nullifier: false},
	}
	wallet := &LocalWalletData{
		LastHeight:   7,
		LastSequence: ^uint64(0),
		Notes:        []FoundNote{short, found},
	}

	result, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{UserAddress: "clair1equivalentnullifier", RootSeed: rootSeed, Wallet: wallet},
	)

	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Equal(t, found.Nullifier, result.Notes[0].Nullifier)
	require.True(t, result.Notes[0].VerifiedUnspent)
	require.Equal(t, [][]string{{found.Nullifier}}, checker.batchRequests)
}

func TestSyncNotesFallsBackWhenBatchResponseConflictsAfterCanonicalization(t *testing.T) {
	rootSeed := []byte("scan-service-conflicting-canonical-status-seed")
	note, scanEvent := newScanServiceDepositScanEventWithLeadingZeroNullifier(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	require.True(t, strings.HasPrefix(found.Nullifier, "00"))
	short := found.Nullifier[2:]
	checker := &stubBatchNullifierUsageChecker{
		stubNullifierUsageChecker: stubNullifierUsageChecker{
			used: map[string]bool{found.Nullifier: true},
		},
		batchUsed: map[string]bool{
			found.Nullifier: true,
			short:           false,
		},
	}
	wallet := &LocalWalletData{
		LastHeight:   7,
		LastSequence: ^uint64(0),
		Notes:        []FoundNote{found},
	}

	result, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{UserAddress: "clair1canonicalconflict", RootSeed: rootSeed, Wallet: wallet},
	)

	require.NoError(t, err)
	require.Equal(t, []string{found.Nullifier}, checker.singleRequests)
	require.True(t, result.Notes[0].IsSpent)
}

func newScanServiceDepositScanEventWithLeadingZeroNullifier(t *testing.T, rootSeed []byte, amount *big.Int, denom string, height int64, sequence uint64) (*privacytypes.Note, *privacytypes.QueryScanEvent) {
	t.Helper()
	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)
	note := &privacytypes.Note{
		ReceiverSpendPubKeyX: pointBigInt(&spendPubKey.X),
		ReceiverSpendPubKeyY: pointBigInt(&spendPubKey.Y),
		ReceiverViewPubKeyX:  pointBigInt(&viewPubKey.X),
		ReceiverViewPubKeyY:  pointBigInt(&viewPubKey.Y),
		Amount:               new(big.Int).Set(amount),
		AssetID:              privacycrypto.HashString(denom),
		Memo:                 "scan-service-canonical-conflict",
	}
	for randomness := int64(1); ; randomness++ {
		note.Randomness = big.NewInt(randomness)
		nullifierHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
		require.NoError(t, err)
		if strings.HasPrefix(nullifierHex, "00") {
			break
		}
	}
	noteBytes, err := privacytypes.MarshalNotePlaintextV1(note)
	require.NoError(t, err)
	cipherBytes, err := privacycrypto.Encrypt(noteBytes, rootSeed)
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	return note, &privacytypes.QueryScanEvent{
		Sequence:  sequence,
		Height:    height,
		TxHashHex: "CCDD",
		EventType: privacytypes.EventTypeDeposit,
		Outputs: []*privacytypes.QueryScanOutput{{
			OutputIndex:      0,
			CommitmentHex:    commitmentHex,
			EncryptedNoteHex: hex.EncodeToString(cipherBytes),
		}},
	}
}

func TestSyncNotesRejectsCachedNullifierThatDoesNotMatchNote(t *testing.T) {
	rootSeed := []byte("scan-service-mismatched-nullifier-seed")
	note, scanEvent := newScanServiceDepositScanEvent(t, rootSeed, big.NewInt(1), "uclair", 7, 1)
	found := BuildFoundNoteFromScanEvent(note, scanEvent)
	found.Nullifier = strings.Repeat("00", 32)
	checker := &stubBatchNullifierUsageChecker{batchUsed: map[string]bool{}}

	_, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{
			UserAddress: "clair1mismatchednullifier",
			RootSeed:    rootSeed,
			Wallet: &LocalWalletData{
				LastHeight:   7,
				LastSequence: ^uint64(0),
				Notes:        []FoundNote{found},
			},
		},
	)

	require.ErrorIs(t, err, ErrInvalidWalletCache)
	require.NotErrorIs(t, err, ErrNullifierStatusUnavailable)
	require.ErrorContains(t, err, "force a rescan")
	require.NotContains(t, err.Error(), found.Nullifier)
	require.Empty(t, checker.batchRequests)
}

func TestSyncNotesRequiresUserAddress(t *testing.T) {
	_, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{},
		&stubNullifierUsageChecker{},
		nil,
		SyncInput{RootSeed: []byte("seed")},
	)

	require.ErrorContains(t, err, "a transparent --from account is required to scan shielded notes")
}

type stubPrivacyTxSource struct {
	latestBlockHeight int64
	searchResults     map[int][]*cmttypes.ResultTx
}

func (s stubPrivacyTxSource) LatestBlockHeight(context.Context) (int64, error) {
	return s.latestBlockHeight, nil
}

func (s stubPrivacyTxSource) SearchPrivacyTxs(_ context.Context, _ int64, page, _ int) ([]*cmttypes.ResultTx, error) {
	if s.searchResults == nil {
		return nil, nil
	}

	return s.searchResults[page], nil
}

type scanRequest struct {
	afterHeight   int64
	afterSequence uint64
	limit         int
}

type txSearchRequest struct {
	afterHeight int64
	page        int
}

type stubPrivacyScanEventSource struct {
	latestBlockHeight int64
	responses         []*privacytypes.QueryScanEventsResponse
	scanErr           error
	scanRequests      []scanRequest
	searchResults     map[int][]*cmttypes.ResultTx
	searchPages       []int
	searchRequests    []txSearchRequest
}

func (s *stubPrivacyScanEventSource) LatestBlockHeight(context.Context) (int64, error) {
	return s.latestBlockHeight, nil
}

func (s *stubPrivacyScanEventSource) SearchPrivacyTxs(_ context.Context, afterHeight int64, page, _ int) ([]*cmttypes.ResultTx, error) {
	s.searchPages = append(s.searchPages, page)
	s.searchRequests = append(s.searchRequests, txSearchRequest{afterHeight: afterHeight, page: page})
	if s.searchResults == nil {
		return nil, nil
	}
	return s.searchResults[page], nil
}

func (s *stubPrivacyScanEventSource) ScanPrivacyEvents(_ context.Context, afterHeight int64, afterSequence uint64, limit int) (*privacytypes.QueryScanEventsResponse, error) {
	s.scanRequests = append(s.scanRequests, scanRequest{afterHeight: afterHeight, afterSequence: afterSequence, limit: limit})
	if s.scanErr != nil {
		return nil, s.scanErr
	}
	if len(s.responses) == 0 {
		return &privacytypes.QueryScanEventsResponse{}, nil
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

type stubNullifierUsageChecker struct {
	used           map[string]bool
	singleRequests []string
	err            error
}

func (s *stubNullifierUsageChecker) CheckNullifierUsed(_ context.Context, nullifierHex string) (bool, error) {
	s.singleRequests = append(s.singleRequests, nullifierHex)
	if s.err != nil {
		return false, s.err
	}
	if s.used == nil {
		return false, nil
	}

	return s.used[nullifierHex], nil
}

type stubBatchNullifierUsageChecker struct {
	stubNullifierUsageChecker
	batchUsed     map[string]bool
	batchRequests [][]string
}

func TestSyncNotesRejectsInvalidNullifierBeforeBatchStatusLookup(t *testing.T) {
	checker := &stubBatchNullifierUsageChecker{}
	_, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{latestBlockHeight: 7},
		checker,
		nil,
		SyncInput{
			UserAddress: "clair1invalidnullifier",
			RootSeed:    []byte("scan-service-invalid-nullifier-seed"),
			Wallet: &LocalWalletData{LastHeight: 7, LastSequence: ^uint64(0), Notes: []FoundNote{{
				Nullifier: "",
				Note:      privacytypes.Note{Amount: big.NewInt(1)},
			}}},
		},
	)
	require.ErrorIs(t, err, ErrInvalidWalletCache)
	require.NotErrorIs(t, err, ErrNullifierStatusUnavailable)
	require.ErrorContains(t, err, "force a rescan")
	require.Empty(t, checker.batchRequests)
}

func (s *stubBatchNullifierUsageChecker) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	s.batchRequests = append(s.batchRequests, append([]string(nil), nullifierHexes...))
	return s.batchUsed, nil
}

type noteFoundEvent struct {
	txHash string
	count  int
}

type stubSyncObserver struct {
	syncRanges      [][2]int64
	rollbackResets  [][2]int64
	notesFound      []noteFoundEvent
	forcedRescanHit bool
}

func (s *stubSyncObserver) OnForcedRescan() {
	s.forcedRescanHit = true
}

func (s *stubSyncObserver) OnRollbackReset(cachedHeight, currentHeight int64) {
	s.rollbackResets = append(s.rollbackResets, [2]int64{cachedHeight, currentHeight})
}

func (s *stubSyncObserver) OnSyncRange(fromHeight, toHeight int64) {
	s.syncRanges = append(s.syncRanges, [2]int64{fromHeight, toHeight})
}

func (s *stubSyncObserver) OnNotesFound(txHash string, count int) {
	s.notesFound = append(s.notesFound, noteFoundEvent{txHash: txHash, count: count})
}

func newScanServiceDepositTx(t *testing.T, rootSeed []byte, amount *big.Int, denom string, height int64) (*privacytypes.Note, *cmttypes.ResultTx) {
	t.Helper()

	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		amount,
		denom,
		"scan-service",
	)
	require.NoError(t, err)

	cipherBytes, err := privacycrypto.Encrypt(mustNoteBytes(t, note), rootSeed)
	require.NoError(t, err)
	cipherBytes = wrapDepositNoteCipherText(t, cipherBytes)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)

	return note, &cmttypes.ResultTx{
		Hash:   []byte{0xAA, 0xBB},
		Height: height,
		TxResult: abci.ExecTxResult{
			Events: []abci.Event{
				{
					Type: "deposit",
					Attributes: []abci.EventAttribute{
						{
							Key:   privacytypes.AttributeKeyCommitment,
							Value: commitmentHex,
						},
						{
							Key:   "encrypted_note",
							Value: hex.EncodeToString(cipherBytes),
						},
					},
				},
			},
		},
	}
}

func newScanServiceDepositScanEvent(t *testing.T, rootSeed []byte, amount *big.Int, denom string, height int64, sequence uint64) (*privacytypes.Note, *privacytypes.QueryScanEvent) {
	t.Helper()

	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		amount,
		denom,
		"scan-service",
	)
	require.NoError(t, err)

	cipherBytes, err := privacycrypto.Encrypt(mustNoteBytes(t, note), rootSeed)
	require.NoError(t, err)
	cipherBytes = wrapDepositNoteCipherText(t, cipherBytes)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)

	return note, &privacytypes.QueryScanEvent{
		Sequence:  sequence,
		Height:    height,
		TxHashHex: "CCDD",
		EventType: privacytypes.EventTypeDeposit,
		Outputs: []*privacytypes.QueryScanOutput{
			{
				OutputIndex:      0,
				CommitmentHex:    commitmentHex,
				EncryptedNoteHex: hex.EncodeToString(cipherBytes),
			},
		},
	}
}

func newScanServiceTransferScanEventWithMismatchedViewTag(t *testing.T, rootSeed []byte, amount *big.Int, denom string, height int64, sequence uint64) (*privacytypes.Note, *privacytypes.QueryScanEvent) {
	t.Helper()

	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		amount,
		denom,
		"scan-service-transfer",
	)
	require.NoError(t, err)

	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	cipherText, viewTag, err := privacycrypto.AsymEncryptWithViewTag(mustNoteBytes(t, note), *viewPubKey, commitmentBytes, 0)
	require.NoError(t, err)
	cipherText = wrapTransferNoteCipherText(t, cipherText)
	viewTag[0] ^= 0xff

	return note, &privacytypes.QueryScanEvent{
		Sequence:  sequence,
		Height:    height,
		TxHashHex: "DDEE",
		EventType: privacytypes.EventTypeShieldedTransfer,
		Outputs: []*privacytypes.QueryScanOutput{
			{
				OutputIndex:    0,
				CommitmentHex:  hex.EncodeToString(commitmentBytes),
				CipherTextHex:  hex.EncodeToString(cipherText),
				ViewTagHex:     hex.EncodeToString(viewTag),
				LeafIndexFound: true,
			},
		},
	}
}
