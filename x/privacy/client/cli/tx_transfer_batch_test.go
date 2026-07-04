package cli

import (
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestParseTransferBatchCoinsAcceptsSameDenomPositiveAmounts(t *testing.T) {
	coins, err := parseTransferBatchCoins([]string{"1uclair", "2uclair", "3uclair"})
	require.NoError(t, err)
	require.Len(t, coins, 3)
	require.Equal(t, "uclair", coins[0].Denom)
	require.Equal(t, int64(2), coins[1].Amount.Int64())
}

func TestParseTransferBatchCoinsRejectsMixedDenom(t *testing.T) {
	_, err := parseTransferBatchCoins([]string{"1uclair", "2uatom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "one denom")
}

func TestParseTransferBatchCoinsRejectsNonPositiveAmount(t *testing.T) {
	_, err := parseTransferBatchCoins([]string{"0uclair"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "positive")
}

func TestTransferBatchUsesStandardTxCLIForNonBroadcastModes(t *testing.T) {
	cases := []struct {
		name      string
		clientCtx client.Context
		want      bool
	}{
		{
			name:      "confirmed broadcast keeps metadata path",
			clientCtx: client.Context{}.WithSkipConfirmation(true),
			want:      false,
		},
		{
			name:      "generate only",
			clientCtx: client.Context{}.WithSkipConfirmation(true).WithGenerateOnly(true),
			want:      true,
		},
		{
			name:      "dry run simulation",
			clientCtx: client.Context{}.WithSkipConfirmation(true).WithSimulation(true),
			want:      true,
		},
		{
			name:      "aux signer",
			clientCtx: client.Context{}.WithSkipConfirmation(true).WithAux(true),
			want:      true,
		},
		{
			name:      "offline",
			clientCtx: client.Context{}.WithSkipConfirmation(true).WithOffline(true),
			want:      true,
		},
		{
			name:      "confirmation prompt",
			clientCtx: client.Context{},
			want:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, transferBatchUsesStandardTxCLI(tc.clientCtx))
		})
	}
}

func TestRemoveTransferBatchInputsRemovesSelectedNullifiers(t *testing.T) {
	notes := []FoundNote{
		testTransferBatchFoundNote("a", 1),
		testTransferBatchFoundNote("b", 2),
		testTransferBatchFoundNote("c", 3),
	}

	remaining := removeTransferBatchInputs(notes, [2]FoundNote{notes[0], notes[2]})

	require.Len(t, remaining, 1)
	require.Equal(t, "b", remaining[0].Nullifier)
}

func TestRemoveTransferBatchInputsFallsBackToCommitmentKey(t *testing.T) {
	selected := FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: big.NewInt(1),
			ReceiverSpendPubKeyY: big.NewInt(2),
			ReceiverViewPubKeyX:  big.NewInt(3),
			ReceiverViewPubKeyY:  big.NewInt(4),
			Amount:               big.NewInt(5),
			AssetID:              crypto.HashString("uclair"),
			Randomness:           big.NewInt(1),
		},
	}
	other := FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: big.NewInt(1),
			ReceiverSpendPubKeyY: big.NewInt(2),
			ReceiverViewPubKeyX:  big.NewInt(3),
			ReceiverViewPubKeyY:  big.NewInt(4),
			Amount:               big.NewInt(7),
			AssetID:              crypto.HashString("uclair"),
			Randomness:           big.NewInt(2),
		},
	}

	remaining := removeTransferBatchInputs([]FoundNote{selected, other}, [2]FoundNote{selected, selected})

	require.Len(t, remaining, 1)
	require.Equal(t, int64(7), remaining[0].Note.Amount.Int64())
}

func testTransferBatchFoundNote(nullifier string, amount int64) FoundNote {
	return FoundNote{
		Note: privacytypes.Note{
			Amount:  big.NewInt(amount),
			AssetID: crypto.HashString("uclair"),
		},
		Nullifier: nullifier,
		IsSpent:   false,
	}
}
