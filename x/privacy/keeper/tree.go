package keeper

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	MerkleDepth = 32
	// MaxMerkleLeaves is the fixed-capacity leaf limit for the active tree.
	MaxMerkleLeaves = uint64(1) << MerkleDepth
	// MaxMerkleRebuildLeaves bounds legacy root/node rebuilds when cached tree state is missing.
	MaxMerkleRebuildLeaves = uint64(1) << 20
)

var (
	errMerkleCommitmentNotFound  = errors.New("commitment was not found in the merkle tree")
	errMerkleTreeOverflow        = errors.New("merkle tree leaf count exceeds max capacity")
	errMerkleTreeCapacity        = errors.New("merkle tree capacity exceeded")
	errMerkleTreeRebuildTooLarge = errors.New("merkle tree cached root is required for large rebuild")
	errMerkleTreeNodeMissing     = errors.New("merkle tree node is missing")
	errMerkleTreeLeafMissing     = errors.New("merkle tree leaf is missing")
	errMerkleTreeLeafMismatch    = errors.New("merkle tree commitment index does not match stored leaf")
)

func validateMerkleLeafCount(count uint64) error {
	if count > MaxMerkleLeaves {
		return fmt.Errorf("%w: leaf_count=%d max_leaves=%d", errMerkleTreeOverflow, count, MaxMerkleLeaves)
	}
	return nil
}

func validateMerkleRebuildCount(count uint64) error {
	if err := validateMerkleLeafCount(count); err != nil {
		return err
	}
	if count > MaxMerkleRebuildLeaves {
		return fmt.Errorf("%w: leaf_count=%d max_rebuild_leaves=%d", errMerkleTreeRebuildTooLarge, count, MaxMerkleRebuildLeaves)
	}
	return nil
}

func remainingMerkleLeaves(count uint64) (uint64, error) {
	if err := validateMerkleLeafCount(count); err != nil {
		return 0, err
	}
	return MaxMerkleLeaves - count, nil
}

// EnsureCanAppendCommitments fails before a message writes more leaves than the tree can hold.
func (k Keeper) EnsureCanAppendCommitments(ctx sdk.Context, appendCount uint64) error {
	count := k.GetLeafCount(ctx)
	remaining, err := remainingMerkleLeaves(count)
	if err != nil {
		return err
	}
	if appendCount > remaining {
		return fmt.Errorf("%w: leaf_count=%d max_leaves=%d append_count=%d remaining_leaves=%d", errMerkleTreeCapacity, count, MaxMerkleLeaves, appendCount, remaining)
	}
	if err := k.validateMerkleCachedRootOrSmallRebuild(ctx, count); err != nil {
		return err
	}
	return nil
}

func (k Keeper) validateMerkleCachedRootOrSmallRebuild(ctx sdk.Context, count uint64) error {
	if err := validateMerkleLeafCount(count); err != nil {
		return err
	}
	if count == 0 || k.HasMerkleNode(ctx, uint8(MerkleDepth), 0) {
		return nil
	}
	return validateMerkleRebuildCount(count)
}

func populatedNodeCountAtLevel(leafCount uint64, level int) uint64 {
	count := leafCount
	for i := 0; i < level && count > 0; i++ {
		count = (count + 1) / 2
	}
	return count
}

func (k Keeper) getLeafRequired(ctx sdk.Context, index uint64, leafCount uint64) ([]byte, error) {
	if index >= leafCount {
		return nil, fmt.Errorf("%w: index=%d leaf_count=%d", errMerkleTreeLeafMissing, index, leafCount)
	}

	leaf := k.GetLeaf(ctx, index)
	if len(leaf) == 0 {
		return nil, fmt.Errorf("%w: index=%d leaf_count=%d", errMerkleTreeLeafMissing, index, leafCount)
	}

	canonicalLeaf, err := canonicalizeFieldBytes(leaf)
	if err != nil {
		return nil, fmt.Errorf("merkle tree leaf at index %d is invalid: %w", index, err)
	}

	return canonicalLeaf, nil
}

func (k Keeper) getMerkleNodeOrZero(ctx sdk.Context, level uint8, index uint64, leafCount uint64) ([]byte, error) {
	if index >= populatedNodeCountAtLevel(leafCount, int(level)) {
		return zeroNodeBytes(), nil
	}
	if !k.HasMerkleNode(ctx, level, index) {
		return nil, fmt.Errorf("%w: level=%d index=%d leaf_count=%d", errMerkleTreeNodeMissing, level, index, leafCount)
	}

	return k.GetMerkleNode(ctx, level, index), nil
}

func (k Keeper) validateAppendPath(ctx sdk.Context, index uint64) error {
	currentIndex := index
	for level := 0; level < MerkleDepth; level++ {
		if currentIndex%2 == 1 {
			if _, err := k.getMerkleNodeOrZero(ctx, uint8(level), currentIndex-1, index); err != nil {
				return err
			}
		}
		currentIndex /= 2
	}
	return nil
}

