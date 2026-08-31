package keeper

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sort"

	corestore "cosmossdk.io/core/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const MaxCommitmentPathSnapshotQuery = 16

// MaxHistoricalPathQueryRebuildLeaves is deliberately much smaller than the
// offline MaxMerkleRebuildLeaves recovery bound. A public query must not turn
// one small request into an unbounded KV scan and MiMC rebuild.
const MaxHistoricalPathQueryRebuildLeaves = uint64(1) << 10

const MaxConcurrentHistoricalPathQueryRebuilds = 2

const historicalPathCancellationCheckInterval = uint64(256)

func validateHistoricalPathQueryRebuildCount(count uint64) error {
	if err := validateMerkleLeafCount(count); err != nil {
		return err
	}
	if count > MaxHistoricalPathQueryRebuildLeaves {
		return fmt.Errorf("%w: leaf_count=%d max_query_rebuild_leaves=%d", errHistoricalPathQueryTooLarge, count, MaxHistoricalPathQueryRebuildLeaves)
	}
	return nil
}

func checkHistoricalPathQueryContext(ctx sdk.Context, work uint64) error {
	if work%historicalPathCancellationCheckInterval != 0 {
		return nil
	}
	return ctx.Context().Err()
}

func (k Keeper) acquireHistoricalPathQueryRebuild(ctx sdk.Context) (func(), error) {
	if k.historicalPathQueryRebuildSlots == nil {
		return nil, fmt.Errorf("%w: admission is not initialized", errHistoricalPathQueryBusy)
	}
	select {
	case k.historicalPathQueryRebuildSlots <- struct{}{}:
		return func() { <-k.historicalPathQueryRebuildSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, errHistoricalPathQueryBusy
	}
}

func (k Keeper) getStoredIncrementalPathV1(ctx sdk.Context, commitment []byte, count uint64) ([]string, []uint32, uint64, error) {
	canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
	if err != nil {
		return nil, nil, 0, err
	}
	leafIndex, found, err := k.GetCommitmentIndex(ctx, canonicalCommitment)
	if err != nil {
		return nil, nil, 0, err
	}
	if !found || leafIndex >= count {
		return nil, nil, 0, errMerkleCommitmentNotFound
	}
	leaf, err := k.getLeafRequired(ctx, leafIndex, count)
	if err != nil {
		return nil, nil, 0, err
	}
	if !bytes.Equal(leaf, canonicalCommitment) {
		return nil, nil, 0, fmt.Errorf("%w: index=%d leaf_count=%d", errMerkleTreeLeafMismatch, leafIndex, count)
	}

	path := make([]string, MerkleDepth)
	helper := make([]uint32, MerkleDepth)
	currentIndex := leafIndex
	for level := uint32(0); level < MerkleDepth; level++ {
		if err := checkHistoricalPathQueryContext(ctx, uint64(level)); err != nil {
			return nil, nil, 0, err
		}
		sibling, err := k.getMerkleNodeOrEmpty(ctx, uint8(level), currentIndex^1, count)
		if err != nil {
			return nil, nil, 0, err
		}
		path[level] = fmt.Sprintf("%x", sibling)
		helper[level] = uint32(currentIndex % 2)
		currentIndex /= 2
	}
	return path, helper, leafIndex, nil
}

func canonicalFieldBytesFromBigInt(value *big.Int) []byte {
	out := make([]byte, fieldElementByteSize)
	if value != nil {
		value.FillBytes(out)
	}
	return out
}

// computeCommitmentPrefixRootsV1 returns one exact NoteV1 tree root per stored
// commitment prefix. It uses the shared NoteV1 parent and empty-root helpers,
// never a query-local Merkle formula.
func (k Keeper) computeCommitmentPrefixRootsV1(ctx sdk.Context, count uint64) ([][]byte, error) {
	if err := validateMerkleRebuildCount(count); err != nil {
		return nil, err
	}
	if count == 0 {
		return [][]byte{}, nil
	}

	emptyRoots := types.EmptyNoteTreeRootsV1(MerkleDepth)
	frontier := make([]*big.Int, MerkleDepth)
	roots := make([][]byte, 0, count)
	for leafIndex := uint64(0); leafIndex < count; leafIndex++ {
		leaf, err := k.getLeafRequired(ctx, leafIndex, count)
		if err != nil {
			return nil, err
		}
		current := new(big.Int).SetBytes(leaf)
		currentIndex := leafIndex
		for level := uint32(0); level < MerkleDepth; level++ {
			if currentIndex%2 == 0 {
				frontier[level] = new(big.Int).Set(current)
				current = types.ComputeNoteTreeNodeV1(level, current, emptyRoots[level])
			} else {
				if frontier[level] == nil {
					return nil, fmt.Errorf("merkle prefix frontier is missing at level %d", level)
				}
				current = types.ComputeNoteTreeNodeV1(level, frontier[level], current)
			}
			currentIndex /= 2
		}
		roots = append(roots, canonicalFieldBytesFromBigInt(current))
	}
	return roots, nil
}

func (k Keeper) SetMerkleRootSnapshotV1(ctx sdk.Context, snapshot *types.MerkleRootSnapshotV1) error {
	if snapshot == nil {
		return fmt.Errorf("merkle root snapshot is required")
	}
	canonicalRoot, err := validateFieldElementBytesStrict(snapshot.Root)
	if err != nil {
		return fmt.Errorf("merkle root snapshot root is invalid: %w", err)
	}
	if snapshot.LeafCount == 0 || snapshot.LeafCount > k.GetLeafCount(ctx) {
		return fmt.Errorf("merkle root snapshot leaf_count is out of range")
	}
	if snapshot.Height < 0 {
		return fmt.Errorf("merkle root snapshot height must not be negative")
	}
	if !k.CheckHistoricalRoot(ctx, canonicalRoot) {
		return fmt.Errorf("merkle root snapshot root is not historical")
	}

	copySnapshot := *snapshot
	copySnapshot.Root = append([]byte(nil), canonicalRoot...)
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetMerkleRootSnapshotKey(canonicalRoot)
	existing, err := store.Get(key)
	if err != nil {
		return err
	}
	if len(existing) != 0 {
		var current types.MerkleRootSnapshotV1
		if err := current.Unmarshal(existing); err != nil {
			return fmt.Errorf("stored merkle root snapshot is corrupt: %w", err)
		}
		if current.LeafCount != copySnapshot.LeafCount || current.Height != copySnapshot.Height || !bytes.Equal(current.Root, copySnapshot.Root) {
			return fmt.Errorf("merkle root snapshot re-registration is inconsistent")
		}
		return setHistoricalRootHeightMetadata(store, canonicalRoot, copySnapshot.Height)
	}
	encoded, err := copySnapshot.Marshal()
	if err != nil {
		return fmt.Errorf("encode merkle root snapshot: %w", err)
	}
	if err := store.Set(key, encoded); err != nil {
		return err
	}
	return setHistoricalRootHeightMetadata(store, canonicalRoot, copySnapshot.Height)
}

func setHistoricalRootHeightMetadata(store corestore.KVStore, root []byte, height int64) error {
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, uint64(height))
	return store.Set(types.GetHistoricalRootKey(root), heightBytes)
}

