package audit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/gogoproto/proto"
)

// CosmosBlockRPC is the two-call CometBFT boundary required by the collector.
// It intentionally does not depend on tx indexing or mempool APIs.
type CosmosBlockRPC interface {
	Block(context.Context, *int64) (*coretypes.ResultBlock, error)
	BlockResults(context.Context, *int64) (*coretypes.ResultBlockResults, error)
}

// CosmosTxSource obtains original transaction bytes and their final execution
// results for one closed block range. Cache may be reused to resume after the
// last completely stored block.
type CosmosTxSource struct {
	RPC        CosmosBlockRPC
	Decoder    sdk.TxDecoder
	Cache      *CosmosFileCache
	Network    [32]byte
	ChainID    string
	FromHeight uint64
	ToHeight   uint64
	// VerifyDelegatedExecution authenticates the downstream wrapper/receipt
	// success semantics for an in-process deposit. Code == 0 alone is not
	// sufficient because an EVM call may have reverted inside a successful
	// Cosmos transaction. Delegated evidence is rejected when this is nil.
	VerifyDelegatedExecution DelegatedExecutionVerifier
}

// DelegatedExecutionEvidence is the narrow downstream verification boundary
// for one event. The verifier must authenticate that the enclosing wrapper at
// MessageIndex committed (rather than reverted) using its receipt semantics.
type DelegatedExecutionEvidence struct {
	Height         uint64
	TxIndex        uint32
	RawTx          []byte
	Result         abci.ExecTxResult
	MessageIndex   uint32
	GlobalSequence uint64
	ExecutionID    [32]byte
	Funder         sdk.AccAddress
	Deposit        *privacyv2.MsgDeposit
}

type DelegatedExecutionVerifier func(context.Context, DelegatedExecutionEvidence) error

func (s CosmosTxSource) SuccessfulAuditTransactions(ctx context.Context, after uint64) ([]SuccessfulAuditTx, error) {
	if s.RPC == nil || s.Decoder == nil || s.Cache == nil || s.Network == ([32]byte{}) || s.ChainID == "" || s.FromHeight == 0 || s.ToHeight < s.FromHeight {
		return nil, fmt.Errorf("cosmos audit source requires RPC, decoder, cache, network, chain ID, and a valid range")
	}
	identity := CosmosCacheIdentity{Network: s.Network, ChainID: s.ChainID, FromHeight: s.FromHeight, ToHeight: s.ToHeight}
	state, err := s.Cache.Load(identity)
	if err != nil {
		return nil, err
	}
	existing, err := s.extract(ctx, state, 0)
	if err != nil {
		return nil, err
	}
	seenExecution, seenSequence, seenLocation := sourceRowIdentities(existing)
	next := s.FromHeight
	if state.LastProcessedHeight >= s.FromHeight {
		next = state.LastProcessedHeight + 1
	}
	for height := next; height <= s.ToHeight; height++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		block, results, err := s.fetchBlock(ctx, height)
		if err != nil {
			return nil, err
		}
		cached := make([]CachedCosmosTx, 0, len(block.Block.Data.Txs))
		for index, raw := range block.Block.Data.Txs {
			result := results.TxsResults[index]
			if result.Code != 0 {
				continue
			}
			decoded, err := s.Decoder(raw)
			if err != nil {
				return nil, fmt.Errorf("decode successful tx %x at height %d: %w", raw.Hash(), height, err)
			}
			if decoded == nil {
				return nil, fmt.Errorf("decode successful tx %x at height %d returned nil", raw.Hash(), height)
			}
			candidate := false
			for _, message := range decoded.GetMsgs() {
				if isAuditMessageType(message) {
					candidate = true
					break
				}
			}
			if !candidate {
				for _, event := range result.Events {
					if isPrivacyExecutionEvent(event.Type) {
						_, present, attrErr := executionAttributes(event)
						if present || attrErr != nil {
							candidate = true
							break
						}
					}
				}
			}
			if !candidate {
				continue
			}
			cached = append(cached, CachedCosmosTx{
				Height: height,
				Index:  uint32(index),
				Hash:   bytes.Clone(raw.Hash()),
				Raw:    bytes.Clone(raw),
				Result: cloneExecTxResult(result),
			})
		}
		nextState := cloneCosmosCacheState(state)
		nextState.Transactions = append(nextState.Transactions, cached...)
		nextState.LastProcessedHeight = height
		// Validate every candidate execution before advancing the durable
		// checkpoint. A missing/partial event or a forged location leaves this
		// entire block unprocessed and therefore retryable.
		newRows, err := s.extract(ctx, CosmosCacheState{Transactions: cached}, 0)
		if err != nil {
			return nil, err
		}
		for _, row := range newRows {
			location := sourceRowLocation(row)
			var executionID [32]byte
			copy(executionID[:], row.Event.ExecutionID)
			if _, duplicate := seenExecution[executionID]; duplicate {
				return nil, fmt.Errorf("duplicate successful audit execution ID %x", executionID)
			}
			if _, duplicate := seenSequence[row.Event.GlobalSequence]; duplicate {
				return nil, fmt.Errorf("duplicate successful audit global sequence %d", row.Event.GlobalSequence)
			}
			if _, duplicate := seenLocation[location]; duplicate {
				return nil, fmt.Errorf("duplicate successful audit message location %s", location)
			}
			seenExecution[executionID] = struct{}{}
			seenSequence[row.Event.GlobalSequence] = struct{}{}
			seenLocation[location] = struct{}{}
		}
		if err := s.Cache.Store(nextState); err != nil {
			return nil, fmt.Errorf("store audit collector block %d: %w", height, err)
		}
		state = nextState
		if height == ^uint64(0) { // defensive overflow guard
			break
		}
	}
	return s.extract(ctx, state, after)
}

