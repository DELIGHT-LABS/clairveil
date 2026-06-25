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
	defaultScanEventsLimit    = uint64(500)
	maxScanEventsLimit        = uint64(1000)
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

func (k Keeper) GetScanEvents(ctx sdk.Context, afterHeight int64, afterSequence uint64, limit uint64, eventTypes []string) ([]*privacytypes.QueryScanEvent, int64, uint64, bool, error) {
	if limit == 0 {
		limit = defaultScanEventsLimit
	}
	if limit > maxScanEventsLimit {
		limit = maxScanEventsLimit
	}

	store := k.storeService.OpenKVStore(ctx)

	startHeight := afterHeight
	if startHeight < 0 {
		startHeight = 0
	}

	iterator, err := store.Iterator(
		privacytypes.GetPrivacyEventStartKey(startHeight),
		storetypes.PrefixEndBytes(privacytypes.GetPrivacyEventPrefix()),
	)
	if err != nil {
		return nil, afterHeight, afterSequence, false, err
	}
	defer iterator.Close()

	typeFilter := normalizeScanEventTypes(eventTypes)
	events := make([]*privacytypes.QueryScanEvent, 0, limit)
	nextHeight := afterHeight
	nextSequence := afterSequence
	hasMore := false
	visited := uint64(0)

	for ; iterator.Valid(); iterator.Next() {
		var event privacytypes.QueryPrivacyEvent
		k.cdc.MustUnmarshal(iterator.Value(), &event)

		if event.Height < afterHeight || (event.Height == afterHeight && event.Sequence <= afterSequence) {
			continue
		}

		visited++
		nextHeight = event.Height
		nextSequence = event.Sequence

		if privacyEventTypeAllowed(event.EventType, typeFilter) {
			scanEvent := k.projectScanEvent(ctx, &event)
			if scanEvent != nil {
				events = append(events, scanEvent)
			}
		}

		if visited == limit {
			iterator.Next()
			hasMore = iterator.Valid()
			break
		}
	}

	return events, nextHeight, nextSequence, hasMore, nil
}

func (k Keeper) projectScanEvent(ctx sdk.Context, event *privacytypes.QueryPrivacyEvent) *privacytypes.QueryScanEvent {
	if event == nil {
		return nil
	}

	attrs := privacyEventAttributesMap(event.Attributes)
	scanEvent := &privacytypes.QueryScanEvent{
		Sequence:  event.Sequence,
		Height:    event.Height,
		TxHashHex: event.TxHashHex,
		EventType: event.EventType,
	}

	switch event.EventType {
	case privacytypes.EventTypeDeposit:
		scanEvent.Outputs = append(scanEvent.Outputs, k.projectScanOutput(ctx, 0, attrs[privacytypes.AttributeKeyCommitment], attrs[privacytypes.AttributeKeyEncryptedNote], "", ""))
	case privacytypes.EventTypeShieldedTransfer:
		scanEvent.Outputs = append(scanEvent.Outputs,
			k.projectScanOutput(ctx, 0, attrs[privacytypes.AttributeKeyCommitment1], "", attrs[privacytypes.AttributeKeyCipherText1], attrs[privacytypes.AttributeKeyViewTag1]),
			k.projectScanOutput(ctx, 1, attrs[privacytypes.AttributeKeyCommitment2], "", attrs[privacytypes.AttributeKeyCipherText2], attrs[privacytypes.AttributeKeyViewTag2]),
		)
		scanEvent.NullifierHexes = appendIfNotEmpty(scanEvent.NullifierHexes, attrs[privacytypes.AttributeKeyNullifier1])
		scanEvent.NullifierHexes = appendIfNotEmpty(scanEvent.NullifierHexes, attrs[privacytypes.AttributeKeyNullifier2])
	case privacytypes.EventTypeWithdraw:
		scanEvent.NullifierHexes = appendIfNotEmpty(scanEvent.NullifierHexes, attrs[privacytypes.AttributeKeyNullifier])
	default:
		return nil
	}

	return scanEvent
}

func (k Keeper) projectScanOutput(ctx sdk.Context, outputIndex uint32, commitmentHex string, encryptedNoteHex string, cipherTextHex string, viewTagHex string) *privacytypes.QueryScanOutput {
	output := &privacytypes.QueryScanOutput{
		OutputIndex:      outputIndex,
		CommitmentHex:    commitmentHex,
		EncryptedNoteHex: encryptedNoteHex,
		CipherTextHex:    cipherTextHex,
		ViewTagHex:       viewTagHex,
	}

	commitmentBytes, err := hex.DecodeString(commitmentHex)
	if err != nil {
		return output
	}
	canonicalCommitment, err := validateFieldElementBytesStrict(commitmentBytes)
	if err != nil {
		return output
	}
	leafIndex, found := k.GetCommitmentIndex(ctx, canonicalCommitment)
	output.LeafIndexFound = found
	output.LeafIndex = leafIndex
	return output
}

func privacyEventAttributesMap(attrs []*privacytypes.QueryPrivacyEventAttribute) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		if attr == nil {
			continue
		}
		out[attr.Key] = strings.Trim(attr.Value, "\"")
	}
	return out
}

func appendIfNotEmpty(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	return append(values, value)
}

func normalizeScanEventTypes(eventTypes []string) map[string]struct{} {
	normalized := normalizePrivacyEventTypes(eventTypes)
	if len(normalized) == 0 {
		return map[string]struct{}{
			privacytypes.EventTypeDeposit:          {},
			privacytypes.EventTypeShieldedTransfer: {},
		}
	}
	return normalized
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
