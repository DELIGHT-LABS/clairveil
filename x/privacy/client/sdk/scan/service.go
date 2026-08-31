package scan

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
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

type PrivacyScanV2Source interface {
	PrivacyScan(ctx context.Context, after *privacytypes.PrivacyScanCursorV1, outputLimit, eventLimit uint32, maxEncodedBytes uint64, eventTypes []string) (*privacytypes.QueryPrivacyScanResponse, error)
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
	UserAddress                string
	RootSeed                   []byte
	Wallet                     *LocalWalletData
	ForceRescan                bool
	PageLimit                  int
	SkipViewTagMismatch        bool
	PrivacyScanEventLimit      uint32
	PrivacyScanMaxEncodedBytes uint64
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

	originalWallet := input.Wallet
	wallet := cloneLocalWalletData(originalWallet)
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
		EventLimit:          input.PrivacyScanEventLimit,
		MaxEncodedBytes:     input.PrivacyScanMaxEncodedBytes,
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
		wallet.LastOutputIndex = 0
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
		wallet.LastOutputIndex = 0
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

		if _, typed := source.(PrivacyScanV2Source); !typed {
			wallet.LastHeight = currentHeight
			wallet.LastSequence = ^uint64(0)
			wallet.LastOutputIndex = ^uint32(0)
		}
		walletChanged = true
	}

	if normalizedNotes, changed := NormalizeFoundNotes(wallet.Notes); changed {
		wallet.Notes = normalizedNotes
		walletChanged = true
		diagnostics.NormalizedCache = true
	}

	finalResults := make([]FoundNote, len(wallet.Notes))
	copy(finalResults, wallet.Notes)
	diagnostics.FinalNoteCount = len(finalResults)

	spentStatusesChanged, err := markSpentStatuses(ctx, checker, finalResults)
	if err != nil {
		// Preserve the existing scan cursor on failure, but never leave a cached
		// note marked verified-unspent after its latest nullifier query failed.
		verificationChanged := copyNullifierVerificationToCachedNotes(originalWallet, finalResults)
		// Keep the durable wallet/cursor at its pre-scan value, but return every
		// note discovered during this scan so read-only callers can show it as
		// unverified instead of silently dropping it from the partial result.
		partialNotes := append([]FoundNote(nil), finalResults...)
		result := &SyncResult{
			Wallet:        originalWallet,
			Notes:         partialNotes,
			Diagnostics:   diagnostics,
			WalletChanged: verificationChanged,
		}
		if errors.Is(err, ErrInvalidWalletCache) {
			return result, fmt.Errorf("failed to validate local wallet cache; force a rescan before retrying: %w", err)
		}
		return result, fmt.Errorf("failed to verify note nullifier status: %w", err)
	}
	wallet.Notes = append([]FoundNote(nil), finalResults...)
	walletChanged = walletChanged || spentStatusesChanged

	if originalWallet != nil {
		*originalWallet = *wallet
		wallet = originalWallet
	}

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
	if typedSource, ok := source.(PrivacyScanV2Source); ok {
		// Typed query failures are terminal. Falling back to minimal ABCI events
		// would silently omit batch ciphertexts.
		return syncNewNotesFromPrivacyScanV2(ctx, typedSource, observer, rootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
	}
	if scanSource, ok := source.(PrivacyScanEventSource); ok {
		newNotes, changed, err := syncNewNotesFromScanEvents(ctx, scanSource, observer, rootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
		if err == nil {
			return newNotes, changed, nil
		}
		if !changed && shouldFallbackFromScanEvents(err) {
			fallbackWallet := legacyFallbackWallet(wallet)
			newNotes, changed, fallbackErr := syncNewNotesFromTxSearch(ctx, source, observer, rootSeed, spendScalar, viewScalar, fallbackWallet, pageLimit, scanOptions)
			if fallbackWallet != wallet && changed {
				wallet.Notes = fallbackWallet.Notes
			}
			return newNotes, changed, fallbackErr
		}
		return newNotes, changed, err
	}
	return syncNewNotesFromTxSearch(ctx, source, observer, rootSeed, spendScalar, viewScalar, wallet, pageLimit, scanOptions)
}

func syncNewNotesFromPrivacyScanV2(ctx context.Context, source PrivacyScanV2Source, observer SyncObserver, rootSeed []byte, spendScalar, viewScalar *big.Int, wallet *LocalWalletData, pageLimit int, scanOptions processOptions) (int, bool, error) {
	startCursor := &privacytypes.PrivacyScanCursorV1{Height: wallet.LastHeight, GlobalSequence: wallet.LastSequence, OutputIndex: wallet.LastOutputIndex}
	cursor := &privacytypes.PrivacyScanCursorV1{Height: startCursor.Height, GlobalSequence: startCursor.GlobalSequence, OutputIndex: startCursor.OutputIndex}
	seen := make(map[string]struct{}, len(wallet.Notes))
	selfViewByEvent := make(map[string]bool)
	newlyFound := make([]FoundNote, 0)
	for _, note := range wallet.Notes {
		seen[foundNoteIdentityKey(note)] = struct{}{}
	}
	for {
		// Request every typed event so zero-output summaries can prove any cursor
		// advance beyond the final returned output without a lossy filtered gap.
		response, err := source.PrivacyScan(ctx, cursor, uint32(pageLimit), scanOptions.EventLimit, scanOptions.MaxEncodedBytes, nil)
		if err != nil {
			return 0, false, fmt.Errorf("failed typed privacy scan (ABCI fallback disabled): %w", err)
		}
		if response == nil || response.NextCursor == nil {
			return 0, false, fmt.Errorf("typed privacy scan response/cursor is unavailable")
		}
		if err := validateTypedScanResponseForSync(response, cursor, selfViewByEvent); err != nil {
			return 0, false, fmt.Errorf("invalid typed privacy scan response: %w", err)
		}
		for _, output := range response.Outputs {
			found, decryptErr := ProcessPrivacyScanOutput(output, rootSeed, spendScalar, viewScalar, scanOptions.SkipViewTagMismatch)
			if decryptErr != nil {
				if errors.Is(decryptErr, ErrPrivacyScanOutputNotOwned) {
					continue
				}
				return 0, false, fmt.Errorf("invalid typed privacy scan output: %w", decryptErr)
			}
			key := foundNoteIdentityKey(*found)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			newlyFound = append(newlyFound, *found)
		}
		if compareScanCursor(response.NextCursor, cursor) < 0 || (response.HasMore && compareScanCursor(response.NextCursor, cursor) <= 0) {
			return 0, false, fmt.Errorf("typed privacy scan cursor did not advance")
		}
		cursor = &privacytypes.PrivacyScanCursorV1{Height: response.NextCursor.Height, GlobalSequence: response.NextCursor.GlobalSequence, OutputIndex: response.NextCursor.OutputIndex}
		if !response.HasMore {
			break
		}
	}
	wallet.Notes = append(wallet.Notes, newlyFound...)
	wallet.LastHeight, wallet.LastSequence, wallet.LastOutputIndex = cursor.Height, cursor.GlobalSequence, cursor.OutputIndex
	if observer != nil {
		for _, found := range newlyFound {
			observer.OnNotesFound(found.TxHash, 1)
		}
	}
	changed := len(newlyFound) > 0 || compareScanCursor(cursor, startCursor) != 0
	return len(newlyFound), changed, nil
}

func validateTypedScanResponseForSync(response *privacytypes.QueryPrivacyScanResponse, after *privacytypes.PrivacyScanCursorV1, selfViewByEvent map[string]bool) error {
	if response.ScanSchemaVersion != privacytypes.PrivacyScanSchemaVersionV2 {
		return fmt.Errorf("unsupported scan schema version %q", response.ScanSchemaVersion)
	}
	previous := after
	for _, output := range response.Outputs {
		if output == nil {
			return fmt.Errorf("nil typed output")
		}
		cursor := &privacytypes.PrivacyScanCursorV1{Height: output.Height, GlobalSequence: output.GlobalSequence, OutputIndex: output.OutputIndex}
		if previous != nil && compareScanCursor(cursor, previous) <= 0 {
			return fmt.Errorf("typed outputs are not strictly ordered")
		}
		if previous != nil && previous.Height == cursor.Height && previous.GlobalSequence == cursor.GlobalSequence && cursor.OutputIndex != previous.OutputIndex+1 {
			return fmt.Errorf("typed output indices are not contiguous")
		}
		if previous != nil && (previous.Height != cursor.Height || previous.GlobalSequence != cursor.GlobalSequence) && cursor.OutputIndex != 0 {
			return fmt.Errorf("typed event output does not start at zero")
		}
		if output.EventType == privacytypes.EventTypeBatchTransferV1 {
			event := fmt.Sprintf("%d/%d", output.Height, output.GlobalSequence)
			selfViewPresent := len(output.SelfViewDisclosurePayload) > 0
			if expected, exists := selfViewByEvent[event]; exists && expected != selfViewPresent {
				return fmt.Errorf("batch self-view disclosure is not all-or-none")
			}
			selfViewByEvent[event] = selfViewPresent
		}
		previous = cursor
	}
	if err := validateTypedScanCursorBoundary(response, after); err != nil {
		return err
	}
	if response.HasMore && compareScanCursor(response.NextCursor, after) <= 0 {
		return fmt.Errorf("typed has_more response did not advance")
	}
	return nil
}

func validateTypedScanCursorBoundary(response *privacytypes.QueryPrivacyScanResponse, after *privacytypes.PrivacyScanCursorV1) error {
	if after == nil {
		after = &privacytypes.PrivacyScanCursorV1{}
	}
	from := after
	if len(response.Outputs) > 0 {
		last := response.Outputs[len(response.Outputs)-1]
		from = &privacytypes.PrivacyScanCursorV1{Height: last.Height, GlobalSequence: last.GlobalSequence, OutputIndex: last.OutputIndex}
		switch comparison := compareScanCursor(response.NextCursor, from); {
		case comparison < 0:
			return fmt.Errorf("typed next cursor precedes the final output")
		case comparison == 0:
			return nil
		}
	} else if compareScanCursor(response.NextCursor, from) <= 0 {
		return nil
	}

	summaries := make(map[string]*privacytypes.PrivacyScanSummaryV2, len(response.Summaries))
	for _, summary := range response.Summaries {
		if summary == nil {
			return fmt.Errorf("nil typed summary")
		}
		key := fmt.Sprintf("%d/%d", summary.Height, summary.GlobalSequence)
		if _, duplicate := summaries[key]; duplicate {
			return fmt.Errorf("duplicate typed summary %s", key)
		}
		summaries[key] = summary
	}
	if len(response.Outputs) > 0 {
		last := response.Outputs[len(response.Outputs)-1]
		lastSummary := summaries[fmt.Sprintf("%d/%d", last.Height, last.GlobalSequence)]
		if lastSummary == nil || last.OutputIndex+1 != lastSummary.OutputCount {
			return fmt.Errorf("typed next cursor advances past an incomplete output event")
		}
	}

	fromEvent := &privacytypes.PrivacyScanCursorV1{Height: from.Height, GlobalSequence: from.GlobalSequence}
	nextEvent := &privacytypes.PrivacyScanCursorV1{Height: response.NextCursor.Height, GlobalSequence: response.NextCursor.GlobalSequence}
	if response.NextCursor.OutputIndex != 0 || compareScanCursor(nextEvent, fromEvent) <= 0 {
		return fmt.Errorf("typed next cursor advance is not an event boundary")
	}
	nextSummary := summaries[fmt.Sprintf("%d/%d", response.NextCursor.Height, response.NextCursor.GlobalSequence)]
	if nextSummary == nil || nextSummary.OutputCount != 0 {
		return fmt.Errorf("typed next cursor advance lacks a zero-output summary")
	}
	for _, summary := range response.Summaries {
		cursor := &privacytypes.PrivacyScanCursorV1{Height: summary.Height, GlobalSequence: summary.GlobalSequence}
		if compareScanCursor(cursor, fromEvent) > 0 && compareScanCursor(cursor, nextEvent) <= 0 && summary.OutputCount != 0 {
			return fmt.Errorf("typed next cursor skips an output-bearing summary")
		}
	}
	return nil
}

func compareScanCursor(a, b *privacytypes.PrivacyScanCursorV1) int {
	if a.Height != b.Height {
		if a.Height < b.Height {
			return -1
		}
		return 1
	}
	if a.GlobalSequence != b.GlobalSequence {
		if a.GlobalSequence < b.GlobalSequence {
			return -1
		}
		return 1
	}
	if a.OutputIndex < b.OutputIndex {
		return -1
	}
	if a.OutputIndex > b.OutputIndex {
		return 1
	}
	return 0
}

// legacyFallbackWallet rewinds a sequence cursor by one height because the
// legacy endpoint cannot express an in-block sequence. Found notes are
// deduplicated when appended, so replaying the boundary height is safe.
func legacyFallbackWallet(wallet *LocalWalletData) *LocalWalletData {
	if wallet == nil || wallet.LastSequence == 0 {
		return wallet
	}
	fallback := cloneLocalWalletData(wallet)
	if fallback.LastHeight > 0 {
		fallback.LastHeight--
	}
	fallback.LastSequence = 0
	return fallback
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
	knownNotes := foundNoteIdentitySet(wallet.Notes)

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

			added := appendUniqueFoundNotes(knownNotes, found)
			if len(added) == 0 {
				continue
			}
			wallet.Notes = append(wallet.Notes, added...)
			walletChanged = true
			newNotes += len(added)
			if observer != nil {
				observer.OnNotesFound(event.TxHashHex, len(added))
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
	knownNotes := foundNoteIdentitySet(wallet.Notes)
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

			added := appendUniqueFoundNotes(knownNotes, found)
			if len(added) == 0 {
				continue
			}
			wallet.Notes = append(wallet.Notes, added...)
			walletChanged = true
			newNotes += len(added)
			if observer != nil {
				observer.OnNotesFound(fmt.Sprintf("%X", txRes.Hash), len(added))
			}
		}

		if len(txs) < pageLimit {
			break
		}
		page++
	}

	return newNotes, walletChanged, nil
}

