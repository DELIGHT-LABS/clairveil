package app

import (
	"encoding/binary"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	upgrade "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

func (app *ClairveilApp) auditProtocolVersion() (uint64, error) {
	raw := app.CommitMultiStore().GetKVStore(app.keys[upgrade.StoreKey]).Get([]byte{upgrade.ProtocolVersionByte})
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("invalid committed audit protocol version")
	}
	return binary.BigEndian.Uint64(raw), nil
}

// The SDK upgrade keeper otherwise changes BaseApp's in-memory Info version
// inside a migration cache. Publish that version only after Commit succeeds.
func (app *ClairveilApp) Commit() (*abci.ResponseCommit, error) {
	if app.auditUpgradeKeeper == nil {
		return app.BaseApp.Commit()
	}
	version, err := app.auditProtocolVersion()
	if err != nil {
		return nil, err
	}
	response, err := app.BaseApp.Commit()
	if err != nil {
		return nil, err
	}
	app.BaseApp.SetProtocolVersion(version)
	return response, nil
}
