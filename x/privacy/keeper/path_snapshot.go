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
		return k.deriveMerkleRootSnapshotV1(ctx, canonicalRoot)
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

// deriveMerkleRootSnapshotV1 is read-only so gRPC queries never populate
// consensus state as a side effect. ExportGenesis may call the explicit
// rebuild writer separately.
func (k Keeper) deriveMerkleRootSnapshotV1(ctx sdk.Context, canonicalRoot []byte) (*types.MerkleRootSnapshotV1, bool, error) {
	count := k.GetLeafCount(ctx)
	if count == 0 {
		return nil, false, nil
	}
	store := k.storeService.OpenKVStore(ctx)
	readHeight := func(root []byte) (int64, error) {
		value, err := store.Get(types.GetHistoricalRootKey(root))
		if err != nil {
			return 0, err
		}
		if len(value) == 0 {
			return 0, errMerkleCommitmentNotFound
		}
		if len(value) != 8 {
			return 0, fmt.Errorf("historical root has invalid height metadata")
		}
		height := binary.BigEndian.Uint64(value)
		if height > math.MaxInt64 {
			return 0, fmt.Errorf("historical root height overflows int64")
		}
		return int64(height), nil
	}

	currentRoot := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
	if len(currentRoot) == 0 {
		var err error
		currentRoot, err = k.RecalculateRoot(ctx, count)
		if err != nil {
			return nil, false, err
		}
	}
	if bytes.Equal(currentRoot, canonicalRoot) {
		height, err := readHeight(canonicalRoot)
		if err != nil {
			return nil, false, err
		}
		return &types.MerkleRootSnapshotV1{Root: append([]byte(nil), canonicalRoot...), LeafCount: count, Height: height}, true, nil
	}

	roots, err := k.computeCommitmentPrefixRootsV1(ctx, count)
	if err != nil {
		return nil, false, err
	}
	for i, root := range roots {
		if !bytes.Equal(root, canonicalRoot) {
			continue
		}
		height, err := readHeight(canonicalRoot)
		if err != nil {
			return nil, false, err
		}
		return &types.MerkleRootSnapshotV1{Root: append([]byte(nil), canonicalRoot...), LeafCount: uint64(i + 1), Height: height}, true, nil
	}
	return nil, false, nil
}

// RecordCurrentMerkleRootSnapshotV1 idempotently confirms the post-operation
// root. AppendCommitment already persists every prefix snapshot; bounded
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
		currentRoot, err = k.RecalculateRoot(ctx, currentCount)
		if err != nil {
			return nil, nil, err
		}
	}
	if snapshot.LeafCount == currentCount && bytes.Equal(currentRoot, canonicalRoot) {
		paths := make([]*types.QueryCommitmentPathAtRoot, 0, len(commitments))
		for _, commitment := range commitments {
			canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
			if err != nil {
				return nil, nil, err
			}
			path, helper, pathRoot, err := k.GetPath(ctx, canonicalCommitment)
			if err != nil {
				return nil, nil, err
			}
			if !bytes.Equal(pathRoot, canonicalRoot) {
				return nil, nil, fmt.Errorf("incremental path provider returned a different root")
			}
			leafIndex, ok, err := k.GetCommitmentIndex(ctx, canonicalCommitment)
			if err != nil {
				return nil, nil, err
			}
			if !ok || leafIndex >= snapshot.LeafCount {
				return nil, nil, errMerkleCommitmentNotFound
			}
			paths = append(paths, &types.QueryCommitmentPathAtRoot{
				CommitmentHex: fmt.Sprintf("%x", canonicalCommitment),
				LeafIndex:     leafIndex,
				Path:          path,
				PathHelper:    helper,
			})
		}
		return paths, snapshot, nil
	}

	if err := validateMerkleRebuildCount(snapshot.LeafCount); err != nil {
		return nil, nil, err
	}
	layer := make([][]byte, snapshot.LeafCount)
	indices := make([]uint64, len(commitments))
	paths := make([]*types.QueryCommitmentPathAtRoot, len(commitments))
	for i := uint64(0); i < snapshot.LeafCount; i++ {
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