func foundNoteIdentitySet(notes []FoundNote) map[string]struct{} {
	known := make(map[string]struct{}, len(notes))
	for _, note := range notes {
		known[foundNoteIdentityKey(note)] = struct{}{}
	}
	return known
}

func appendUniqueFoundNotes(known map[string]struct{}, found []FoundNote) []FoundNote {
	if len(found) == 0 {
		return nil
	}
	added := make([]FoundNote, 0, len(found))
	for _, note := range found {
		key := foundNoteIdentityKey(note)
		if _, exists := known[key]; exists {
			continue
		}
		known[key] = struct{}{}
		added = append(added, note)
	}
	return added
}

func markSpentStatuses(ctx context.Context, checker NullifierUsageChecker, notes []FoundNote) (bool, error) {
	original := make([]nullifierVerificationState, len(notes))
	for i := range notes {
		original[i] = nullifierVerificationState{
			isSpent:         notes[i].IsSpent,
			verifiedUnspent: notes[i].VerifiedUnspent,
		}
	}
	// A prior successful scan must not authorize spending while this refresh is
	// incomplete. Keep an existing spent marker, but downgrade every unspent
	// cache entry until this invocation explicitly confirms it again.
	for i := range notes {
		if notes[i].IsSpent || !notes[i].VerifiedUnspent {
			continue
		}
		notes[i].VerifiedUnspent = false
	}
	canonicalKeys, normalizeErr := canonicalNullifierKeys(notes)
	if normalizeErr != nil {
		return false, fmt.Errorf("validate cached note nullifier: %w", redactedInvalidWalletCacheError{})
	}
	if batchChecker, ok := checker.(BatchNullifierUsageChecker); ok {
		nullifiers := uniqueCanonicalNullifiers(canonicalKeys)
		if usedByNullifier, err := batchChecker.CheckNullifiersUsed(ctx, nullifiers); err == nil {
			normalizedStatuses, normalizeErr := normalizeBatchNullifierStatuses(usedByNullifier)
			if normalizeErr == nil && allNullifierStatusesPresent(nullifiers, normalizedStatuses) {
				for i := range notes {
					used := normalizedStatuses[canonicalKeys[i]]
					verifiedUnspent := !used
					notes[i].IsSpent = used
					notes[i].VerifiedUnspent = verifiedUnspent
				}
				return nullifierVerificationChanged(notes, original), nil
			}
		}
	}

	for i := range notes {
		used, err := checker.CheckNullifierUsed(ctx, canonicalKeys[i])
		if err != nil {
			return false, fmt.Errorf("check nullifier status at note %d: %w", i, newRedactedNullifierStatusError(err))
		}
		verifiedUnspent := !used
		notes[i].IsSpent = used
		notes[i].VerifiedUnspent = verifiedUnspent
	}
	return nullifierVerificationChanged(notes, original), nil
}

