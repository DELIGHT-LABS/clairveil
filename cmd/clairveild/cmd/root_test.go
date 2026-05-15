package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
)

func TestRootCommandIncludesReferenceChainCommands(t *testing.T) {
	clairveiltypes.SetConfig()

	rootCmd := NewRootCmd()

	for _, path := range [][]string{
		{"init"},
		{"keys"},
		{"start"},
		{"tx", "privacy"},
		{"query", "privacy"},
		{"query", "bank", "balances"},
		{"add-genesis-account"},
		{"gentx"},
		{"collect-gentxs"},
		{"validate"},
	} {
		_, _, err := rootCmd.Find(path)
		require.NoError(t, err, path)
	}
}