func hashNodes(left, right []byte) []byte {
	leftBig := new(big.Int).SetBytes(left)
	rightBig := new(big.Int).SetBytes(right)
	res := crypto.MimcHash(leftBig, rightBig)
	return res.Bytes()
}

func zeroNodeBytes() []byte {
	return big.NewInt(0).Bytes()
}

// AppendCommitment appends a leaf commitment and updates the incremental tree state.
func (k Keeper) AppendCommitment(ctx sdk.Context, commitment []byte) error {
	canonicalCommitment, err := canonicalizeFieldBytes(commitment)
	if err != nil {
		return err
	}

	if err := k.EnsureCanAppendCommitments(ctx, 1); err != nil {
		return err
	}

	if err := k.ensureIncrementalTreeState(ctx); err != nil {
		return err
	}

	index := k.GetLeafCount(ctx)
	if err := k.validateAppendPath(ctx, index); err != nil {
		return err
	}

	k.SetLeaf(ctx, index, canonicalCommitment)
	k.SetCommitmentIndex(ctx, canonicalCommitment, index)

	current := canonicalCommitment
	currentIndex := index
	k.SetMerkleNode(ctx, 0, currentIndex, current)

	for level := 0; level < MerkleDepth; level++ {
		var left []byte
		var right []byte

		if currentIndex%2 == 0 {
			left = current
			right = zeroNodeBytes()
		} else {
			left, err = k.getMerkleNodeOrZero(ctx, uint8(level), currentIndex-1, index)
			if err != nil {
				return err
			}
			right = current
		}

		parent := hashNodes(left, right)
		parentIndex := currentIndex / 2
		k.SetMerkleNode(ctx, uint8(level+1), parentIndex, parent)

		current = parent
		currentIndex = parentIndex
	}

	k.SetHistoricalRoot(ctx, current)
	k.SetLeafCount(ctx, index+1)
	return nil
}

// RecalculateRoot rebuilds a root from the stored leaves when the cached root is missing.
func (k Keeper) RecalculateRoot(ctx sdk.Context, count uint64) ([]byte, error) {
	if count == 0 {
		return zeroNodeBytes(), nil
	}
	if err := validateMerkleRebuildCount(count); err != nil {
		return nil, err
	}

	layer := make([][]byte, count)
	for i := uint64(0); i < count; i++ {
		leaf, err := k.getLeafRequired(ctx, i, count)
		if err != nil {
			return nil, err
		}
		layer[i] = leaf
	}

	for i := 0; i < MerkleDepth; i++ {
		nextLayerLen := (len(layer) + 1) / 2
		nextLayer := make([][]byte, nextLayerLen)

		for j := 0; j < len(layer); j += 2 {
			left := layer[j]
			var right []byte
			if j+1 < len(layer) {
				right = layer[j+1]
			} else {
				right = zeroNodeBytes()
			}
			nextLayer[j/2] = hashNodes(left, right)
		}
		layer = nextLayer
	}
	return layer[0], nil
}

