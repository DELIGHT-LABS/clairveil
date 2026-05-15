package app

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
)

func (app *ClairveilApp) applyClairveilGenesisDefaults(genesis map[string]json.RawMessage) {
	ApplyClairveilGenesisDefaults(app.appCodec, genesis)
}

// ApplyClairveilGenesisDefaults rewrites generic SDK genesis defaults to the
// canonical Clairveil reference-chain defaults.
func ApplyClairveilGenesisDefaults(cdc codec.Codec, genesis map[string]json.RawMessage) {
	setStakingBondDenom(cdc, genesis)
	setMintDenom(cdc, genesis)
	setGovDepositDenom(cdc, genesis)
	setBankMetadata(cdc, genesis)
}

func setStakingBondDenom(cdc codec.Codec, genesis map[string]json.RawMessage) {
	var state stakingtypes.GenesisState
	cdc.MustUnmarshalJSON(genesis[stakingtypes.ModuleName], &state)
	state.Params.BondDenom = clairveiltypes.DefaultDenom
	genesis[stakingtypes.ModuleName] = cdc.MustMarshalJSON(&state)
}

func setMintDenom(cdc codec.Codec, genesis map[string]json.RawMessage) {
	var state minttypes.GenesisState
	cdc.MustUnmarshalJSON(genesis[minttypes.ModuleName], &state)
	state.Params.MintDenom = clairveiltypes.DefaultDenom
	genesis[minttypes.ModuleName] = cdc.MustMarshalJSON(&state)
}

func setGovDepositDenom(cdc codec.Codec, genesis map[string]json.RawMessage) {
	var state govv1types.GenesisState
	cdc.MustUnmarshalJSON(genesis[govtypes.ModuleName], &state)
	state.Params.MinDeposit = sdk.NewCoins(sdk.NewInt64Coin(clairveiltypes.DefaultDenom, 10_000_000))
	genesis[govtypes.ModuleName] = cdc.MustMarshalJSON(&state)
}

func setBankMetadata(cdc codec.Codec, genesis map[string]json.RawMessage) {
	var state banktypes.GenesisState
	cdc.MustUnmarshalJSON(genesis[banktypes.ModuleName], &state)
	state.DenomMetadata = append(state.DenomMetadata, banktypes.Metadata{
		Description: "The base staking and fee token of the Clairveil reference chain.",
		Base:        clairveiltypes.DefaultDenom,
		Display:     "clair",
		Name:        "Clairveil",
		Symbol:      "CLAIR",
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    clairveiltypes.DefaultDenom,
				Exponent: 0,
			},
			{
				Denom:    "clair",
				Exponent: uint32(clairveiltypes.DefaultDenomPrecision),
			},
		},
	})
	genesis[banktypes.ModuleName] = cdc.MustMarshalJSON(&state)
}