func (k Keeper) GetMerkleRootSnapshotV1(ctx sdk.Context, root []byte) (*types.MerkleRootSnapshotV1, bool, error) {
	canonicalRoot, err := validateFieldElementBytesStrict(root)
	if err != nil {
		return nil, false, err
	}
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetMerkleRootSnapshotKey(canonicalRoot)
	bz, err := store.Get(key)
	if err != nil {
		return nil, false, err
	}
	if len(bz) == 0 {
		// Every accepted commitment prefix persists immutable snapshot metadata.
		// Query-time derivation would require an attacker-controlled full prefix
		// scan, so missing metadata fails closed and is repaired only through the
		// explicit offline rebuild path.
		return nil, false, nil
	}

	var snapshot types.MerkleRootSnapshotV1
	if err := snapshot.Unmarshal(bz); err != nil {
		return nil, false, fmt.Errorf("stored merkle root snapshot is corrupt: %w", err)
	}
	if !bytes.Equal(snapshot.Root, canonicalRoot) || snapshot.LeafCount == 0 || snapshot.LeafCount > k.GetLeafCount(ctx) || snapshot.Height < 0 {
		return nil, false, fmt.Errorf("stored merkle root snapshot is inconsistent")
	}
	heightBytes, err := store.Get(types.GetHistoricalRootKey(canonicalRoot))
	if err != nil {
		return nil, false, err
	}
	if len(heightBytes) != 8 {
		return nil, false, fmt.Errorf("stored merkle root snapshot historical height is missing or corrupt")
	}
	height := binary.BigEndian.Uint64(heightBytes)
	if height > math.MaxInt64 || int64(height) != snapshot.Height {
		return nil, false, fmt.Errorf("stored merkle root snapshot historical height is inconsistent")
	}
	return &snapshot, true, nil
}

