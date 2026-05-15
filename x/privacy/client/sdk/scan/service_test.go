package scan

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

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
	nullifierChecker := stubNullifierUsageChecker{
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
	require.Len(t, result.Notes, 1)
	require.Equal(t, "7", result.Notes[0].Note.Amount.String())
	require.True(t, result.Notes[0].IsSpent)
	require.Equal(t, int64(3), result.Diagnostics.LoadedLastHeight)
	require.Equal(t, 1, result.Diagnostics.LoadedNoteCount)
	require.Equal(t, int64(4), result.Diagnostics.ScannedFromHeight)
	require.Equal(t, int64(11), result.Diagnostics.ScannedToHeight)
	require.True(t, result.Diagnostics.NormalizedCache)
	require.Equal(t, 1, result.Diagnostics.NewNotesFound)
	require.Equal(t, 1, result.Diagnostics.FinalNoteCount)
	require.Equal(t, [][2]int64{{4, 11}}, observer.syncRanges)
	require.Equal(t, []noteFoundEvent{{txHash: "AABB", count: 1}}, observer.notesFound)
}

func TestSyncNotesResetsWalletWhenCachedHeightRollsBack(t *testing.T) {
	rootSeed := []byte("scan-service-rollback-seed")
	txSource := stubPrivacyTxSource{latestBlockHeight: 5}
	checker := stubNullifierUsageChecker{}
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
				LastHeight: 8,
				Notes: []FoundNote{
					{Nullifier: "aa"},
				},
			},
		},
	)

	require.NoError(t, err)
	require.Equal(t, int64(5), result.Wallet.LastHeight)
	require.Empty(t, result.Notes)
	require.True(t, result.Diagnostics.RollbackReset)
	require.Equal(t, [][2]int64{{8, 5}}, observer.rollbackResets)
}

func TestSyncNotesRequiresUserAddress(t *testing.T) {
	_, err := SyncNotes(
		context.Background(),
		stubPrivacyTxSource{},
		stubNullifierUsageChecker{},
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

type stubNullifierUsageChecker struct {
	used map[string]bool
}

func (s stubNullifierUsageChecker) CheckNullifierUsed(_ context.Context, nullifierHex string) (bool, error) {
	if s.used == nil {
		return false, nil
	}

	return s.used[nullifierHex], nil
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

	cipherBytes, err := privacycrypto.Encrypt(note.Bytes(), rootSeed)
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
							Key:   "encrypted_note",
							Value: hex.EncodeToString(cipherBytes),
						},
					},
				},
			},
		},
	}
}