func sourceRowLocation(row SuccessfulAuditTx) string {
	base := fmt.Sprintf("%d/%x/%d", row.Event.Height, row.Event.TxHash, row.Event.MessageIndex)
	if row.Event.Funder != "" {
		return fmt.Sprintf("%s/%x", base, row.Event.ExecutionID)
	}
	return base
}

func sourceRowIdentities(rows []SuccessfulAuditTx) (map[[32]byte]struct{}, map[uint64]struct{}, map[string]struct{}) {
	executions := make(map[[32]byte]struct{}, len(rows))
	sequences := make(map[uint64]struct{}, len(rows))
	locations := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		var executionID [32]byte
		copy(executionID[:], row.Event.ExecutionID)
		executions[executionID] = struct{}{}
		sequences[row.Event.GlobalSequence] = struct{}{}
		locations[sourceRowLocation(row)] = struct{}{}
	}
	return executions, sequences, locations
}

func (s CosmosTxSource) fetchBlock(ctx context.Context, height uint64) (*coretypes.ResultBlock, *coretypes.ResultBlockResults, error) {
	if height > uint64(^uint64(0)>>1) {
		return nil, nil, fmt.Errorf("block height %d exceeds int64", height)
	}
	h := int64(height)
	block, err := s.RPC.Block(ctx, &h)
	if err != nil {
		return nil, nil, fmt.Errorf("block %d: %w", height, err)
	}
	results, err := s.RPC.BlockResults(ctx, &h)
	if err != nil {
		return nil, nil, fmt.Errorf("block results %d: %w", height, err)
	}
	if block == nil || block.Block == nil || block.Block.Height != h || block.Block.ChainID != s.ChainID {
		return nil, nil, fmt.Errorf("block %d identity mismatch", height)
	}
	if results == nil || results.Height != h || len(block.Block.Data.Txs) != len(results.TxsResults) {
		return nil, nil, fmt.Errorf("block %d transaction/result count mismatch", height)
	}
	for index, result := range results.TxsResults {
		if result == nil {
			return nil, nil, fmt.Errorf("block %d result %d is nil", height, index)
		}
	}
	return block, results, nil
}

