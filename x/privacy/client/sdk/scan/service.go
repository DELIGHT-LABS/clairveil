package scan

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type PrivacyTxSource interface {
	LatestBlockHeight(ctx context.Context) (int64, error)
	SearchPrivacyTxs(ctx context.Context, afterHeight int64, page, limit int) ([]*cmttypes.ResultTx, error)
}

type PrivacyScanEventSource interface {
	ScanPrivacyEvents(ctx context.Context, afterHeight int64, afterSequence uint64, limit int) (*privacytypes.QueryScanEventsResponse, error)
}

type NullifierUsageChecker interface {
	CheckNullifierUsed(ctx context.Context, nullifierHex string) (bool, error)
}

type BatchNullifierUsageChecker interface {
	CheckNullifiersUsed(ctx context.Context, nullifierHexes []string) (map[string]bool, error)
}

type SyncObserver interface {
	OnForcedRescan()
	OnRollbackReset(cachedHeight, currentHeight int64)
	OnSyncRange(fromHeight, toHeight int64)
	OnNotesFound(txHash string, count int)
}

type SyncInput struct {
	UserAddress         string
	RootSeed            []byte
	Wallet              *LocalWalletData
	ForceRescan         bool
	PageLimit           int
	SkipViewTagMismatch bool
}

type SyncDiagnostics struct {
	LoadedLastHeight  int64 `json:"loaded_last_height"`
	LoadedNoteCount   int   `json:"loaded_note_count"`
	ScannedFromHeight int64 `json:"scanned_from_height"`
	ScannedToHeight   int64 `json:"scanned_to_height"`
	ForcedRescan      bool  `json:"forced_rescan"`
	RollbackReset     bool  `json:"rollback_reset"`
	NormalizedCache   bool  `json:"normalized_cache"`
	NewNotesFound     int   `json:"new_notes_found"`
	FinalNoteCount    int   `json:"final_note_count"`
}

type SyncResult struct {
	Wallet        *LocalWalletData
	Notes         []FoundNote
	Diagnostics   SyncDiagnostics
	WalletChanged bool
}

var errUnsupportedScanEventsVersion = errors.New("unsupported scan events version")