// RecordCurrentMerkleRootSnapshotV1 idempotently confirms the post-operation
// root. AppendCommitment already persists every prefix snapshot, including the
// block height at which that root was created. Output-less operations such as a
// withdraw must therefore preserve an existing snapshot rather than trying to
// register the same root again at the operation's later block height. Bounded
// deterministic rebuild remains only a fail-closed recovery/query path.
func (k Keeper) RecordCurrentMerkleRootSnapshotV1(ctx sdk.Context) error {
	count := k.GetLeafCount(ctx)
	if count == 0 {
		return nil
	}
	root := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
	if len(root) == 0 {
		var err error
		root, err = k.RecalculateRoot(ctx, count)
		if err != nil {
			return err
		}
	}
	current, found, err := k.GetMerkleRootSnapshotV1(ctx, root)
	if err != nil {
		return fmt.Errorf("load current merkle root snapshot: %w", err)
	}
	if found {
		if current.LeafCount != count {
			return fmt.Errorf("current merkle root snapshot leaf_count is inconsistent: got %d want %d", current.LeafCount, count)
		}
		return nil
	}
	return k.SetMerkleRootSnapshotV1(ctx, &types.MerkleRootSnapshotV1{
		Root:      root,
		LeafCount: count,
		Height:    ctx.BlockHeight(),
	})
}

