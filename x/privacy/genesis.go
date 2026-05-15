package privacy

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	if err := genState.Validate(); err != nil {
		panic(fmt.Errorf("invalid privacy genesis state: %w", err))
	}

	if err := k.InitGenesisCommitments(ctx, genState.Commitments); err != nil {
		panic(fmt.Errorf("failed to initialize privacy commitments: %w", err))
	}

	if err := k.InitGenesisHistoricalRoots(ctx, genState.HistoricalRoots); err != nil {
		panic(fmt.Errorf("failed to initialize privacy historical roots: %w", err))
	}

	if err := k.InitGenesisNullifiers(ctx, genState.Nullifiers); err != nil {
		panic(fmt.Errorf("failed to initialize privacy nullifiers: %w", err))
	}

	if len(genState.AuditMasterPubkey) != 0 {
		k.SetAuditMasterPubkey(ctx, genState.AuditMasterPubkey)
	}
}

// ExportGenesis returns the module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()

	commitments, err := k.ExportGenesisCommitments(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy commitments: %w", err))
	}

	historicalRoots, err := k.ExportGenesisHistoricalRoots(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy historical roots: %w", err))
	}

	nullifiers, err := k.ExportGenesisNullifiers(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy nullifiers: %w", err))
	}

	genesis.Commitments = commitments
	genesis.HistoricalRoots = historicalRoots
	genesis.Nullifiers = nullifiers
	genesis.AuditMasterPubkey = k.GetAuditMasterPubkey(ctx)

	return genesis
}
