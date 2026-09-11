package withdraw

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
)

func FindExactMatchSpendableNoteByDenom(notes []privacyscan.SecretFoundNote, denom string, targetAmount *big.Int) *privacyscan.SecretFoundNote {
	return privacytransfer.FindExactMatchSpendableNoteByDenom(notes, denom, targetAmount)
}

func BuildExactMatchError(targetCoin sdk.Coin, foundNotes []privacyscan.SecretFoundNote) error {
	sameDenomNotes, sameDenomTotal := privacytransfer.SummarizeSpendableNotesByDenom(foundNotes, targetCoin.Denom)
	if len(sameDenomNotes) == 0 {
		return fmt.Errorf(
			"withdraw requires one exact-match note for %s and does not create change; no spendable %s notes were found. Run list-notes to inspect available notes, then create an exact-match note with a shielded self-transfer before retrying withdraw or prepare-withdraw",
			targetCoin.String(),
			targetCoin.Denom,
		)
	}

	return fmt.Errorf(
		"withdraw requires one exact-match note for %s and does not create change; spendable %s notes: %s (total %s across %d notes). This command will not split larger notes or merge fragmented notes. Create an exact-match note with a shielded self-transfer, then retry withdraw or prepare-withdraw",
		targetCoin.String(),
		targetCoin.Denom,
		formatSpendableNoteAmounts(sameDenomNotes, targetCoin.Denom, 5),
		sdk.NewCoin(targetCoin.Denom, sdkmath.NewIntFromBigInt(sameDenomTotal)).String(),
		len(sameDenomNotes),
	)
}

func formatSpendableNoteAmounts(notes []privacyscan.SecretFoundNote, denom string, limit int) string {
	if len(notes) == 0 {
		return ""
	}

	amounts := make([]*big.Int, 0, len(notes))
	for _, note := range notes {
		amounts = append(amounts, new(big.Int).SetUint64(note.Note.Amount))
	}

	sort.Slice(amounts, func(i, j int) bool {
		return amounts[i].Cmp(amounts[j]) < 0
	})

	if limit <= 0 || limit > len(amounts) {
		limit = len(amounts)
	}

	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		parts = append(parts, sdk.NewCoin(denom, sdkmath.NewIntFromBigInt(amounts[i])).String())
	}
	if len(amounts) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(amounts)-limit))
	}

	return strings.Join(parts, ", ")
}
