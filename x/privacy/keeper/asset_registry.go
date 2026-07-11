package keeper

import (
	"bytes"
	"fmt"
	"strings"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// CanonicalAssetIDV1 validates the Cosmos denom boundary and returns the exact
// 32-byte NoteV1 asset ID. AssetRegistryV1 is the authoritative denom mapping;
// this helper does not imply that the returned asset has been registered.
func CanonicalAssetIDV1(canonicalDenom string) ([]byte, error) {
	if canonicalDenom != strings.TrimSpace(canonicalDenom) {
		return nil, fmt.Errorf("asset denom must not contain surrounding whitespace")
	}
	if err := sdk.ValidateDenom(canonicalDenom); err != nil {
		return nil, fmt.Errorf("asset denom is invalid: %w", err)
	}

	assetID := make([]byte, fieldElementByteSize)
	types.ComputeAssetIDV1(canonicalDenom).FillBytes(assetID)
	return assetID, nil
}

// RegisterAssetV1 creates both directions of the AssetRegistryV1 mapping.
// Re-registration, including an idempotent-looking registration, is rejected
// so governance/genesis mistakes cannot silently change protocol state.
func (k Keeper) RegisterAssetV1(ctx sdk.Context, canonicalDenom string, assetID []byte) error {
	expected, err := CanonicalAssetIDV1(canonicalDenom)
	if err != nil {
		return err
	}
	canonicalAssetID, err := validateFieldElementBytesStrict(assetID)
	if err != nil {
		return fmt.Errorf("asset ID must be canonical 32-byte field bytes: %w", err)
	}
	if !bytes.Equal(expected, canonicalAssetID) {
		return fmt.Errorf("asset ID does not match canonical denom %q", canonicalDenom)
	}

	store := k.storeService.OpenKVStore(ctx)
	denomKey := types.GetAssetByDenomKey(canonicalDenom)
	idKey := types.GetAssetByIDKey(canonicalAssetID)

	denomExists, err := store.Has(denomKey)
	if err != nil {
		return fmt.Errorf("check asset denom registration: %w", err)
	}
	if denomExists {
		return fmt.Errorf("asset denom %q is already registered", canonicalDenom)
	}
	idExists, err := store.Has(idKey)
	if err != nil {
		return fmt.Errorf("check asset ID registration: %w", err)
	}
	if idExists {
		return fmt.Errorf("asset ID %x is already registered", canonicalAssetID)
	}

	if err := store.Set(denomKey, append([]byte(nil), canonicalAssetID...)); err != nil {
		return fmt.Errorf("store asset denom mapping: %w", err)
	}
	if err := store.Set(idKey, []byte(canonicalDenom)); err != nil {
		return fmt.Errorf("store asset ID mapping: %w", err)
	}
	return nil
}

func (k Keeper) RegisterCanonicalAssetV1(ctx sdk.Context, canonicalDenom string) (*types.AssetRegistryEntryV1, error) {
	assetID, err := CanonicalAssetIDV1(canonicalDenom)
	if err != nil {
		return nil, err
	}
	if err := k.RegisterAssetV1(ctx, canonicalDenom, assetID); err != nil {
		return nil, err
	}
	return &types.AssetRegistryEntryV1{CanonicalDenom: canonicalDenom, AssetId: assetID}, nil
}

func (k Keeper) GetAssetByDenomV1(ctx sdk.Context, canonicalDenom string) (*types.AssetRegistryEntryV1, bool, error) {
	if _, err := CanonicalAssetIDV1(canonicalDenom); err != nil {
		return nil, false, err
	}
	store := k.storeService.OpenKVStore(ctx)
	assetID, err := store.Get(types.GetAssetByDenomKey(canonicalDenom))
	if err != nil {
		return nil, false, err
	}
	if len(assetID) == 0 {
		return nil, false, nil
	}
	canonicalAssetID, err := validateFieldElementBytesStrict(assetID)
	if err != nil {
		return nil, false, fmt.Errorf("asset registry denom mapping is corrupt: %w", err)
	}
	expected, _ := CanonicalAssetIDV1(canonicalDenom)
	if !bytes.Equal(expected, canonicalAssetID) {
		return nil, false, fmt.Errorf("asset registry denom mapping does not match NoteV1 asset ID")
	}
	reverseDenom, err := store.Get(types.GetAssetByIDKey(canonicalAssetID))
	if err != nil {
		return nil, false, err
	}
	if string(reverseDenom) != canonicalDenom {
		return nil, false, fmt.Errorf("asset registry reverse mapping is missing or inconsistent")
	}
	return &types.AssetRegistryEntryV1{
		CanonicalDenom: canonicalDenom,
		AssetId:        append([]byte(nil), canonicalAssetID...),
	}, true, nil
}

func (k Keeper) GetAssetByIDV1(ctx sdk.Context, assetID []byte) (*types.AssetRegistryEntryV1, bool, error) {
	canonicalAssetID, err := validateFieldElementBytesStrict(assetID)
	if err != nil {
		return nil, false, fmt.Errorf("asset ID must be canonical 32-byte field bytes: %w", err)
	}
	store := k.storeService.OpenKVStore(ctx)
	denomBytes, err := store.Get(types.GetAssetByIDKey(canonicalAssetID))
	if err != nil {
		return nil, false, err
	}
	if len(denomBytes) == 0 {
		return nil, false, nil
	}
	canonicalDenom := string(denomBytes)
	expected, err := CanonicalAssetIDV1(canonicalDenom)
	if err != nil {
		return nil, false, fmt.Errorf("asset registry ID mapping is corrupt: %w", err)
	}
	if !bytes.Equal(expected, canonicalAssetID) {
		return nil, false, fmt.Errorf("asset registry ID mapping does not match NoteV1 asset ID")
	}
	forwardID, err := store.Get(types.GetAssetByDenomKey(canonicalDenom))
	if err != nil {
		return nil, false, err
	}
	if !bytes.Equal(forwardID, canonicalAssetID) {
		return nil, false, fmt.Errorf("asset registry forward mapping is missing or inconsistent")
	}
	return &types.AssetRegistryEntryV1{
		CanonicalDenom: canonicalDenom,
		AssetId:        append([]byte(nil), canonicalAssetID...),
	}, true, nil
}

// RequireRegisteredAssetV1 is the Deposit/SDK boundary: a valid denom hash is
// insufficient unless both authoritative registry directions are present.
func (k Keeper) RequireRegisteredAssetV1(ctx sdk.Context, canonicalDenom string) ([]byte, error) {
	entry, found, err := k.GetAssetByDenomV1(ctx, canonicalDenom)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("asset denom %q is not registered", canonicalDenom)
	}
	return append([]byte(nil), entry.AssetId...), nil
}

