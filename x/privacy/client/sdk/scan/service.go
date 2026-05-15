package scan

import (
	"context"
	"fmt"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"

	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
)

type PrivacyTxSource interface {
	LatestBlockHeight(ctx context.Context) (int64, error)
	SearchPrivacyTxs(ctx context.Context, afterHeight int64, page, limit int) ([]*cmttypes.ResultTx, error)
}

type NullifierUsageChecker interface {
	CheckNullifierUsed(ctx context.Context, nullifierHex string) (bool, error)
}

type SyncObserver interface {
	OnForcedRescan()
	OnRollbackReset(cachedHeight, currentHeight int64)
	OnSyncRange(fromHeight, toHeight int64)
	OnNotesFound(txHash string, count int)
}

type SyncInput struct {
	UserAddress string
	RootSeed    []byte
	Wallet      *LocalWalletData
	ForceRescan bool
	PageLimit   int
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
			LastHeight: 0,
			Notes:      []FoundNote{},
		}
	}

	diagnostics := SyncDiagnostics{
		LoadedLastHeight: wallet.LastHeight,
		LoadedNoteCount:  len(wallet.Notes),
	}
	walletChanged := false
	spendScalar, _, _ := privacyidentity.DeriveSpendKeys(input.RootSeed)
	viewScalar, _, _ := privacyidentity.DeriveViewKeys(input.RootSeed)

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
		wallet.Notes = []FoundNote{}
		walletChanged = true
		diagnostics.ForcedRescan = true
	}
	if wallet.LastHeight > currentHeight {
		if observer != nil {
			observer.OnRollbackReset(wallet.LastHeight, currentHeight)
		}
		wallet.LastHeight = 0
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

		page := 1
		for {
			txs, err := source.SearchPrivacyTxs(ctx, wallet.LastHeight, page, pageLimit)
			if err != nil {
				return nil, fmt.Errorf("failed to search txs: %w", err)
			}
			if len(txs) == 0 {
				break
			}

			for _, txRes := range txs {
				found := ProcessTx(txRes, input.RootSeed, spendScalar, viewScalar)
				if len(found) == 0 {
					continue
				}

				wallet.Notes = append(wallet.Notes, found...)
				walletChanged = true
				diagnostics.NewNotesFound += len(found)
				if observer != nil {
					observer.OnNotesFound(fmt.Sprintf("%X", txRes.Hash), len(found))
				}
			}

			if len(txs) < pageLimit {
				break
			}
			page++
		}

		wallet.LastHeight = currentHeight
		walletChanged = true
	}

	if normalizedNotes, changed := NormalizeFoundNotes(wallet.Notes); changed {
		wallet.Notes = normalizedNotes
		walletChanged = true
		diagnostics.NormalizedCache = true
	}

	finalResults := make([]FoundNote, len(wallet.Notes))
	copy(finalResults, wallet.Notes)

	for i := range finalResults {
		used, err := checker.CheckNullifierUsed(ctx, finalResults[i].Nullifier)
		finalResults[i].IsSpent = err == nil && used
	}

	diagnostics.FinalNoteCount = len(finalResults)

	return &SyncResult{
		Wallet:        wallet,
		Notes:         finalResults,
		Diagnostics:   diagnostics,
		WalletChanged: walletChanged,
	}, nil
}