// GetPath returns the Merkle authentication path for a commitment.
func (k Keeper) GetPath(ctx sdk.Context, commitment []byte) ([]string, []uint32, []byte, error) {
	if err := k.ensureIncrementalTreeState(ctx); err != nil {
		return nil, nil, nil, err
	}

	count := k.GetLeafCount(ctx)
	if count == 0 {
		return nil, nil, nil, errMerkleCommitmentNotFound
	}

	canonicalCommitment := canonicalizeFieldBytesOrOriginal(commitment)
	targetIndex, found := k.GetCommitmentIndex(ctx, canonicalCommitment)
	if !found || targetIndex >= count {
		return nil, nil, nil, errMerkleCommitmentNotFound
	}
	targetLeaf, err := k.getLeafRequired(ctx, targetIndex, count)
	if err != nil {
		return nil, nil, nil, err
	}
	if !bytes.Equal(targetLeaf, canonicalCommitment) {
		return nil, nil, nil, fmt.Errorf("%w: index=%d leaf_count=%d", errMerkleTreeLeafMismatch, targetIndex, count)
	}

	pathHex := make([]string, MerkleDepth)
	helperInt := make([]uint32, MerkleDepth)

	currentIndex := targetIndex

	for i := 0; i < MerkleDepth; i++ {
		isRight := currentIndex % 2
		siblingIndex := currentIndex ^ 1

		sibling, err := k.getMerkleNodeOrZero(ctx, uint8(i), siblingIndex, count)
		if err != nil {
			return nil, nil, nil, err
		}

		pathHex[i] = fmt.Sprintf("%x", sibling)
		if len(sibling) == 0 {
			pathHex[i] = "00"
		}

		if isRight == 1 {
			helperInt[i] = 1
		} else {
			helperInt[i] = 0
		}

		currentIndex /= 2
	}

	root := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
	if len(root) == 0 {
		var err error
		root, err = k.RecalculateRoot(ctx, count)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return pathHex, helperInt, root, nil
}

func (k Keeper) ensureIncrementalTreeState(ctx sdk.Context) error {
	count := k.GetLeafCount(ctx)
	if err := k.validateMerkleCachedRootOrSmallRebuild(ctx, count); err != nil {
		return err
	}
	if count == 0 || k.HasMerkleNode(ctx, uint8(MerkleDepth), 0) {
		return nil
	}

	for i := uint64(0); i < count; i++ {
		leaf, err := k.getLeafRequired(ctx, i, count)
		if err != nil {
			return err
		}
		k.SetCommitmentIndex(ctx, leaf, i)

		current := leaf
		currentIndex := i
		k.SetMerkleNode(ctx, 0, currentIndex, current)

		for level := 0; level < MerkleDepth; level++ {
			var left []byte
			var right []byte

			if currentIndex%2 == 0 {
				left = current
				right = zeroNodeBytes()
			} else {
				left, err = k.getMerkleNodeOrZero(ctx, uint8(level), currentIndex-1, count)
				if err != nil {
					return err
				}
				right = current
			}

			parent := hashNodes(left, right)
			parentIndex := currentIndex / 2
			k.SetMerkleNode(ctx, uint8(level+1), parentIndex, parent)

			current = parent
			currentIndex = parentIndex
		}
	}
	return nil
}

func (k Keeper) GetLeafCount(ctx sdk.Context) uint64 {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get([]byte("LeafCount"))
	if err != nil || bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (k Keeper) SetLeafCount(ctx sdk.Context, count uint64) {
	store := k.storeService.OpenKVStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, count)
	store.Set([]byte("LeafCount"), bz)
}

func (k Keeper) SetLeaf(ctx sdk.Context, index uint64, leaf []byte) {
	store := k.storeService.OpenKVStore(ctx)
	key := append([]byte("Leaf/"), sdk.Uint64ToBigEndian(index)...)
	store.Set(key, leaf)
}

func (k Keeper) GetLeaf(ctx sdk.Context, index uint64) []byte {
	store := k.storeService.OpenKVStore(ctx)
	key := append([]byte("Leaf/"), sdk.Uint64ToBigEndian(index)...)
	bz, _ := store.Get(key)
	return bz
}

func (k Keeper) SetHistoricalRoot(ctx sdk.Context, root []byte) {
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetHistoricalRootKey(canonicalizeFieldBytesOrOriginal(root))
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(ctx.BlockHeight()))
	store.Set(key, bz)
}

func (k Keeper) CheckHistoricalRoot(ctx sdk.Context, root []byte) bool {
	store := k.storeService.OpenKVStore(ctx)
	canonicalRoot, err := canonicalizeFieldBytes(root)
	if err != nil {
		return false
	}

	key := types.GetHistoricalRootKey(canonicalRoot)
	flag, _ := store.Has(key)
	if !flag {
		k.Logger(ctx).Debug("historical root not found", "root", fmt.Sprintf("%x", canonicalRoot))
	}
	return flag
}

func (k Keeper) SetNullifier(ctx sdk.Context, nullifier []byte) {
	store := k.storeService.OpenKVStore(ctx)
	key := types.GetNullifierKey(canonicalizeFieldBytesOrOriginal(nullifier))
	store.Set(key, []byte{0x01})
}

func (k Keeper) HasNullifier(ctx sdk.Context, nullifier []byte) bool {
	store := k.storeService.OpenKVStore(ctx)
	canonicalNullifier, err := canonicalizeFieldBytes(nullifier)
	if err != nil {
		return false
	}

	key := types.GetNullifierKey(canonicalNullifier)
	flag, _ := store.Has(key)
	return flag
}

func (k Keeper) SetMerkleNode(ctx sdk.Context, level uint8, index uint64, node []byte) {
	store := k.storeService.OpenKVStore(ctx)
	store.Set(types.GetMerkleNodeKey(level, index), node)
}

func (k Keeper) GetMerkleNode(ctx sdk.Context, level uint8, index uint64) []byte {
	store := k.storeService.OpenKVStore(ctx)
	bz, _ := store.Get(types.GetMerkleNodeKey(level, index))
	return bz
}

func (k Keeper) HasMerkleNode(ctx sdk.Context, level uint8, index uint64) bool {
	store := k.storeService.OpenKVStore(ctx)
	flag, _ := store.Has(types.GetMerkleNodeKey(level, index))
	return flag
}

func (k Keeper) SetCommitmentIndex(ctx sdk.Context, commitment []byte, index uint64) {
	store := k.storeService.OpenKVStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, index)
	store.Set(types.GetCommitmentIndexKey(canonicalizeFieldBytesOrOriginal(commitment)), bz)
}

func (k Keeper) GetCommitmentIndex(ctx sdk.Context, commitment []byte) (uint64, bool) {
	store := k.storeService.OpenKVStore(ctx)
	canonicalCommitment, err := canonicalizeFieldBytes(commitment)
	if err != nil {
		return 0, false
	}

	key := types.GetCommitmentIndexKey(canonicalCommitment)
	flag, _ := store.Has(key)
	if !flag {
		return 0, false
	}

	bz, _ := store.Get(key)
	if len(bz) != 8 {
		return 0, false
	}

	return binary.BigEndian.Uint64(bz), true
}
