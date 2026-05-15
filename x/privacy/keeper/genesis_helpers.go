package keeper

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) InitGenesisCommitments(ctx sdk.Context, commitments [][]byte) error {
	if err := k.EnsureCanAppendCommitments(ctx, uint64(len(commitments))); err != nil {
		return fmt.Errorf("genesis commitments exceed merkle tree capacity: %w", err)
	}

	for i, commitment := range commitments {
		canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
		if err != nil {
			return fmt.Errorf("genesis commitment at index %d is invalid: %w", i, err)
		}

		if err := k.AppendCommitment(ctx, canonicalCommitment); err != nil {
			return fmt.Errorf("failed to append the genesis commitment at index %d: %w", i, err)
		}
	}

	return nil
}

func (k Keeper) InitGenesisHistoricalRoots(ctx sdk.Context, roots [][]byte) error {
	for i, root := range roots {
		canonicalRoot, err := validateFieldElementBytesStrict(root)
		if err != nil {
			return fmt.Errorf("genesis historical root at index %d is invalid: %w", i, err)
		}

		k.SetHistoricalRoot(ctx, canonicalRoot)
	}

	return nil
}

func (k Keeper) InitGenesisNullifiers(ctx sdk.Context, nullifiers [][]byte) error {
	for i, nullifier := range nullifiers {
		canonicalNullifier, err := validateFieldElementBytesStrict(nullifier)
		if err != nil {
			return fmt.Errorf("genesis nullifier at index %d is invalid: %w", i, err)
		}

		k.SetNullifier(ctx, canonicalNullifier)
	}

	return nil
}

func (k Keeper) ExportGenesisCommitments(ctx sdk.Context) ([][]byte, error) {
	count := k.GetLeafCount(ctx)
	if err := validateMerkleLeafCount(count); err != nil {
		return nil, fmt.Errorf("cannot export genesis commitments from invalid merkle tree state: %w", err)
	}

	commitments := make([][]byte, 0, count)

	for i := uint64(0); i < count; i++ {
		leaf := k.GetLeaf(ctx, i)
		if len(leaf) == 0 {
			return nil, fmt.Errorf("commitment leaf at index %d is missing during genesis export", i)
		}

		canonicalLeaf, err := canonicalizeFieldBytes(leaf)
		if err != nil {
			return nil, fmt.Errorf("commitment leaf at index %d is invalid during genesis export: %w", i, err)
		}

		canonicalLeaf, err = validateFieldElementBytesStrict(canonicalLeaf)
		if err != nil {
			return nil, fmt.Errorf("commitment leaf at index %d is non-canonical during genesis export: %w", i, err)
		}

		out := make([]byte, len(canonicalLeaf))
		copy(out, canonicalLeaf)
		commitments = append(commitments, out)
	}

	return commitments, nil
}

func (k Keeper) ExportGenesisHistoricalRoots(ctx sdk.Context) ([][]byte, error) {
	return k.exportFieldValuesByPrefix(ctx, types.KeyPrefixHistoricalRoot)
}

func (k Keeper) ExportGenesisNullifiers(ctx sdk.Context) ([][]byte, error) {
	return k.exportFieldValuesByPrefix(ctx, types.KeyPrefixNullifier)
}

func (k Keeper) exportFieldValuesByPrefix(ctx sdk.Context, prefix byte) ([][]byte, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefixBytes := []byte{prefix}

	iterator, err := store.Iterator(prefixBytes, storetypes.PrefixEndBytes(prefixBytes))
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	values := make([][]byte, 0)
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) <= 1 {
			return nil, fmt.Errorf("invalid genesis export key length for prefix 0x%x", prefix)
		}

		raw := make([]byte, len(key)-1)
		copy(raw, key[1:])

		canonical, err := canonicalizeFieldBytes(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid genesis export value for prefix 0x%x: %w", prefix, err)
		}

		canonical, err = validateFieldElementBytesStrict(canonical)
		if err != nil {
			return nil, fmt.Errorf("non-canonical genesis export value for prefix 0x%x: %w", prefix, err)
		}

		out := make([]byte, len(canonical))
		copy(out, canonical)
		values = append(values, out)
	}

	return values, nil
}