func (s CosmosTxSource) extract(ctx context.Context, state CosmosCacheState, after uint64) ([]SuccessfulAuditTx, error) {
	rows := make([]SuccessfulAuditTx, 0)
	seenExecution := map[[32]byte]struct{}{}
	seenSequence := map[uint64]struct{}{}
	seenLocation := map[string]struct{}{}
	for _, cached := range state.Transactions {
		if cached.Result.Code != 0 {
			continue
		}
		tx, err := s.Decoder(cached.Raw)
		if err != nil {
			return nil, fmt.Errorf("decode successful tx %x at height %d: %w", cached.Hash, cached.Height, err)
		}
		if tx == nil {
			return nil, fmt.Errorf("decoded successful tx %x is nil", cached.Hash)
		}
		msgs := tx.GetMsgs()
		matched := make(map[uint32]struct{})
		for eventIndex, event := range cached.Result.Events {
			if !isPrivacyExecutionEvent(event.Type) {
				continue
			}
			attrs, present, err := executionAttributes(event)
			if err != nil {
				return nil, fmt.Errorf("height %d tx %d event %d: %w", cached.Height, cached.Index, eventIndex, err)
			}
			if !present {
				continue // legacy/non-audit event; never synthesize evidence from it
			}
			global, err := canonicalUint(attrs["global_sequence"])
			if err != nil || global == 0 {
				return nil, fmt.Errorf("height %d tx %d event %d has invalid global sequence", cached.Height, cached.Index, eventIndex)
			}
			messageIndex64, err := canonicalUint(attrs["message_index"])
			if err != nil || messageIndex64 > uint64(^uint32(0)) || messageIndex64 >= uint64(len(msgs)) {
				return nil, fmt.Errorf("height %d tx %d event %d has invalid message index", cached.Height, cached.Index, eventIndex)
			}
			messageIndex := uint32(messageIndex64)
			delegated, funder, err := delegatedDepositFromAttributes(attrs)
			if err != nil {
				return nil, fmt.Errorf("height %d tx %d event %d: %w", cached.Height, cached.Index, eventIndex, err)
			}
			var rawMessage sdk.Msg = msgs[messageIndex]
			if delegated != nil {
				if isAuditMessageType(rawMessage) {
					return nil, fmt.Errorf("height %d tx %d event %d delegated deposit cannot reference a top-level audit message", cached.Height, cached.Index, eventIndex)
				}
				rawMessage = delegated
			}
			message, err := privacytypes.ValidateAuditMessage(rawMessage)
			if err != nil {
				if delegated == nil {
					return nil, fmt.Errorf("height %d tx %d event %d does not reference a top-level audit message: %w", cached.Height, cached.Index, eventIndex, err)
				}
				return nil, fmt.Errorf("height %d tx %d event %d has invalid audit message: %w", cached.Height, cached.Index, eventIndex, err)
			}
			if event.Type != eventTypeForKind(message.Kind()) {
				return nil, fmt.Errorf("height %d tx %d event %d kind mismatch", cached.Height, cached.Index, eventIndex)
			}
			txHash, err := canonicalHex32(attrs["tx_hash"])
			if err != nil || !bytes.Equal(txHash[:], cached.Hash) {
				return nil, fmt.Errorf("height %d tx %d event %d transaction hash mismatch", cached.Height, cached.Index, eventIndex)
			}
			executionID, err := canonicalHex32(attrs["execution_id"])
			if err != nil {
				return nil, fmt.Errorf("height %d tx %d event %d has invalid execution ID", cached.Height, cached.Index, eventIndex)
			}
			want := collectedExecutionID(s.Network, txHash, global, messageIndex)
			if executionID != want {
				return nil, fmt.Errorf("height %d tx %d event %d execution ID mismatch", cached.Height, cached.Index, eventIndex)
			}
			funderString := ""
			location := fmt.Sprintf("%d/%x/%d", cached.Height, cached.Hash, messageIndex)
			if delegated != nil {
				if s.VerifyDelegatedExecution == nil {
					return nil, fmt.Errorf("AUDIT_INCOMPLETE: delegated deposit execution verifier is required")
				}
				evidence := DelegatedExecutionEvidence{
					Height: cached.Height, TxIndex: cached.Index, RawTx: bytes.Clone(cached.Raw), Result: cloneExecTxResult(&cached.Result),
					MessageIndex: messageIndex, GlobalSequence: global, ExecutionID: executionID,
					Funder: append(sdk.AccAddress(nil), funder...), Deposit: proto.Clone(delegated).(*privacyv2.MsgDeposit),
				}
				if err := s.VerifyDelegatedExecution(ctx, evidence); err != nil {
					return nil, fmt.Errorf("AUDIT_INCOMPLETE: height %d tx %d event %d delegated execution: %w", cached.Height, cached.Index, eventIndex, err)
				}
				funderString = funder.String()
				location = fmt.Sprintf("%s/%x", location, executionID)
			}
			if _, duplicate := seenExecution[executionID]; duplicate {
				return nil, fmt.Errorf("duplicate successful audit execution ID %x", executionID)
			}
			if _, duplicate := seenSequence[global]; duplicate {
				return nil, fmt.Errorf("duplicate successful audit global sequence %d", global)
			}
			if _, duplicate := seenLocation[location]; duplicate {
				return nil, fmt.Errorf("duplicate successful audit message location %s", location)
			}
			seenExecution[executionID] = struct{}{}
			seenSequence[global] = struct{}{}
			seenLocation[location] = struct{}{}
			if delegated == nil {
				matched[messageIndex] = struct{}{}
			}
			if global <= after {
				continue
			}
			rows = append(rows, SuccessfulAuditTx{Message: rawMessage, Event: ExecutionEvent{
				Height: cached.Height, TxIndex: cached.Index, TxHash: bytes.Clone(cached.Hash), EventType: event.Type,
				GlobalSequence: global, MessageIndex: messageIndex, ExecutionID: bytes.Clone(executionID[:]), Funder: funderString,
			}})
		}
		for index, message := range msgs {
			if !isAuditMessageType(message) {
				continue
			}
			if _, err := privacytypes.ValidateAuditMessage(message); err != nil {
				return nil, fmt.Errorf("height %d tx %d top-level privacy message %d is invalid: %w", cached.Height, cached.Index, index, err)
			}
			if _, found := matched[uint32(index)]; !found {
				return nil, fmt.Errorf("AUDIT_INCOMPLETE: height %d tx %d top-level privacy message %d has no matching execution event", cached.Height, cached.Index, index)
			}
		}
	}
	return rows, nil
}

