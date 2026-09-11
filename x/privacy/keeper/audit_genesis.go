package keeper

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"sort"
)

func (k Keeper) freshEntries(ctx sdk.Context, g types.GenesisStateV4, anchor [32]byte) (map[string][]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	if k.audit == nil || !bytes.Equal(g.NetworkNonce, k.audit.nonce[:]) || g.InitialHeight != k.audit.initialHeight || !bytes.Equal(mustMarshalCircuitIdentity(g.CircuitSetIdentity), mustMarshalCircuitIdentity(k.audit.identity)) {
		return nil, fmt.Errorf("fresh network identity mismatch")
	}
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return nil, err
	}
	height := k.audit.initialHeight - 1
	key := g.InitialAuditKey
	var id [32]byte
	copy(id[:], key.KeyID)
	record := &auditKeyRecord{Epoch: key.Epoch, KeyID: id, Suite: key.Suite, PK: key.PublicKey, PoP: key.PoP, ActivationHeight: height, RegisteredHeight: height, Origin: auditOrigin{Kind: 3, Anchor: anchor, Height: height}}
	raw, err := encodeAuditKey(record, network, k.audit.initialHeight)
	if err != nil {
		return nil, err
	}
	identity, err := k.audit.identity.Marshal()
	if err != nil {
		return nil, err
	}
	entries := map[string][]byte{string(types.GetCircuitIdentityKey()): identity, "LeafCount": make([]byte, 8), string(types.GetMerkleNodeKey(MerkleDepth, 0)): emptyNodeBytes(MerkleDepth), string([]byte{0x1b}): anchor[:], string(auditEpochKey(auditEpochPrefix, key.Epoch)): raw}
	active := make([]byte, 18)
	binary.BigEndian.PutUint16(active, 1)
	binary.BigEndian.PutUint64(active[2:], key.Epoch)
	binary.BigEndian.PutUint64(active[10:], height)
	entries[string([]byte{auditActivePrefix})] = active
	// The initial epoch is already active. Prefix 0x17 is the pending activation
	// queue consumed by BeginAuditBlock, not immutable activation history.
	for _, e := range g.AssetRegistry {
		entries[string(types.GetAssetByDenomKey(e.CanonicalDenom))] = e.AssetId
		entries[string(types.GetAssetByIDKey(e.AssetId))] = []byte(e.CanonicalDenom)
	}
	return entries, nil
}

func mustMarshalCircuitIdentity(identity *types.CircuitSetIdentity) []byte {
	if identity == nil {
		return nil
	}
	encoded, err := identity.Marshal()
	if err != nil {
		return nil
	}
	return encoded
}
func (k Keeper) InitializeFreshAudit(ctx sdk.Context, g types.GenesisStateV4, anchor [32]byte) error {
	if !auditinit.Active(ctx) {
		return fmt.Errorf("fresh privacy initialization requires trusted init context")
	}
	entries, err := k.freshEntries(ctx, g, anchor)
	if err != nil {
		return err
	}
	s := k.storeService.OpenKVStore(ctx)
	it, err := s.Iterator(nil, nil)
	if err != nil {
		return err
	}
	nonempty := it.Valid()
	it.Close()
	if nonempty {
		return fmt.Errorf("fresh privacy store must be empty")
	}
	// Canonical order is explicit even though final IAVL cache writes are sorted.
	for _, key := range sortedAuditKeys(entries) {
		if err := s.Set([]byte(key), entries[key]); err != nil {
			return err
		}
	}
	return nil
}

