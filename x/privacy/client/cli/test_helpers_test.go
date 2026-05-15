package cli

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func testBech32Address() string {
	return sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()
}
