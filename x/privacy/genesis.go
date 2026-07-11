package privacy

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

// InitGenesis initializes the module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	if err := genState.Validate(); err != nil {
		panic(fmt.Errorf("invalid privacy genesis state: %w", err))
	}
	if genState.CircuitSetIdentity == nil {
		panic("invalid privacy genesis state: circuit_set_identity is required")
	}
	if err := zk.ValidateLocalVerifierIdentity(genState.CircuitSetIdentity); err != nil {
		panic(fmt.Errorf("privacy circuit identity preflight failed: %w", err))
	}
	if err := k.SetCircuitSetIdentity(ctx, genState.CircuitSetIdentity); err != nil {
		panic(fmt.Errorf("failed to initialize privacy circuit identity: %w", err))
	}
	if err := k.InitGenesisAssetRegistryV1(ctx, genState.AssetRegistry); err != nil {
		panic(fmt.Errorf("failed to initialize privacy asset registry: %w", err))
	}

	if err := k.InitGenesisCommitments(ctx, genState.Commitments); err != nil {
		panic(fmt.Errorf("failed to initialize privacy commitments: %w", err))
	}

	if err := k.InitGenesisHistoricalRoots(ctx, genState.HistoricalRoots); err != nil {
		panic(fmt.Errorf("failed to initialize privacy historical roots: %w", err))
	}
	if err := k.InitGenesisMerkleRootSnapshotsV1(ctx, genState.MerkleRootSnapshots); err != nil {
		panic(fmt.Errorf("failed to initialize privacy merkle root snapshots: %w", err))
	}

	if err := k.InitGenesisNullifiers(ctx, genState.Nullifiers); err != nil {
		panic(fmt.Errorf("failed to initialize privacy nullifiers: %w", err))
	}
	if err := k.InitGenesisReserveBalancesV1(ctx, genState.ReserveBalances); err != nil {
		panic(fmt.Errorf("failed to initialize privacy reserve balances: %w", err))
	}
	if err := k.InitGenesisPrivacyIndexV2(ctx, genState.PrivacyGlobalSequence, genState.PrivacyEvents, genState.PrivacyScanSummaries, genState.PrivacyScanOutputs); err != nil {
		panic(fmt.Errorf("failed to initialize privacy scan index: %w", err))
	}

	if len(genState.AuditMasterPubkey) != 0 {
		k.SetAuditMasterPubkey(ctx, genState.AuditMasterPubkey)
	}
}

// ExportGenesis returns the module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	identity, found, err := k.GetCircuitSetIdentity(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy circuit identity: %w", err))
	}
	if !found {
		panic("failed to export privacy circuit identity: identity is missing")
	}
	genesis := types.DefaultGenesis(identity)

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
	assetRegistry, err := k.ExportGenesisAssetRegistryV1(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy asset registry: %w", err))
	}
	rootSnapshots, err := k.ExportGenesisMerkleRootSnapshotsV1(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy merkle root snapshots: %w", err))
	}
	reserveBalances, err := k.ExportGenesisReserveBalancesV1(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy reserve balances: %w", err))
	}
	privacyEvents, err := k.ExportGenesisPrivacyEventsV1(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy events: %w", err))
	}
	privacyScanSummaries, privacyScanOutputs, err := k.ExportGenesisPrivacyScanV2(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy scan index: %w", err))
	}
	globalSequence, err := k.GetPrivacyGlobalSequence(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export privacy global sequence: %w", err))
	}

	genesis.Commitments = commitments
	genesis.HistoricalRoots = historicalRoots
	genesis.Nullifiers = nullifiers
	genesis.AuditMasterPubkey = k.GetAuditMasterPubkey(ctx)
	genesis.AssetRegistry = assetRegistry
	genesis.MerkleRootSnapshots = rootSnapshots
	genesis.ReserveBalances = reserveBalances
	genesis.PrivacyEvents = privacyEvents
	genesis.PrivacyScanSummaries = privacyScanSummaries
	genesis.PrivacyScanOutputs = privacyScanOutputs
	genesis.PrivacyGlobalSequence = globalSequence
	if err := genesis.Validate(); err != nil {
		panic(fmt.Errorf("exported privacy genesis state is invalid: %w", err))
	}
	return genesis
}
