package main

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestConfigureSDKUsesClairveilAddressPrefix(t *testing.T) {
	configureSDK()

	if got := sdk.GetConfig().GetBech32AccountAddrPrefix(); got != "clair" {
		t.Fatalf("unexpected account address prefix %q", got)
	}
}
