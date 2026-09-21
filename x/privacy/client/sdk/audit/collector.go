package audit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// ExecutionEvent is the minimal event evidence emitted only after a privacy
// transaction commits successfully. It is deliberately not a scan record.
type ExecutionEvent struct {
	Height         uint64
	TxIndex        uint32
	TxHash         []byte
	EventType      string
	GlobalSequence uint64
	MessageIndex   uint32
	ExecutionID    []byte
	// Funder is set only for a trusted delegated V2 deposit. Creator remains
	// the proof-bound principal and provenance identity in Message.
	Funder string
}

// SuccessfulAuditTx supplies the original delivered transaction message and
// its matching successful execution event. Implementations obtain both from a
// trusted RPC/indexer source; callers must not synthesize either from scan
// rows or from mempool data.
type SuccessfulAuditTx struct {
	Message sdk.Msg
	Event   ExecutionEvent
}

// SuccessfulAuditTxSource pages original successful transactions together
// with their execution events. The cursor is the last observed global
// sequence; absent sequences are not filled and are reported by consumers as
// incomplete evidence when their policy requires continuity.
type SuccessfulAuditTxSource interface {
	SuccessfulAuditTransactions(context.Context, uint64) ([]SuccessfulAuditTx, error)
}

// AuditKeyHistory resolves the immutable public audit key history required to
// interpret a delivered transaction's epoch. It is independent of the tx
// source and must be pinned to the same trusted chain view.
type AuditKeyHistory interface {
	AuditKey(context.Context, uint64) (KeyRecord, error)
}

type Collector struct {
	Network [32]byte
	Keys    AuditKeyHistory
}

type CollectionFailure struct{ Err error }

func (e CollectionFailure) Error() string { return e.Err.Error() }
func (e CollectionFailure) Unwrap() error { return e.Err }

type ProvenanceFailure struct{ Err error }

func (e ProvenanceFailure) Error() string { return e.Err.Error() }
func (e ProvenanceFailure) Unwrap() error { return e.Err }

// CollectedAuditTx is validated provenance from an original successful tx and
// execution event. It contains no reconstructed transition record and no
// on-chain replay/index payload.
type CollectedAuditTx struct {
	Height         uint64
	TxIndex        uint32
	TxHash         [32]byte
	GlobalSequence uint64
	MessageIndex   uint32
	ExecutionID    [32]byte
	Funder         string
	Message        privacytypes.ValidatedAuditMessage
	Key            KeyRecord
}

// Collect validates the original messages, their historical audit keys, and
// the execution-event binding. It accepts non-contiguous global sequences so
// a caller can explicitly surface incomplete source evidence rather than
// silently filling gaps.
func (c Collector) Collect(ctx context.Context, source SuccessfulAuditTxSource, after uint64) ([]CollectedAuditTx, error) {
	if source == nil || c.Keys == nil || c.Network == [32]byte{} {
		return nil, fmt.Errorf("collector source, key history, and network are required")
	}
	rows, err := source.SuccessfulAuditTransactions(ctx, after)
	if err != nil {
		return nil, CollectionFailure{Err: err}
	}
	result := make([]CollectedAuditTx, 0, len(rows))
	seen := make(map[uint64]struct{}, len(rows))
	for index, row := range rows {
		message, err := privacytypes.ValidateAuditMessage(row.Message)
		if err != nil {
			return nil, fmt.Errorf("successful audit tx %d has invalid message: %w", index, err)
		}
		event := row.Event
		if event.Height == 0 || event.GlobalSequence <= after || len(event.TxHash) != 32 || len(event.ExecutionID) != 32 || event.EventType != eventTypeForKind(message.Kind()) {
			return nil, fmt.Errorf("successful audit tx %d has invalid execution event", index)
		}
		if event.Funder != "" {
			funder, err := sdk.AccAddressFromBech32(event.Funder)
			if err != nil || funder.String() != event.Funder || message.Kind() != auditfield.KindDeposit || funder.Equals(authtypes.NewModuleAddress(privacytypes.ModuleName)) || funder.Equals(authtypes.NewModuleAddress(govtypes.ModuleName)) {
				return nil, fmt.Errorf("successful audit tx %d has invalid delegated funder", index)
			}
		}
		if _, duplicate := seen[event.GlobalSequence]; duplicate {
			return nil, fmt.Errorf("duplicate successful audit global sequence %d", event.GlobalSequence)
		}
		seen[event.GlobalSequence] = struct{}{}
		key, err := c.Keys.AuditKey(ctx, message.Epoch())
		if err != nil {
			return nil, ProvenanceFailure{Err: fmt.Errorf("audit key epoch %d: %w", message.Epoch(), err)}
		}
		if key.Epoch != message.Epoch() || key.Key.ID() != message.KeyID() {
			return nil, ProvenanceFailure{Err: fmt.Errorf("successful audit tx %d key history mismatch", index)}
		}
		var txHash [32]byte
		copy(txHash[:], event.TxHash)
		executionID := collectedExecutionID(c.Network, txHash, event.GlobalSequence, event.MessageIndex)
		if string(executionID[:]) != string(event.ExecutionID) {
			return nil, fmt.Errorf("successful audit tx %d execution ID mismatch", index)
		}
		result = append(result, CollectedAuditTx{Height: event.Height, TxIndex: event.TxIndex, TxHash: txHash, GlobalSequence: event.GlobalSequence, MessageIndex: event.MessageIndex, ExecutionID: executionID, Funder: event.Funder, Message: message, Key: key})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GlobalSequence < result[j].GlobalSequence })
	return result, nil
}

func collectedExecutionID(network, txHash [32]byte, global uint64, messageIndex uint32) [32]byte {
	var encoded [16]byte
	binary.BigEndian.PutUint64(encoded[:8], global)
	binary.BigEndian.PutUint64(encoded[8:], uint64(messageIndex))
	return sha256.Sum256(append(append(append(append([]byte("clairveil/privacy/execution-id/v1"), network[:]...), txHash[:]...), encoded[:]...), byte(1)))
}

func (r CollectedAuditTx) Kind() auditfield.Kind { return r.Message.Kind() }
