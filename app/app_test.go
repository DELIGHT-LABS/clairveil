package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type testAppOptions map[string]any

func (opts testAppOptions) Get(key string) any {
	return opts[key]
}

func TestDefaultGenesisUsesClairveilDenom(t *testing.T) {
	clairveiltypes.SetConfig()
	configureTestPrivacyManifest(t)

	app := NewClairveilApp(log.NewNopLogger(), dbm.NewMemDB(), nil, false, testAppOptions{})
	genesis := app.DefaultGenesis()

	var stakingState stakingtypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], &stakingState)
	require.Equal(t, clairveiltypes.DefaultDenom, stakingState.Params.BondDenom)

	var mintState minttypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesis[minttypes.ModuleName], &mintState)
	require.Equal(t, clairveiltypes.DefaultDenom, mintState.Params.MintDenom)

	var govState govv1types.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesis[govtypes.ModuleName], &govState)
	require.Equal(t, clairveiltypes.DefaultDenom, govState.Params.MinDeposit[0].Denom)

	var bankState banktypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], &bankState)
	var metadata banktypes.Metadata
	for _, candidate := range bankState.DenomMetadata {
		if candidate.Base == clairveiltypes.DefaultDenom {
			metadata = candidate
			break
		}
	}
	require.Equal(t, "The base staking and fee token of the Clairveil reference chain.", metadata.Description)
	require.Equal(t, "clair", metadata.Display)
	require.Equal(t, "Clairveil", metadata.Name)
	require.Equal(t, "CLAIR", metadata.Symbol)
	require.Len(t, metadata.DenomUnits, 2)
	require.Equal(t, clairveiltypes.DefaultDenom, metadata.DenomUnits[0].Denom)
	require.Equal(t, uint32(0), metadata.DenomUnits[0].Exponent)
	require.Equal(t, "clair", metadata.DenomUnits[1].Denom)
	require.Equal(t, uint32(clairveiltypes.DefaultDenomPrecision), metadata.DenomUnits[1].Exponent)

	var privacyState privacytypes.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesis[privacytypes.ModuleName], &privacyState)
	require.NoError(t, privacyState.Validate())
}

func configureTestPrivacyManifest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	checksums := make(map[string]string)
	for _, descriptor := range privacyzk.DefaultArtifactDescriptors() {
		checksums[descriptor.ChecksumEnv] = strings.Repeat("a", 64)
	}
	manifest := privacyzk.ManifestFromChecksums(dir, "", checksums)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, privacyzk.ArtifactManifestFile), manifestBytes, 0o600))
	t.Setenv(privacyzk.ZKArtifactDirEnv, dir)
}
