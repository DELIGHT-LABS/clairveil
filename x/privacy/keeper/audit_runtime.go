package keeper

import (
	"crypto/sha256"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	"github.com/cometbft/cometbft/crypto/tmhash"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuditRuntimeConfig prepares the audit-field v2 execution lane. Its artifact
// manifest is explicitly development-grade; it does not authorize production
// keys or initialize state. P5 owns authenticated initialization.
type AuditPrincipalAdapter interface {
	Lock(ctx sdk.Context, funder sdk.AccAddress, coin sdk.Coin) error
	Release(ctx sdk.Context, recipient sdk.AccAddress, coin sdk.Coin) error
}

// The adapter must enforce exact final endpoints, one-shot consumption, and
// no reentrant bank effect. No raw BankKeeper is promoted to this capability.
type AuditRuntimeConfig struct {
	PrincipalAdapter AuditPrincipalAdapter
	ArtifactDir      string
	ExpectedIdentity *types.CircuitSetIdentity
	NetworkNonce     [32]byte
	InitialHeight    uint64
	Authority        string
}
type auditRuntime struct {
	principal     AuditPrincipalAdapter
	registry      *zk.ArtifactRegistry
	identity      *types.CircuitSetIdentity
	nonce         [32]byte
	initialHeight uint64
	authority     string
}

func (k *Keeper) ConfigureAuditRuntime(config AuditRuntimeConfig) error {
	if config.PrincipalAdapter == nil {
		return fmt.Errorf("audit principal adapter is required")
	}
	if k.audit != nil {
		return fmt.Errorf("audit runtime is immutable once configured")
	}
	if config.InitialHeight == 0 || config.InitialHeight > 1<<63-1 {
		return fmt.Errorf("audit initial height is invalid")
	}
	authority, err := sdk.AccAddressFromBech32(config.Authority)
	if err != nil || authority.String() != config.Authority {
		return fmt.Errorf("audit authority must be canonical")
	}
	// Authority must be the actual gov module account, never an arbitrary signer.
	if config.Authority != authtypes.NewModuleAddress(govtypes.ModuleName).String() {
		return fmt.Errorf("audit authority must equal gov module address")
	}
	registry, err := zk.NewArtifactRegistry(zk.ArtifactRegistryConfig{ArtifactDir: config.ArtifactDir, CircuitSetID: zk.AuditFieldCircuitSetID, RuntimeEnvironment: zk.ZKRuntimeEnvironmentDevelopment})
	if err != nil {
		return err
	}
	identity := types.CloneCircuitSetIdentity(config.ExpectedIdentity)
	if err := registry.CheckReadiness(zk.ArtifactRoleValidator, zk.AuditFieldCircuitIDs(), identity); err != nil {
		return err
	}
	k.audit = &auditRuntime{principal: config.PrincipalAdapter, registry: registry, identity: identity, nonce: config.NetworkNonce, initialHeight: config.InitialHeight, authority: config.Authority}
	return nil
}
func (k Keeper) AuditRuntimeEnabled() bool { return k.audit != nil }

// allowsGenesisStateImport is true only for the app's scoped, atomic genesis
// context. It does not open raw mutation methods to transactions.
func (k Keeper) allowsGenesisStateImport(ctx sdk.Context) bool {
	return k.audit == nil || auditinit.Active(ctx)
}
func (k Keeper) AuditInitialHeight() uint64 {
	if k.audit == nil {
		return 0
	}
	return k.audit.initialHeight
}
func (k Keeper) auditNetwork(ctx sdk.Context) ([32]byte, error) {
	if k.audit == nil {
		return [32]byte{}, fmt.Errorf("audit-field runtime is not configured")
	}
	return auditfield.NetworkDigest(ctx.ChainID(), k.audit.nonce)
}
func (k Keeper) auditExecutionOrigin(ctx sdk.Context) (auditOrigin, error) {
	if auditinit.Active(ctx) || k.audit == nil || ctx.BlockHeight() < int64(k.audit.initialHeight) {
		return auditOrigin{}, fmt.Errorf("privacy transition is forbidden during initialization")
	}
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return auditOrigin{}, fmt.Errorf("privacy apply requires delivery context")
	}
	o := auditOrigin{Height: uint64(ctx.BlockHeight())}
	if tx := ctx.TxBytes(); len(tx) > 0 {
		o.Kind = 1
		copy(o.Anchor[:], tmhash.Sum(tx))
	} else {
		hash := ctx.HeaderHash()
		if len(hash) != 32 {
			return auditOrigin{}, fmt.Errorf("trusted block hash is required")
		}
		o.Kind = 2
		copy(o.Anchor[:], hash)
	}
	return o, nil
}

// The apply marker belongs to one execution context, never the keeper singleton.
type auditApplyContextKey struct{}

// auditAssetSimulationContextKey is private to the trusted host API.
type auditAssetSimulationContextKey struct{}

// WithAuditAssetSimulation marks an already isolated, disposable host context
// for real asset verification and execution without original transaction bytes.
// It only adds a private marker: SDK flags, execution mode, stores, gas meters,
// and events are preserved. It provides no isolation, rollback, or persistence
// guarantee. The host must discard all state and events, and must never mark
// real delivery or ordinary CheckTx/ReCheckTx contexts. Internal asset publish
// remains visible to later Privacy calls and policy checks in this simulation.
// The temporary origin anchor is not a transaction hash and must never be
// published as persistent provenance. Governance/key lifecycle is unaffected.
func (k Keeper) WithAuditAssetSimulation(ctx sdk.Context) sdk.Context {
	return ctx.WithValue(auditAssetSimulationContextKey{}, true)
}

func isAuditAssetSimulation(ctx sdk.Context) bool {
	marked, _ := ctx.Value(auditAssetSimulationContextKey{}).(bool)
	return marked
}

// auditAssetExecutionOrigin is used only by asset build and apply.
func (k Keeper) auditAssetExecutionOrigin(ctx sdk.Context) (auditOrigin, error) {
	if !isAuditAssetSimulation(ctx) {
		return k.auditExecutionOrigin(ctx)
	}
	if auditinit.Active(ctx) || k.audit == nil || ctx.BlockHeight() < int64(k.audit.initialHeight) {
		return auditOrigin{}, fmt.Errorf("privacy transition is forbidden during initialization")
	}
	return auditOrigin{
		Kind: 1, Height: uint64(ctx.BlockHeight()),
		Anchor: sha256.Sum256([]byte("clairveil/audit-asset-simulation/v1")),
	}, nil
}