type nullifierVerificationState struct {
	isSpent         bool
	verifiedUnspent bool
}

func nullifierVerificationChanged(notes []FoundNote, original []nullifierVerificationState) bool {
	if len(notes) != len(original) {
		return true
	}
	for i := range notes {
		// IsSpent is an in-memory observation (json:"-"). Only persisted
		// verification changes should trigger a wallet-file rewrite.
		if notes[i].VerifiedUnspent != original[i].verifiedUnspent {
			return true
		}
	}
	return false
}

func copyNullifierVerificationToCachedNotes(cached *LocalWalletData, refreshed []FoundNote) bool {
	if cached == nil || len(cached.Notes) == 0 {
		return false
	}
	changed := false
	byIdentity := make(map[string]FoundNote, len(refreshed))
	for _, note := range refreshed {
		byIdentity[foundNoteIdentityKey(note)] = note
	}
	for i := range cached.Notes {
		refreshedNote, ok := byIdentity[foundNoteIdentityKey(cached.Notes[i])]
		if !ok {
			if cached.Notes[i].VerifiedUnspent {
				changed = true
			}
			cached.Notes[i].VerifiedUnspent = false
			continue
		}
		if cached.Notes[i].VerifiedUnspent != refreshedNote.VerifiedUnspent {
			changed = true
		}
		cached.Notes[i].VerifiedUnspent = refreshedNote.VerifiedUnspent
		if refreshedNote.IsSpent {
			cached.Notes[i].IsSpent = true
		}
	}
	return changed
}

