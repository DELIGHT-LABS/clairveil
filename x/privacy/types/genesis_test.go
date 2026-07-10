package types

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	gs := DefaultGenesis()
	require.NotNil(t, gs)
	require.Empty(t, gs.Commitments)
	require.Empty(t, gs.HistoricalRoots)
	require.Empty(t, gs.Nullifiers)
	require.NoError(t, gs.Validate())
}

func TestGenesisValidateRejectsInvalidLength(t *testing.T) {
	gs := GenesisState{
		Commitments: [][]byte{{0x01}},
	}

	require.Error(t, gs.Validate())
}

func TestGenesisValidateRejectsNonCanonicalFieldBytes(t *testing.T) {
	nonCanonical := fr.Modulus().Bytes()

	gs := GenesisState{
		Nullifiers: [][]byte{nonCanonical[:]},
	}

	require.Error(t, gs.Validate())
}

func TestGenesisValidateRejectsDuplicateStateElements(t *testing.T) {
	value := make([]byte, genesisFieldElementByteSize)
	value[len(value)-1] = 1

	for _, gs := range []GenesisState{
		{Commitments: [][]byte{value, append([]byte(nil), value...)}},
		{HistoricalRoots: [][]byte{value, append([]byte(nil), value...)}},
		{Nullifiers: [][]byte{value, append([]byte(nil), value...)}},
	} {
		require.ErrorContains(t, gs.Validate(), "duplicates index 0")
	}
}