// InitializeAuditMetadata installs only the V4 key/identity metadata around
// a normal exported privacy state. It deliberately does not recreate a
// transition ledger or scan index; those are restored by the ordinary privacy
// genesis importer.
func (k Keeper) InitializeAuditMetadata(ctx sdk.Context, g types.GenesisStateV4, anchor [32]byte) error {
	if !auditinit.Active(ctx) {
		return fmt.Errorf("audit metadata initialization requires trusted init context")
	}
	if g.State == nil {
		return fmt.Errorf("exported privacy state is required")
	}
	if err := g.Validate(); err != nil {
		return err
	}
	identity, found, err := k.GetCircuitSetIdentity(ctx)
	if err != nil || !found || !bytes.Equal(mustMarshalCircuitIdentity(identity), mustMarshalCircuitIdentity(g.CircuitSetIdentity)) {
		return fmt.Errorf("exported privacy circuit identity mismatch")
	}
	entries, err := k.auditMetadataEntriesFromGenesis(ctx, g, anchor)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	for _, prefix := range []byte{0x1b, auditEpochPrefix, auditActivationPrefix, auditActivePrefix, auditCancellationPrefix, auditHaltPrefix} {
		end := []byte{prefix + 1}
		it, err := store.Iterator([]byte{prefix}, end)
		if err != nil {
			return err
		}
		nonempty := it.Valid()
		closeErr := it.Close()
		if closeErr != nil {
			return closeErr
		}
		if nonempty {
			return fmt.Errorf("audit metadata already initialized")
		}
	}
	for _, key := range sortedAuditKeys(entries) {
		if err := store.Set([]byte(key), entries[key]); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) ExportAuditGenesisMetadata(ctx sdk.Context) (types.GenesisStateV4, error) {
	if k.audit == nil {
		return types.GenesisStateV4{}, fmt.Errorf("audit runtime is not configured")
	}
	key, _, err := k.getAuditKey(ctx, 1)
	if err != nil {
		return types.GenesisStateV4{}, err
	}
	identity, found, err := k.GetCircuitSetIdentity(ctx)
	if err != nil || !found {
		return types.GenesisStateV4{}, fmt.Errorf("audit circuit identity is unavailable")
	}
	assets, err := k.ExportGenesisAssetRegistryV1(ctx)
	if err != nil {
		return types.GenesisStateV4{}, err
	}
	management, err := k.exportAuditManagementState(ctx)
	if err != nil {
		return types.GenesisStateV4{}, err
	}
	return types.GenesisStateV4{
		FormatVersion:      4,
		Mode:               "FRESH",
		NetworkNonce:       append([]byte(nil), k.audit.nonce[:]...),
		InitialHeight:      k.audit.initialHeight,
		RestartHeight:      uint64(ctx.BlockHeight() + 1),
		InitialAuditKey:    types.InitialAuditKeyV4{Epoch: key.Epoch, KeyID: append([]byte(nil), key.KeyID[:]...), Suite: key.Suite, PublicKey: append([]byte(nil), key.PK...), PoP: append([]byte(nil), key.PoP...)},
		CircuitSetIdentity: identity,
		AssetRegistry:      assets,
		AuditKeyHistory:    management.AuditKeyHistory,
		ActiveAuditEpoch:   management.ActiveAuditEpoch,
		PendingAuditKey:    management.PendingAuditKey,
		AuditCancellations: management.AuditCancellations,
		PrivacyHalted:      management.PrivacyHalted,
		TreeCapacity:       1 << 32,
	}, nil
}
func (k Keeper) ValidateFreshAudit(ctx sdk.Context, g types.GenesisStateV4, anchor [32]byte) error {
	entries, err := k.freshEntries(ctx, g, anchor)
	if err != nil {
		return err
	}
	it, err := k.storeService.OpenKVStore(ctx).Iterator(nil, nil)
	if err != nil {
		return err
	}
	defer it.Close()
	seen := 0
	for ; it.Valid(); it.Next() {
		expected, ok := entries[string(it.Key())]
		if !ok || !bytes.Equal(expected, it.Value()) {
			return fmt.Errorf("fresh privacy state was modified during module initialization: %x", it.Key())
		}
		seen++
	}
	if seen != len(entries) {
		return fmt.Errorf("fresh privacy initialization missing keys")
	}
	return nil
}

type auditManagementGenesis struct {
	AuditKeyHistory    []*types.AuditKeyHistoryV4
	ActiveAuditEpoch   uint64
	PendingAuditKey    *types.AuditKeyActivationV4
	AuditCancellations []*types.AuditKeyCancellationV4
	PrivacyHalted      bool
}

func auditOriginToGenesis(origin auditOrigin) *types.AuditOriginV4 {
	return &types.AuditOriginV4{Kind: origin.Kind, Anchor: append([]byte(nil), origin.Anchor[:]...), Height: origin.Height}
}

func auditOriginFromGenesis(origin *types.AuditOriginV4) (auditOrigin, error) {
	if origin == nil || len(origin.Anchor) != 32 {
		return auditOrigin{}, fmt.Errorf("invalid audit genesis origin")
	}
	var anchor [32]byte
	copy(anchor[:], origin.Anchor)
	result := auditOrigin{Kind: origin.Kind, Anchor: anchor, Height: origin.Height}
	if err := result.validate(true); err != nil {
		return auditOrigin{}, err
	}
	return result, nil
}

func auditKeyToGenesis(record *auditKeyRecord) *types.AuditKeyHistoryV4 {
	return &types.AuditKeyHistoryV4{Epoch: record.Epoch, KeyID: append([]byte(nil), record.KeyID[:]...), Suite: record.Suite, PublicKey: append([]byte(nil), record.PK...), PoP: append([]byte(nil), record.PoP...), ActivationHeight: record.ActivationHeight, RegisteredHeight: record.RegisteredHeight, Origin: auditOriginToGenesis(record.Origin)}
}

func auditKeyFromGenesis(key *types.AuditKeyHistoryV4) (*auditKeyRecord, error) {
	if key == nil || len(key.KeyID) != 32 {
		return nil, fmt.Errorf("invalid audit genesis key")
	}
	origin, err := auditOriginFromGenesis(key.Origin)
	if err != nil {
		return nil, err
	}
	var id [32]byte
	copy(id[:], key.KeyID)
	return &auditKeyRecord{Epoch: key.Epoch, KeyID: id, Suite: key.Suite, PK: append([]byte(nil), key.PublicKey...), PoP: append([]byte(nil), key.PoP...), ActivationHeight: key.ActivationHeight, RegisteredHeight: key.RegisteredHeight, Origin: origin}, nil
}

func decodeAuditCancellation(raw []byte) (auditOrigin, uint64, error) {
	r := &auditDecoder{raw: raw}
	if r.u16() != 1 {
		return auditOrigin{}, 0, fmt.Errorf("unsupported audit cancellation version")
	}
	origin := r.origin()
	height := r.u64()
	if err := r.done(); err != nil {
		return auditOrigin{}, 0, err
	}
	if err := origin.validate(false); err != nil || origin.Height != height {
		return auditOrigin{}, 0, fmt.Errorf("invalid audit cancellation")
	}
	return origin, height, nil
}

func encodeAuditCancellation(origin auditOrigin, height uint64) ([]byte, error) {
	if err := origin.validate(false); err != nil || origin.Height != height {
		return nil, fmt.Errorf("invalid audit cancellation")
	}
	w := &auditEncoder{}
	w.u16(1)
	w.origin(origin)
	w.u64(height)
	return w.Bytes(), nil
}

func (k Keeper) exportAuditManagementState(ctx sdk.Context) (auditManagementGenesis, error) {
	result := auditManagementGenesis{}
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return result, err
	}
	store := k.storeService.OpenKVStore(ctx)
	keys, err := store.Iterator([]byte{auditEpochPrefix}, []byte{auditEpochPrefix + 1})
	if err != nil {
		return result, err
	}
	defer keys.Close()
	for expected := uint64(1); keys.Valid(); keys.Next() {
		if len(keys.Key()) != 9 || binary.BigEndian.Uint64(keys.Key()[1:]) != expected {
			return result, fmt.Errorf("noncontiguous audit key history")
		}
		record, err := decodeAuditKey(keys.Value(), network, k.audit.initialHeight)
		if err != nil {
			return result, err
		}
		result.AuditKeyHistory = append(result.AuditKeyHistory, auditKeyToGenesis(record))
		expected++
	}
	active, err := k.activeAuditKey(ctx)
	if err != nil {
		return result, err
	}
	result.ActiveAuditEpoch = active.Epoch
	pendingEpoch, pendingHeight, err := k.pendingAuditEpoch(ctx)
	if err != nil {
		return result, err
	}
	if pendingEpoch != 0 {
		result.PendingAuditKey = &types.AuditKeyActivationV4{Epoch: pendingEpoch, ActivationHeight: pendingHeight}
	}
	cancellations, err := store.Iterator([]byte{auditCancellationPrefix}, []byte{auditCancellationPrefix + 1})
	if err != nil {
		return result, err
	}
	defer cancellations.Close()
	for ; cancellations.Valid(); cancellations.Next() {
		if len(cancellations.Key()) != 9 {
			return result, fmt.Errorf("invalid audit cancellation key")
		}
		origin, height, err := decodeAuditCancellation(cancellations.Value())
		if err != nil {
			return result, err
		}
		result.AuditCancellations = append(result.AuditCancellations, &types.AuditKeyCancellationV4{Epoch: binary.BigEndian.Uint64(cancellations.Key()[1:]), Origin: auditOriginToGenesis(origin), CancelledHeight: height})
	}
	result.PrivacyHalted, err = k.auditHalted(ctx)
	return result, err
}