var (
	ErrNullifierStatusUnavailable = errors.New("nullifier status unavailable")
	ErrInvalidWalletCache         = errors.New("invalid wallet cache")
)

type redactedInvalidWalletCacheError struct{}

func (redactedInvalidWalletCacheError) Error() string {
	return "cached note validation failed"
}

func (redactedInvalidWalletCacheError) Unwrap() error {
	return ErrInvalidWalletCache
}

type redactedNullifierStatusError struct {
	contextCause error
}

func newRedactedNullifierStatusError(cause error) redactedNullifierStatusError {
	redacted := redactedNullifierStatusError{}
	switch {
	case errors.Is(cause, context.Canceled):
		redacted.contextCause = context.Canceled
	case errors.Is(cause, context.DeadlineExceeded):
		redacted.contextCause = context.DeadlineExceeded
	}
	return redacted
}

func (e redactedNullifierStatusError) Error() string {
	return "query failed"
}

func (e redactedNullifierStatusError) Unwrap() []error {
	causes := []error{ErrNullifierStatusUnavailable}
	if e.contextCause != nil {
		causes = append(causes, e.contextCause)
	}
	return causes
}

func cloneLocalWalletData(wallet *LocalWalletData) *LocalWalletData {
	if wallet == nil {
		return nil
	}
	cloned := *wallet
	cloned.Notes = append([]FoundNote(nil), wallet.Notes...)
	return &cloned
}

