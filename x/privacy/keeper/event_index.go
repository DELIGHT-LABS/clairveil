package keeper

import (
	"encoding/binary"
	"encoding/hex"
	"strings"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/cometbft/cometbft/crypto/tmhash"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	defaultPrivacyEventsPage  = uint64(1)
	defaultPrivacyEventsLimit = uint64(100)
	maxPrivacyEventsLimit     = uint64(200)
)

func (k Keeper) emitIndexedPrivacyEvent(ctx sdk.Context, eventType string, attrs []sdk.Attribute) error {
	ctx.EventManager().EmitEvent(sdk.NewEvent(eventType, attrs...))
	return k.indexPrivacyEvent(ctx, eventType, txHashHexFromContext(ctx), attrs)
}

func (k Keeper) indexPrivacyEvent(ctx sdk.Context, eventType string, txHashHex string, attrs []sdk.Attribute) error {
	store := k.storeService.OpenKVStore(ctx)

	sequence, err := k.nextPrivacyEventSequence(ctx)
	if err != nil {
		return err
	}

	event := &privacytypes.QueryPrivacyEvent{
		Sequence:   sequence,
		Height:     ctx.BlockHeight(),
		TxHashHex:  strings.ToUpper(strings.TrimSpace(txHashHex)),
		EventType:  eventType,
		Attributes: make([]*privacytypes.QueryPrivacyEventAttribute, 0, len(attrs)),
	}
	for _, attr := range attrs {
		event.Attributes = append(event.Attributes, &privacytypes.QueryPrivacyEventAttribute{
			Key:   attr.Key,
			Value: attr.Value,
		})
	}

	return store.Set(privacytypes.GetPrivacyEventKey(ctx.BlockHeight(), sequence), k.cdc.MustMarshal(event))
}

func (k Keeper) nextPrivacyEventSequence(ctx sdk.Context) (uint64, error) {
	store := k.storeService.OpenKVStore(ctx)

	current, err := store.Get(privacytypes.GetPrivacyEventSequenceKey())
	if err != nil {
		return 0, err
	}

	var sequence uint64
	if len(current) > 0 {
		sequence = binary.BigEndian.Uint64(current)
	}
	sequence++

	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, sequence)
	if err := store.Set(privacytypes.GetPrivacyEventSequenceKey(), bz); err != nil {
		return 0, err
	}

	return sequence, nil
}

func (k Keeper) GetPrivacyEvents(ctx sdk.Context, afterHeight int64, page uint64, limit uint64, eventTypes []string) ([]*privacytypes.QueryPrivacyEvent, bool, error) {
	store := k.storeService.OpenKVStore(ctx)

	startHeight := afterHeight + 1
	if startHeight < 0 {
		startHeight = 0
	}

	iterator, err := store.Iterator(
		privacytypes.GetPrivacyEventStartKey(startHeight),
		storetypes.PrefixEndBytes(privacytypes.GetPrivacyEventPrefix()),
	)
	if err != nil {
		return nil, false, err
	}
	defer iterator.Close()

	typeFilter := normalizePrivacyEventTypes(eventTypes)
	skip := (page - 1) * limit
	events := make([]*privacytypes.QueryPrivacyEvent, 0, limit)
	hasMore := false

	for ; iterator.Valid(); iterator.Next() {
		var event privacytypes.QueryPrivacyEvent
		k.cdc.MustUnmarshal(iterator.Value(), &event)

		if !privacyEventTypeAllowed(event.EventType, typeFilter) {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		if uint64(len(events)) == limit {
			hasMore = true
			break
		}

		eventCopy := event
		events = append(events, &eventCopy)
	}

	return events, hasMore, nil
}

func normalizePrivacyEventTypes(eventTypes []string) map[string]struct{} {
	if len(eventTypes) == 0 {
		return nil
	}

	out := make(map[string]struct{}, len(eventTypes))
	for _, eventType := range eventTypes {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		out[eventType] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func privacyEventTypeAllowed(eventType string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[eventType]
	return ok
}

func txHashHexFromContext(ctx sdk.Context) string {
	if len(ctx.TxBytes()) == 0 {
		return ""
	}
	return hex.EncodeToString(tmhash.Sum(ctx.TxBytes()))
}