func SyncNotes(
	ctx context.Context,
	source PrivacyTxSource,
	checker NullifierUsageChecker,
	observer SyncObserver,
	input SyncInput,
) (*SyncResult, error) {
	if source == nil {
		return nil, fmt.Errorf("a privacy tx source is required to scan notes")
	}
	if checker == nil {
		return nil, fmt.Errorf("a nullifier checker is required to scan notes")
	}
	if input.UserAddress == "" {
		return nil, fmt.Errorf("a transparent --from account is required to scan shielded notes")
	}
	if len(input.RootSeed) == 0 {
		return nil, fmt.Errorf("a privacy root seed is required to scan shielded notes")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pageLimit := input.PageLimit
	if pageLimit <= 0 {
		pageLimit = 100
	}

	wallet := input.Wallet
	if wallet == nil {
		wallet = &LocalWalletData{
			LastHeight:   0,
			LastSequence: 0,
			Notes:        []FoundNote{},
		}
	}

	diagnostics := SyncDiagnostics{
		LoadedLastHeight: wallet.LastHeight,
		LoadedNoteCount:  len(wallet.Notes),
	}
	walletChanged := false
	spendScalar, _, _ := privacyidentity.DeriveSpendKeys(input.RootSeed)
	viewScalar, _, _ := privacyidentity.DeriveViewKeys(input.RootSeed)
	scanOptions := processOptions{
		SkipViewTagMismatch: input.SkipViewTagMismatch && !input.ForceRescan,
	}

	if normalizedNotes, changed := NormalizeFoundNotes(wallet.Notes); changed {
		wallet.Notes = normalizedNotes
		walletChanged = true
		diagnostics.NormalizedCache = true
	}

	currentHeight, err := source.LatestBlockHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node status: %w", err)
	}
	if input.ForceRescan {
		if observer != nil {
			observer.OnForcedRescan()
		}
		wallet.LastHeight = 0
		wallet.LastSequence = 0
		wallet.Notes = []FoundNote{}
		walletChanged = true
		diagnostics.ForcedRescan = true
	}
	if wallet.LastHeight > currentHeight {
		if observer != nil {
			observer.OnRollbackReset(wallet.LastHeight, currentHeight)
		}
		scanOptions.SkipViewTagMismatch = false
		wallet.LastHeight = 0
		wallet.LastSequence = 0
		wallet.Notes = []FoundNote{}
		walletChanged = true
		diagnostics.RollbackReset = true
	}

	diagnostics.ScannedToHeight = currentHeight
	if currentHeight > wallet.LastHeight {
		diagnostics.ScannedFromHeight = wallet.LastHeight + 1
		if observer != nil {
			observer.OnSyncRange(wallet.LastHeight+1, currentHeight)
		}

		newNotes, changed, err := syncNewNotes(ctx, source, observer, input.RootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
		if err != nil {
			return nil, err
		}
		walletChanged = walletChanged || changed
		diagnostics.NewNotesFound += newNotes

		wallet.LastHeight = currentHeight
		wallet.LastSequence = ^uint64(0)
		walletChanged = true
	}

	if normalizedNotes, changed := NormalizeFoundNotes(wallet.Notes); changed {
		wallet.Notes = normalizedNotes
		walletChanged = true
		diagnostics.NormalizedCache = true
	}

	finalResults := make([]FoundNote, len(wallet.Notes))
	copy(finalResults, wallet.Notes)

	markSpentStatuses(ctx, checker, finalResults)

	diagnostics.FinalNoteCount = len(finalResults)

	return &SyncResult{
		Wallet:        wallet,
		Notes:         finalResults,
		Diagnostics:   diagnostics,
		WalletChanged: walletChanged,
	}, nil
}

func syncNewNotes(
	ctx context.Context,
	source PrivacyTxSource,
	observer SyncObserver,
	rootSeed []byte,
	spendScalar *big.Int,
	viewScalar *big.Int,
	wallet *LocalWalletData,
	pageLimit int,
	scanOptions processOptions,
) (int, bool, error) {
	if scanSource, ok := source.(PrivacyScanEventSource); ok {
		newNotes, changed, err := syncNewNotesFromScanEvents(ctx, scanSource, observer, rootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
		if err == nil {
			return newNotes, changed, nil
		}
		if !changed && shouldFallbackFromScanEvents(err) {
			return syncNewNotesFromTxSearch(ctx, source, observer, rootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
		}
		return newNotes, changed, err
	}
	return syncNewNotesFromTxSearch(ctx, source, observer, rootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
}

func shouldFallbackFromScanEvents(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, errUnsupportedScanEventsVersion) {
		return true
	}
	switch status.Code(err) {
	case codes.Unimplemented, codes.NotFound, codes.Unavailable:
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "scan events querier is required")
}

func syncNewNotesFromScanEvents(
	ctx context.Context,
	source PrivacyScanEventSource,
	observer SyncObserver,
	rootSeed []byte,
	spendScalar *big.Int,
	viewScalar *big.Int,
	wallet *LocalWalletData,
	pageLimit int,
	scanOptions processOptions,
) (int, bool, error) {
	newNotes := 0
	walletChanged := false
	afterHeight := wallet.LastHeight
	afterSequence := wallet.LastSequence

	for {
		response, err := source.ScanPrivacyEvents(ctx, afterHeight, afterSequence, pageLimit)
		if err != nil {
			return newNotes, walletChanged, fmt.Errorf("failed to scan privacy events: %w", err)
		}
		if response == nil {
			break
		}
		if err := validateScanEventsResponseVersions(response); err != nil {
			return newNotes, walletChanged, err
		}

		for _, event := range response.Events {
			found := processScanEventWithOptions(event, rootSeed, spendScalar, viewScalar, scanOptions)
			if len(found) == 0 {
				continue
			}

			wallet.Notes = append(wallet.Notes, found...)
			walletChanged = true
			newNotes += len(found)
			if observer != nil {
				observer.OnNotesFound(event.TxHashHex, len(found))
			}
		}

		if response.NextHeight == afterHeight && response.NextSequence == afterSequence {
			if response.HasMore {
				return newNotes, walletChanged, fmt.Errorf("scan events cursor did not advance")
			}
			break
		}
		afterHeight = response.NextHeight
		afterSequence = response.NextSequence
		if !response.HasMore {
			break
		}
	}

	return newNotes, walletChanged, nil
}

func validateScanEventsResponseVersions(response *privacytypes.QueryScanEventsResponse) error {
	if response == nil {
		return nil
	}
	if response.ScanFormatVersion != privacytypes.ScanFormatVersion {
		return fmt.Errorf("%w: scan_format_version got %d expected %d", errUnsupportedScanEventsVersion, response.ScanFormatVersion, privacytypes.ScanFormatVersion)
	}
	if response.ViewTagVersion != privacytypes.ViewTagVersion {
		return fmt.Errorf("%w: view_tag_version got %d expected %d", errUnsupportedScanEventsVersion, response.ViewTagVersion, privacytypes.ViewTagVersion)
	}
	return nil
}

func syncNewNotesFromTxSearch(
	ctx context.Context,
	source PrivacyTxSource,
	observer SyncObserver,
	rootSeed []byte,
	spendScalar *big.Int,
	viewScalar *big.Int,
	wallet *LocalWalletData,
	pageLimit int,
	scanOptions processOptions,
) (int, bool, error) {
	newNotes := 0
	walletChanged := false
	page := 1
	for {
		txs, err := source.SearchPrivacyTxs(ctx, wallet.LastHeight, page, pageLimit)
		if err != nil {
			return 0, false, fmt.Errorf("failed to search txs: %w", err)
		}
		if len(txs) == 0 {
			break
		}

		for _, txRes := range txs {
			found := processTxWithOptions(txRes, rootSeed, spendScalar, viewScalar, scanOptions)
			if len(found) == 0 {
				continue
			}

			wallet.Notes = append(wallet.Notes, found...)
			walletChanged = true
			newNotes += len(found)
			if observer != nil {
				observer.OnNotesFound(fmt.Sprintf("%X", txRes.Hash), len(found))
			}
		}

		if len(txs) < pageLimit {
			break
		}
		page++
	}

	return newNotes, walletChanged, nil
}

func markSpentStatuses(ctx context.Context, checker NullifierUsageChecker, notes []FoundNote) {
	if batchChecker, ok := checker.(BatchNullifierUsageChecker); ok {
		nullifiers := uniqueNullifiers(notes)
		if usedByNullifier, err := batchChecker.CheckNullifiersUsed(ctx, nullifiers); err == nil {
			if allNullifierStatusesPresent(nullifiers, usedByNullifier) {
				for i := range notes {
					notes[i].IsSpent = usedByNullifier[strings.ToLower(strings.TrimSpace(notes[i].Nullifier))]
				}
				return
			}
		}
	}

	for i := range notes {
		used, err := checker.CheckNullifierUsed(ctx, notes[i].Nullifier)
		notes[i].IsSpent = err == nil && used
	}
}

func allNullifierStatusesPresent(nullifiers []string, usedByNullifier map[string]bool) bool {
	for _, nullifier := range nullifiers {
		if _, ok := usedByNullifier[strings.ToLower(strings.TrimSpace(nullifier))]; !ok {
			return false
		}
	}
	return true
}

func uniqueNullifiers(notes []FoundNote) []string {
	seen := make(map[string]struct{}, len(notes))
	nullifiers := make([]string, 0, len(notes))
	for _, note := range notes {
		nullifier := strings.ToLower(strings.TrimSpace(note.Nullifier))
		if nullifier == "" {
			continue
		}
		if _, ok := seen[nullifier]; ok {
			continue
		}
		seen[nullifier] = struct{}{}
		nullifiers = append(nullifiers, nullifier)
	}
	return nullifiers
}