func isAuditMessageType(message sdk.Msg) bool {
	switch message.(type) {
	case *privacyv2.MsgDeposit, *privacyv2.MsgWithdraw, *privacyv2.MsgTransfer, *privacyv2.MsgBatchTransfer:
		return true
	default:
		return false
	}
}

func executionAttributes(event abci.Event) (map[string]string, bool, error) {
	keys := []string{"execution_id", "global_sequence", "message_index", "tx_hash", privacytypes.AttributeKeyDelegatedDepositPayload, privacytypes.AttributeKeyDelegatedFunder}
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[key] = true
	}
	attrs := map[string]string{}
	for _, attr := range event.Attributes {
		if !wanted[attr.Key] {
			continue
		}
		if _, duplicate := attrs[attr.Key]; duplicate {
			return nil, false, fmt.Errorf("duplicate %s attribute", attr.Key)
		}
		attrs[attr.Key] = attr.Value
	}
	if len(attrs) == 0 {
		return nil, false, nil
	}
	core := 0
	for _, key := range []string{"execution_id", "global_sequence", "message_index", "tx_hash"} {
		if _, ok := attrs[key]; ok {
			core++
		}
	}
	_, payload := attrs[privacytypes.AttributeKeyDelegatedDepositPayload]
	_, funder := attrs[privacytypes.AttributeKeyDelegatedFunder]
	if core != 4 || payload != funder {
		return nil, false, fmt.Errorf("partial privacy execution attributes")
	}
	return attrs, true, nil
}

func delegatedDepositFromAttributes(attrs map[string]string) (*privacyv2.MsgDeposit, sdk.AccAddress, error) {
	encoded, delegated := attrs[privacytypes.AttributeKeyDelegatedDepositPayload]
	if !delegated {
		return nil, nil, nil
	}
	if encoded == "" || base64.StdEncoding.EncodedLen(privacytypes.MaxDelegatedDepositPayloadBytes) < len(encoded) {
		return nil, nil, fmt.Errorf("delegated deposit payload exceeds event cap")
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) == 0 || len(payload) > privacytypes.MaxDelegatedDepositPayloadBytes {
		return nil, nil, fmt.Errorf("invalid delegated deposit payload encoding")
	}
	message := &privacyv2.MsgDeposit{}
	if err := proto.Unmarshal(payload, message); err != nil {
		return nil, nil, fmt.Errorf("invalid delegated deposit protobuf: %w", err)
	}
	canonical, err := proto.Marshal(message)
	if err != nil || !bytes.Equal(canonical, payload) {
		return nil, nil, fmt.Errorf("noncanonical delegated deposit protobuf")
	}
	funderRaw := attrs[privacytypes.AttributeKeyDelegatedFunder]
	funder, err := sdk.AccAddressFromBech32(funderRaw)
	if err != nil || funder.String() != funderRaw || len(funder) == 0 {
		return nil, nil, fmt.Errorf("invalid delegated deposit funder")
	}
	if funder.Equals(authtypes.NewModuleAddress(privacytypes.ModuleName)) || funder.Equals(authtypes.NewModuleAddress(govtypes.ModuleName)) {
		return nil, nil, fmt.Errorf("invalid delegated deposit funder")
	}
	return message, funder, nil
}

func isPrivacyExecutionEvent(eventType string) bool {
	switch eventType {
	case privacytypes.EventTypeDeposit, privacytypes.EventTypeWithdraw, privacytypes.EventTypeShieldedTransfer, privacytypes.EventTypeBatchTransferV1:
		return true
	default:
		return false
	}
}

func eventTypeForKind(kind auditfield.Kind) string {
	switch kind {
	case 1:
		return privacytypes.EventTypeDeposit
	case 2:
		return privacytypes.EventTypeWithdraw
	case 3:
		return privacytypes.EventTypeShieldedTransfer
	case 4:
		return privacytypes.EventTypeBatchTransferV1
	default:
		return ""
	}
}

func canonicalUint(raw string) (uint64, error) {
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, fmt.Errorf("noncanonical unsigned integer")
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || strconv.FormatUint(value, 10) != raw {
		return 0, fmt.Errorf("noncanonical unsigned integer")
	}
	return value, nil
}

func canonicalHex32(raw string) ([32]byte, error) {
	var result [32]byte
	if len(raw) != hex.EncodedLen(len(result)) {
		return result, fmt.Errorf("expected 32 lowercase hex bytes")
	}
	count, err := hex.Decode(result[:], []byte(raw))
	if err != nil || count != len(result) || hex.EncodeToString(result[:]) != raw {
		return [32]byte{}, fmt.Errorf("expected 32 lowercase hex bytes")
	}
	return result, nil
}
