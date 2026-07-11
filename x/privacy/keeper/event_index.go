package keeper

import (
	"encoding/hex"
	"fmt"
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
	scanSummary, scanOutputs, err := k.buildLegacyPrivacyScanV2(ctx, sequence, ctx.BlockHeight(), txHashHex, eventType, attrs)
	if err != nil {
		return fmt.Errorf("build typed privacy scan index: %w", err)
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

	if err := k.StorePrivacyScanV2(ctx, scanSummary, scanOutputs); err != nil {
		return fmt.Errorf("store typed privacy scan index: %w", err)
	}
	if err := k.RecordCurrentMerkleRootSnapshotV1(ctx); err != nil {
		return fmt.Errorf("record privacy merkle root snapshot: %w", err)
	}
	return store.Set(privacytypes.GetPrivacyEventKey(ctx.BlockHeight(), sequence), k.cdc.MustMarshal(event))
}

func (k Keeper) nextPrivacyEventSequence(ctx sdk.Context) (uint64, error) {
	return k.AllocatePrivacyGlobalSequence(ctx)
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
			scanEvent, err := k.projectScanEvent(ctx, &event)
			if err != nil {
				return nil, 0, 0, false, err
			}
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

func (k Keeper) projectScanEvent(ctx sdk.Context, event *privacytypes.QueryPrivacyEvent) (*privacytypes.QueryScanEvent, error) {
	if event == nil {
		return nil, nil
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
		output, err := k.projectScanOutput(ctx, 0, attrs[privacytypes.AttributeKeyCommitment], attrs[privacytypes.AttributeKeyEncryptedNote], "", "")
		if err != nil {
			return nil, err
		}
		scanEvent.Outputs = append(scanEvent.Outputs, output)
	case privacytypes.EventTypeShieldedTransfer:
		for i, commitmentKey := range []string{privacytypes.AttributeKeyCommitment1, privacytypes.AttributeKeyCommitment2} {
			ciphertextKey := []string{privacytypes.AttributeKeyCipherText1, privacytypes.AttributeKeyCipherText2}[i]
			viewTagKey := []string{privacytypes.AttributeKeyViewTag1, privacytypes.AttributeKeyViewTag2}[i]
			output, err := k.projectScanOutput(ctx, uint32(i), attrs[commitmentKey], "", attrs[ciphertextKey], attrs[viewTagKey])
			if err != nil {
				return nil, err
			}
			scanEvent.Outputs = append(scanEvent.Outputs, output)
		}
		scanEvent.NullifierHexes = appendIfNotEmpty(scanEvent.NullifierHexes, attrs[privacytypes.AttributeKeyNullifier1])
		scanEvent.NullifierHexes = appendIfNotEmpty(scanEvent.NullifierHexes, attrs[privacytypes.AttributeKeyNullifier2])
	case privacytypes.EventTypeWithdraw:
		scanEvent.NullifierHexes = appendIfNotEmpty(scanEvent.NullifierHexes, attrs[privacytypes.AttributeKeyNullifier])
	default:
		return nil, nil
	}

	return scanEvent, nil
}

func (k Keeper) projectScanOutput(ctx sdk.Context, outputIndex uint32, commitmentHex string, encryptedNoteHex string, cipherTextHex string, viewTagHex string) (*privacytypes.QueryScanOutput, error) {
	output := &privacytypes.QueryScanOutput{
		OutputIndex:      outputIndex,
		CommitmentHex:    commitmentHex,
		EncryptedNoteHex: encryptedNoteHex,
		CipherTextHex:    cipherTextHex,
		ViewTagHex:       viewTagHex,
	}

	commitmentBytes, err := hex.DecodeString(commitmentHex)
	if err != nil {
		return output, nil
	}
	canonicalCommitment, err := validateFieldElementBytesStrict(commitmentBytes)
	if err != nil {
		return output, nil
	}
	leafIndex, found, err := k.GetCommitmentIndex(ctx, canonicalCommitment)
	if err != nil {
		return nil, err
	}
	output.LeafIndexFound = found
	output.LeafIndex = leafIndex
	return output, nil
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
