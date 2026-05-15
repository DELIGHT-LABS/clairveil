package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// SetConfig applies Clairveil address and HD-path settings to the global SDK config.
func SetConfig() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
	config.SetCoinType(CoinType)
	config.SetFullFundraiserPath(FullFundraiserPath)
	sdk.DefaultBondDenom = DefaultDenom
	config.Seal()
}