func allNullifierStatusesPresent(nullifiers []string, usedByNullifier map[string]bool) bool {
	for _, nullifier := range nullifiers {
		if _, ok := usedByNullifier[strings.ToLower(strings.TrimSpace(nullifier))]; !ok {
			return false
		}
	}
	return true
}

func canonicalNullifierKeys(notes []FoundNote) ([]string, error) {
	keys := make([]string, len(notes))
	for i, note := range notes {
		canonical, err := CanonicalFoundNoteNullifier(note)
		if err != nil {
			return nil, err
		}
		keys[i] = canonical
	}
	return keys, nil
}

// CanonicalFoundNoteNullifier binds cached verification state to the note
// body that proof construction will consume.
func CanonicalFoundNoteNullifier(note FoundNote) (string, error) {
	if note.Note.Randomness == nil || note.Note.ReceiverSpendPubKeyX == nil || note.Note.ReceiverSpendPubKeyY == nil {
		return "", fmt.Errorf("found note cannot compute its nullifier")
	}
	storedBytes, err := privacyfield.DecodeCanonicalHex(strings.TrimSpace(note.Nullifier), "nullifier")
	if err != nil {
		return "", fmt.Errorf("found note has an invalid nullifier")
	}
	computedBytes, err := privacyfield.CanonicalBytesFromBigInt(note.Note.ComputeNullifier())
	if err != nil {
		return "", fmt.Errorf("found note cannot compute its nullifier")
	}
	if !bytes.Equal(storedBytes, computedBytes) {
		return "", fmt.Errorf("found note nullifier does not match note contents")
	}
	return hex.EncodeToString(computedBytes), nil
}

func uniqueCanonicalNullifiers(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	nullifiers := make([]string, 0, len(keys))
	for _, nullifier := range keys {
		if _, ok := seen[nullifier]; ok {
			continue
		}
		seen[nullifier] = struct{}{}
		nullifiers = append(nullifiers, nullifier)
	}
	return nullifiers
}

func normalizeBatchNullifierStatuses(statuses map[string]bool) (map[string]bool, error) {
	normalized := make(map[string]bool, len(statuses))
	for nullifier, used := range statuses {
		nullifierBytes, err := privacyfield.DecodeCanonicalHex(strings.TrimSpace(nullifier), "nullifier")
		if err != nil {
			continue
		}
		canonical := hex.EncodeToString(nullifierBytes)
		if existing, ok := normalized[canonical]; ok && existing != used {
			return nil, fmt.Errorf("conflicting statuses for canonical nullifier")
		}
		normalized[canonical] = used
	}
	return normalized, nil
}