func (k Keeper) InitGenesisAssetRegistryV1(ctx sdk.Context, entries []*types.AssetRegistryEntryV1) error {
	for i, entry := range entries {
		if entry == nil {
			return fmt.Errorf("genesis asset registry entry %d is nil", i)
		}
		if err := k.RegisterAssetV1(ctx, entry.CanonicalDenom, entry.AssetId); err != nil {
			return fmt.Errorf("genesis asset registry entry %d is invalid: %w", i, err)
		}
	}
	return nil
}

func (k Keeper) ExportGenesisAssetRegistryV1(ctx sdk.Context) ([]*types.AssetRegistryEntryV1, error) {
	store := k.storeService.OpenKVStore(ctx)
	denomPrefix := types.GetAssetByDenomPrefix()
	iterator, err := store.Iterator(denomPrefix, storetypes.PrefixEndBytes(denomPrefix))
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	entries := make([]*types.AssetRegistryEntryV1, 0)
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) <= len(denomPrefix) {
			return nil, fmt.Errorf("asset registry contains an empty denom key")
		}
		canonicalDenom := string(key[len(denomPrefix):])
		entry, found, err := k.GetAssetByDenomV1(ctx, canonicalDenom)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("asset registry entry %q disappeared during export", canonicalDenom)
		}
		entries = append(entries, entry)
	}

	// Validate the reverse namespace independently. Walking only the forward
	// map would omit an orphan ID key from genesis export and silently turn a
	// corrupt 1:1 registry into apparently valid exported state.
	idPrefix := types.GetAssetByIDPrefix()
	reverseIterator, err := store.Iterator(idPrefix, storetypes.PrefixEndBytes(idPrefix))
	if err != nil {
		return nil, err
	}
	defer reverseIterator.Close()
	reverseCount := 0
	for ; reverseIterator.Valid(); reverseIterator.Next() {
		key := reverseIterator.Key()
		if len(key) != len(idPrefix)+fieldElementByteSize {
			return nil, fmt.Errorf("asset registry contains a malformed reverse ID key")
		}
		assetID := append([]byte(nil), key[len(idPrefix):]...)
		_, found, err := k.GetAssetByIDV1(ctx, assetID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("asset registry reverse entry %x disappeared during export", assetID)
		}
		reverseCount++
	}
	if reverseCount != len(entries) {
		return nil, fmt.Errorf("asset registry forward/reverse entry count mismatch: forward=%d reverse=%d", len(entries), reverseCount)
	}
	return entries, nil
}
