package main

import (
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"github.com/DELIGHT-LABS/clairveil/app"
	"github.com/DELIGHT-LABS/clairveil/cmd/clairveild/cmd"
	"github.com/DELIGHT-LABS/clairveil/types"
)

func main() {
	types.SetConfig()

	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, "", app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