func (k Keeper) auditMetadataEntriesFromGenesis(ctx sdk.Context, g types.GenesisStateV4, anchor [32]byte) (map[string][]byte, error) {
	if len(g.AuditKeyHistory) == 0 || g.ActiveAuditEpoch == 0 {
		return nil, fmt.Errorf("exported audit management state is required")
	}
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return nil, err
	}
	entries := map[string][]byte{string([]byte{0x1b}): append([]byte(nil), anchor[:]...)}
	seen := make(map[uint64]*auditKeyRecord, len(g.AuditKeyHistory))
	for expected, history := range g.AuditKeyHistory {
		record, err := auditKeyFromGenesis(history)
		if err != nil || record.Epoch != uint64(expected+1) {
			return nil, fmt.Errorf("invalid exported audit key history")
		}
		if record.Epoch == 1 && (record.Origin.Kind != 3 || record.Origin.Anchor != anchor) {
			return nil, fmt.Errorf("exported initial audit key provenance mismatch")
		}
		raw, err := encodeAuditKey(record, network, k.audit.initialHeight)
		if err != nil {
			return nil, err
		}
		entries[string(auditEpochKey(auditEpochPrefix, record.Epoch))] = raw
		seen[record.Epoch] = record
	}
	initial := seen[1]
	if initial == nil || initial.Epoch != g.InitialAuditKey.Epoch || initial.KeyID != [32]byte(g.InitialAuditKey.KeyID) || initial.Suite != g.InitialAuditKey.Suite || !bytes.Equal(initial.PK, g.InitialAuditKey.PublicKey) || !bytes.Equal(initial.PoP, g.InitialAuditKey.PoP) {
		return nil, fmt.Errorf("exported initial audit key differs from history")
	}
	active := seen[g.ActiveAuditEpoch]
	if active == nil || active.ActivationHeight > uint64(ctx.BlockHeight()) {
		return nil, fmt.Errorf("invalid exported active audit key")
	}
	activeRaw := make([]byte, 18)
	binary.BigEndian.PutUint16(activeRaw, 1)
	binary.BigEndian.PutUint64(activeRaw[2:], active.Epoch)
	binary.BigEndian.PutUint64(activeRaw[10:], active.ActivationHeight)
	entries[string([]byte{auditActivePrefix})] = activeRaw
	if pending := g.PendingAuditKey; pending != nil {
		record := seen[pending.Epoch]
		if record == nil || pending.ActivationHeight != record.ActivationHeight || pending.ActivationHeight <= uint64(ctx.BlockHeight()) {
			return nil, fmt.Errorf("invalid exported pending audit activation")
		}
		var epoch [8]byte
		binary.BigEndian.PutUint64(epoch[:], pending.Epoch)
		entries[string(auditEpochKey(auditActivationPrefix, pending.ActivationHeight))] = epoch[:]
	}
	for _, cancellation := range g.AuditCancellations {
		if cancellation == nil || seen[cancellation.Epoch] == nil {
			return nil, fmt.Errorf("invalid exported audit cancellation")
		}
		origin, err := auditOriginFromGenesis(cancellation.Origin)
		if err != nil {
			return nil, err
		}
		raw, err := encodeAuditCancellation(origin, cancellation.CancelledHeight)
		if err != nil {
			return nil, err
		}
		key := string(auditEpochKey(auditCancellationPrefix, cancellation.Epoch))
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("duplicate exported audit cancellation")
		}
		entries[key] = raw
	}
	halt := byte(0)
	if g.PrivacyHalted {
		halt = 1
	}
	entries[string([]byte{auditHaltPrefix})] = []byte{halt}
	return entries, nil
}

func sortedAuditKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