func (k Keeper) RebuildMerkleRootSnapshotIndexV1(ctx sdk.Context) error {
	count := k.GetLeafCount(ctx)
	roots, err := k.computeCommitmentPrefixRootsV1(ctx, count)
	if err != nil {
		return fmt.Errorf("rebuild merkle root snapshots: %w", err)
	}
	store := k.storeService.OpenKVStore(ctx)
	for i, root := range roots {
		historicalValue, err := store.Get(types.GetHistoricalRootKey(root))
		if err != nil {
			return err
		}
		if len(historicalValue) != 8 {
			return fmt.Errorf("historical root %x has invalid height metadata", root)
		}
		heightValue := binary.BigEndian.Uint64(historicalValue)
		if heightValue > math.MaxInt64 {
			return fmt.Errorf("historical root %x height overflows int64", root)
		}
		if err := k.SetMerkleRootSnapshotV1(ctx, &types.MerkleRootSnapshotV1{
			Root:      root,
			LeafCount: uint64(i + 1),
			Height:    int64(heightValue),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) InitGenesisMerkleRootSnapshotsV1(ctx sdk.Context, snapshots []*types.MerkleRootSnapshotV1) error {
	count := k.GetLeafCount(ctx)
	if uint64(len(snapshots)) != count {
		return fmt.Errorf("genesis merkle root snapshots must contain every commitment prefix: got %d want %d", len(snapshots), count)
	}
	// InitGenesisCommitments persists one authoritative root/count snapshot per
	// append even for trees larger than the bounded historical rebuild limit.
	// Validate the imported metadata against that append-built index, then
	// replace only the historical heights from the exported genesis.
	_, expected, err := k.loadStoredMerkleRootSnapshotsV1(ctx, count)
	if err != nil {
		return err
	}
	if uint64(len(expected)) != count {
		return fmt.Errorf("append-built merkle root snapshot index is incomplete: got %d want %d", len(expected), count)
	}
	seen := make(map[string]struct{}, len(snapshots))
	store := k.storeService.OpenKVStore(ctx)
	for i, snapshot := range snapshots {
		if snapshot == nil {
			return fmt.Errorf("genesis merkle root snapshot %d is nil", i)
		}
		canonicalRoot, err := validateFieldElementBytesStrict(snapshot.Root)
		if err != nil {
			return fmt.Errorf("genesis merkle root snapshot %d is invalid: %w", i, err)
		}
		expectedSnapshot, found := expected[string(canonicalRoot)]
		if !found || snapshot.LeafCount != expectedSnapshot.LeafCount {
			return fmt.Errorf("genesis merkle root snapshot %d does not match commitment prefix", i)
		}
		if _, duplicate := seen[string(canonicalRoot)]; duplicate {
			return fmt.Errorf("genesis merkle root snapshot %d duplicates a root", i)
		}
		seen[string(canonicalRoot)] = struct{}{}
		copySnapshot := *snapshot
		copySnapshot.Root = append([]byte(nil), canonicalRoot...)
		encoded, err := copySnapshot.Marshal()
		if err != nil {
			return fmt.Errorf("encode genesis merkle root snapshot %d: %w", i, err)
		}
		if err := store.Set(types.GetMerkleRootSnapshotKey(canonicalRoot), encoded); err != nil {
			return fmt.Errorf("initialize genesis merkle root snapshot %d: %w", i, err)
		}
		if err := setHistoricalRootHeightMetadata(store, canonicalRoot, copySnapshot.Height); err != nil {
			return fmt.Errorf("initialize genesis historical root height %d: %w", i, err)
		}
	}
	return nil
}

func (k Keeper) ExportGenesisMerkleRootSnapshotsV1(ctx sdk.Context) ([]*types.MerkleRootSnapshotV1, error) {
	count := k.GetLeafCount(ctx)
	snapshots, stored, err := k.loadStoredMerkleRootSnapshotsV1(ctx, count)
	if err != nil {
		return nil, err
	}
	if count <= MaxMerkleRebuildLeaves {
		roots, err := k.computeCommitmentPrefixRootsV1(ctx, count)
		if err != nil {
			return nil, err
		}
		store := k.storeService.OpenKVStore(ctx)
		rebuilt := make([]*types.MerkleRootSnapshotV1, 0, count)
		for i, root := range roots {
			heightBytes, err := store.Get(types.GetHistoricalRootKey(root))
			if err != nil {
				return nil, err
			}
			if len(heightBytes) != 8 || binary.BigEndian.Uint64(heightBytes) > math.MaxInt64 {
				return nil, fmt.Errorf("historical root %x has invalid height metadata", root)
			}
			want := &types.MerkleRootSnapshotV1{Root: append([]byte(nil), root...), LeafCount: uint64(i + 1), Height: int64(binary.BigEndian.Uint64(heightBytes))}
			if cached, found := stored[string(root)]; found && (cached.LeafCount != want.LeafCount || cached.Height != want.Height) {
				return nil, fmt.Errorf("merkle root snapshot index does not match commitment prefixes")
			}
			rebuilt = append(rebuilt, want)
		}
		if len(stored) > len(rebuilt) {
			return nil, fmt.Errorf("merkle root snapshot index contains a non-prefix root")
		}
		sort.Slice(rebuilt, func(i, j int) bool { return bytes.Compare(rebuilt[i].Root, rebuilt[j].Root) < 0 })
		return rebuilt, nil
	}
	if uint64(len(snapshots)) != count {
		return nil, fmt.Errorf("large merkle tree requires persisted snapshot for every commitment prefix: got %d want %d", len(snapshots), count)
	}
	return snapshots, nil
}

func (k Keeper) loadStoredMerkleRootSnapshotsV1(ctx sdk.Context, count uint64) ([]*types.MerkleRootSnapshotV1, map[string]*types.MerkleRootSnapshotV1, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.GetMerkleRootSnapshotPrefix()
	iterator, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, nil, err
	}
	defer iterator.Close()

	snapshots := make([]*types.MerkleRootSnapshotV1, 0, count)
	byRoot := make(map[string]*types.MerkleRootSnapshotV1, count)
	seenCounts := make(map[uint64]struct{}, count)
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != 1+fieldElementByteSize {
			return nil, nil, fmt.Errorf("merkle root snapshot key has invalid length")
		}
		var snapshot types.MerkleRootSnapshotV1
		if err := snapshot.Unmarshal(iterator.Value()); err != nil {
			return nil, nil, fmt.Errorf("merkle root snapshot is corrupt: %w", err)
		}
		if !bytes.Equal(key[1:], snapshot.Root) || snapshot.LeafCount == 0 || snapshot.LeafCount > count || snapshot.Height < 0 {
			return nil, nil, fmt.Errorf("merkle root snapshot key/body metadata is inconsistent")
		}
		if _, duplicate := byRoot[string(snapshot.Root)]; duplicate {
			return nil, nil, fmt.Errorf("merkle root snapshot duplicates a root")
		}
		if _, duplicate := seenCounts[snapshot.LeafCount]; duplicate {
			return nil, nil, fmt.Errorf("merkle root snapshot duplicates leaf_count %d", snapshot.LeafCount)
		}
		heightBytes, err := store.Get(types.GetHistoricalRootKey(snapshot.Root))
		if err != nil {
			return nil, nil, err
		}
		if len(heightBytes) != 8 || binary.BigEndian.Uint64(heightBytes) > math.MaxInt64 || int64(binary.BigEndian.Uint64(heightBytes)) != snapshot.Height {
			return nil, nil, fmt.Errorf("merkle root snapshot historical height is inconsistent")
		}
		copySnapshot := snapshot
		copySnapshot.Root = append([]byte(nil), snapshot.Root...)
		byRoot[string(copySnapshot.Root)] = &copySnapshot
		seenCounts[copySnapshot.LeafCount] = struct{}{}
		snapshots = append(snapshots, &copySnapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool { return bytes.Compare(snapshots[i].Root, snapshots[j].Root) < 0 })
	return snapshots, byRoot, nil
}

// GetCommitmentPathsAtRootV1 returns all paths from one immutable commitment
// prefix. Current-root requests use the incremental provider; historical roots
// use the same NoteV1 helpers in a bounded deterministic rebuild.
func (k Keeper) GetCommitmentPathsAtRootV1(ctx sdk.Context, commitments [][]byte, root []byte, snapshotHeight int64) ([]*types.QueryCommitmentPathAtRoot, *types.MerkleRootSnapshotV1, error) {
	if len(commitments) == 0 || len(commitments) > MaxCommitmentPathSnapshotQuery {
		return nil, nil, fmt.Errorf("commitment path snapshot requires 1..%d commitments", MaxCommitmentPathSnapshotQuery)
	}
	if err := types.ValidateDistinctCanonicalFieldElements("path commitment", commitments); err != nil {
		return nil, nil, err
	}
	canonicalRoot, err := validateFieldElementBytesStrict(root)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot root is invalid: %w", err)
	}
	if err := checkHistoricalPathQueryContext(ctx, 0); err != nil {
		return nil, nil, err
	}
	snapshot, found, err := k.GetMerkleRootSnapshotV1(ctx, canonicalRoot)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, nil, errMerkleCommitmentNotFound
	}
	if snapshotHeight > 0 && snapshot.Height != snapshotHeight {
		return nil, nil, fmt.Errorf("snapshot height does not match root metadata")
	}

	currentCount := k.GetLeafCount(ctx)
	currentRoot := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
	if len(currentRoot) == 0 && currentCount > 0 {
		return nil, nil, fmt.Errorf("%w: leaf_count=%d", errMerkleQueryCachedRootMissing, currentCount)
	}
	if snapshot.LeafCount == currentCount && bytes.Equal(currentRoot, canonicalRoot) {
		paths := make([]*types.QueryCommitmentPathAtRoot, 0, len(commitments))
		for _, commitment := range commitments {
			path, helper, leafIndex, err := k.getStoredIncrementalPathV1(ctx, commitment, currentCount)
			if err != nil {
				return nil, nil, err
			}
			paths = append(paths, &types.QueryCommitmentPathAtRoot{
				CommitmentHex: fmt.Sprintf("%x", commitment),
				LeafIndex:     leafIndex,
				Path:          path,
				PathHelper:    helper,
			})
		}
		return paths, snapshot, nil
	}

	// Enforce the public-query budget before allocating a leaf layer or reading
	// any leaf. The larger MaxMerkleRebuildLeaves bound remains offline-only.
	if err := validateHistoricalPathQueryRebuildCount(snapshot.LeafCount); err != nil {
		return nil, nil, err
	}
	release, err := k.acquireHistoricalPathQueryRebuild(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	layer := make([][]byte, snapshot.LeafCount)
	indices := make([]uint64, len(commitments))
	paths := make([]*types.QueryCommitmentPathAtRoot, len(commitments))
	for i := uint64(0); i < snapshot.LeafCount; i++ {
		if err := checkHistoricalPathQueryContext(ctx, i); err != nil {
			return nil, nil, err
		}
		leaf, err := k.getLeafRequired(ctx, i, snapshot.LeafCount)
		if err != nil {
			return nil, nil, err
		}
		layer[i] = leaf
	}
	for i, commitment := range commitments {
		canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
		if err != nil {
			return nil, nil, err
		}
		leafIndex, found, err := k.GetCommitmentIndex(ctx, canonicalCommitment)
		if err != nil {
			return nil, nil, err
		}
		if !found || leafIndex >= snapshot.LeafCount || !bytes.Equal(layer[leafIndex], canonicalCommitment) {
			return nil, nil, errMerkleCommitmentNotFound
		}
		indices[i] = leafIndex
		paths[i] = &types.QueryCommitmentPathAtRoot{
			CommitmentHex: fmt.Sprintf("%x", canonicalCommitment),
			LeafIndex:     leafIndex,
			Path:          make([]string, 0, MerkleDepth),
			PathHelper:    make([]uint32, 0, MerkleDepth),
		}
	}

	for level := uint32(0); level < MerkleDepth; level++ {
		for i, index := range indices {
			siblingIndex := index ^ 1
			sibling := emptyNodeBytes(level)
			if siblingIndex < uint64(len(layer)) {
				sibling = layer[siblingIndex]
			}
			paths[i].Path = append(paths[i].Path, fmt.Sprintf("%x", sibling))
			paths[i].PathHelper = append(paths[i].PathHelper, uint32(index%2))
			indices[i] /= 2
		}
		next := make([][]byte, (len(layer)+1)/2)
		for i := 0; i < len(layer); i += 2 {
			if err := checkHistoricalPathQueryContext(ctx, uint64(i)); err != nil {
				return nil, nil, err
			}
			right := emptyNodeBytes(level)
			if i+1 < len(layer) {
				right = layer[i+1]
			}
			next[i/2] = hashNodes(level, layer[i], right)
		}
		layer = next
	}
	if len(layer) != 1 || !bytes.Equal(layer[0], canonicalRoot) {
		return nil, nil, fmt.Errorf("historical path snapshot rebuild produced a different root")
	}
	return paths, snapshot, nil
}
