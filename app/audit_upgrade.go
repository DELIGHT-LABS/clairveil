package app

import (
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	upgrade "github.com/cosmos/cosmos-sdk/x/upgrade"
)

func (app *ClairveilApp) UpgradePreBlocker(ctx sdk.Context, _ *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	result, err := upgrade.PreBlocker(ctx, app.auditUpgradeKeeper)
	if err != nil {
		return nil, err
	}
	return &sdk.ResponsePreBlock{ConsensusParamsChanged: result.IsConsensusParamsChanged()}, nil
}
