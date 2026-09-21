package audit

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

type sourceTestTx struct{ messages []sdk.Msg }

func (t sourceTestTx) GetMsgs() []sdk.Msg { return t.messages }
func (t sourceTestTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

type sourceMockRPC struct {
	blocks  map[int64]*coretypes.ResultBlock
	results map[int64]*coretypes.ResultBlockResults
	failAt  int64
	calls   []string
}

func (m *sourceMockRPC) Block(_ context.Context, height *int64) (*coretypes.ResultBlock, error) {
	m.calls = append(m.calls, fmt.Sprintf("block/%d", *height))
	if *height == m.failAt {
		return nil, errors.New("injected block failure")
	}
	return m.blocks[*height], nil
}

func (m *sourceMockRPC) BlockResults(_ context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	m.calls = append(m.calls, fmt.Sprintf("results/%d", *height))
	return m.results[*height], nil
}

func TestCosmosTxSourceCollectsSuccessfulTopLevelMessagesAndResumes(t *testing.T) {
	network := [32]byte{1}
	creator := sdk.AccAddress(make([]byte, 20)).String()
	key := testAuditKey(t)
	auth := func(kind auditfield.Kind) *privacyv2.AuditAuthorization {
		size, err := kind.EnvelopeSize()
		require.NoError(t, err)
		return &privacyv2.AuditAuthorization{KeyId: key.IDBytes(), Epoch: 1, Envelope: make([]byte, size)}
	}
	output := func(value uint64) *privacyv2.OutputEffect {
		return &privacyv2.OutputEffect{Commitment: auditfield.Field32FromUint64(value).Bytes(), Ciphertext: testEnvelope(t, privacytypes.EnvelopeDepositNoteV1)}
	}
	deposit1 := &privacyv2.MsgDeposit{Creator: creator, Amount: "1uclair", Output: output(1), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindDeposit)}
	deposit2 := &privacyv2.MsgDeposit{Creator: creator, Amount: "2uclair", Output: output(2), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindDeposit)}
	failed := &privacyv2.MsgDeposit{Creator: creator, Amount: "3uclair", Output: output(3), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindDeposit)}
	rawMulti, rawFailed, rawProposal := cmttypes.Tx("multi"), cmttypes.Tx("failed"), cmttypes.Tx("proposal")
	decodes := map[string]sdk.Tx{
		"multi":    sourceTestTx{messages: []sdk.Msg{deposit1, deposit2}},
		"failed":   sourceTestTx{messages: []sdk.Msg{failed}},
		"proposal": sourceTestTx{messages: []sdk.Msg{&govv1.MsgSubmitProposal{}}},
	}
	decoder := func(raw []byte) (sdk.Tx, error) { return decodes[string(raw)], nil }
	event := func(raw cmttypes.Tx, global uint64, message uint32) abci.Event {
		var hash [32]byte
		copy(hash[:], raw.Hash())
		id := collectedExecutionID(network, hash, global, message)
		return abci.Event{Type: privacytypes.EventTypeDeposit, Attributes: []abci.EventAttribute{
			{Key: "execution_id", Value: hex.EncodeToString(id[:])},
			{Key: "global_sequence", Value: fmt.Sprint(global)},
			{Key: "message_index", Value: fmt.Sprint(message)},
			{Key: "tx_hash", Value: hex.EncodeToString(raw.Hash())},
		}}
	}
	rpc := &sourceMockRPC{blocks: map[int64]*coretypes.ResultBlock{}, results: map[int64]*coretypes.ResultBlockResults{}}
	rpc.blocks[10] = sourceBlock(10, "audit-chain", cmttypes.Txs{rawMulti, rawFailed, rawProposal})
	rpc.results[10] = &coretypes.ResultBlockResults{Height: 10, TxsResults: []*abci.ExecTxResult{
		{Events: []abci.Event{event(rawMulti, 5, 0), event(rawMulti, 6, 1)}},
		{Code: 9, Events: []abci.Event{event(rawFailed, 7, 0)}},
		{Events: []abci.Event{{Type: privacytypes.EventTypeDeposit}}},
	}}
	rpc.blocks[11] = sourceBlock(11, "audit-chain", nil)
	rpc.results[11] = &coretypes.ResultBlockResults{Height: 11}
	cache := &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}
	source := CosmosTxSource{RPC: rpc, Decoder: decoder, Cache: cache, Network: network, ChainID: "audit-chain", FromHeight: 10, ToHeight: 11}

	rows, err := source.SuccessfulAuditTransactions(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, []uint32{0, 1}, []uint32{rows[0].Event.MessageIndex, rows[1].Event.MessageIndex})
	require.Equal(t, []uint64{5, 6}, []uint64{rows[0].Event.GlobalSequence, rows[1].Event.GlobalSequence})
	require.Len(t, rpc.calls, 4)

	rpc.calls = nil
	rows, err = source.SuccessfulAuditTransactions(context.Background(), 5)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 6, rows[0].Event.GlobalSequence)
	require.Empty(t, rpc.calls, "a complete cache must not refetch blocks")
	state, err := cache.Load(CosmosCacheIdentity{Network: network, ChainID: "audit-chain", FromHeight: 10, ToHeight: 11})
	require.NoError(t, err)
	require.EqualValues(t, 11, state.LastProcessedHeight, "empty blocks advance the checkpoint")
	require.Len(t, state.Transactions, 1, "cache retains only successful privacy execution tx/results")
	require.Equal(t, []byte(rawMulti), state.Transactions[0].Raw)
}

func TestCosmosTxSourceRejectsSuccessfulPrivacyMessageWithoutExecutionEvent(t *testing.T) {
	network := [32]byte{4}
	creator := sdk.AccAddress(make([]byte, 20)).String()
	key := testAuditKey(t)
	size, err := auditfield.KindDeposit.EnvelopeSize()
	require.NoError(t, err)
	message := &privacyv2.MsgDeposit{
		Creator: creator, Amount: "1uclair",
		Output:        &privacyv2.OutputEffect{Commitment: auditfield.Field32FromUint64(1).Bytes(), Ciphertext: testEnvelope(t, privacytypes.EnvelopeDepositNoteV1)},
		Proof:         make([]byte, 164),
		ExpiresAtUnix: 1,
		Audit:         &privacyv2.AuditAuthorization{KeyId: key.IDBytes(), Epoch: 1, Envelope: make([]byte, size)},
	}
	raw := cmttypes.Tx("missing-event")
	rpc := &sourceMockRPC{
		blocks:  map[int64]*coretypes.ResultBlock{1: sourceBlock(1, "missing-chain", cmttypes.Txs{raw})},
		results: map[int64]*coretypes.ResultBlockResults{1: {Height: 1, TxsResults: []*abci.ExecTxResult{{}}}},
	}
	source := CosmosTxSource{RPC: rpc, Decoder: func([]byte) (sdk.Tx, error) { return sourceTestTx{messages: []sdk.Msg{message}}, nil }, Cache: &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}, Network: network, ChainID: "missing-chain", FromHeight: 1, ToHeight: 1}
	_, err = source.SuccessfulAuditTransactions(context.Background(), 0)
	require.ErrorContains(t, err, "no matching execution event")
	state, loadErr := source.Cache.Load(CosmosCacheIdentity{Network: network, ChainID: "missing-chain", FromHeight: 1, ToHeight: 1})
	require.NoError(t, loadErr)
	require.Zero(t, state.LastProcessedHeight, "invalid execution evidence must not advance the checkpoint")
	require.Empty(t, state.Transactions)
}

func TestCosmosTxSourceResumesWithoutSkippingFailedHeight(t *testing.T) {
	network := [32]byte{2}
	rpc := &sourceMockRPC{blocks: map[int64]*coretypes.ResultBlock{}, results: map[int64]*coretypes.ResultBlockResults{}, failAt: 21}
	for _, height := range []int64{20, 21} {
		rpc.blocks[height] = sourceBlock(height, "resume-chain", nil)
		rpc.results[height] = &coretypes.ResultBlockResults{Height: height}
	}
	cache := &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}
	source := CosmosTxSource{RPC: rpc, Decoder: func([]byte) (sdk.Tx, error) { return nil, nil }, Cache: cache, Network: network, ChainID: "resume-chain", FromHeight: 20, ToHeight: 21}
	_, err := source.SuccessfulAuditTransactions(context.Background(), 0)
	require.ErrorContains(t, err, "block 21")
	state, err := cache.Load(CosmosCacheIdentity{Network: network, ChainID: "resume-chain", FromHeight: 20, ToHeight: 21})
	require.NoError(t, err)
	require.EqualValues(t, 20, state.LastProcessedHeight)

	rpc.failAt, rpc.calls = 0, nil
	_, err = source.SuccessfulAuditTransactions(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, []string{"block/21", "results/21"}, rpc.calls)
}

func TestCosmosTxSourceRejectsNestedProposalAsExecutionLeaf(t *testing.T) {
	network := [32]byte{3}
	raw := cmttypes.Tx("proposal")
	var hash [32]byte
	copy(hash[:], raw.Hash())
	id := collectedExecutionID(network, hash, 1, 0)
	event := abci.Event{Type: privacytypes.EventTypeDeposit, Attributes: []abci.EventAttribute{
		{Key: "execution_id", Value: hex.EncodeToString(id[:])}, {Key: "global_sequence", Value: "1"},
		{Key: "message_index", Value: "0"}, {Key: "tx_hash", Value: hex.EncodeToString(raw.Hash())},
	}}
	rpc := &sourceMockRPC{
		blocks:  map[int64]*coretypes.ResultBlock{1: sourceBlock(1, "nested-chain", cmttypes.Txs{raw})},
		results: map[int64]*coretypes.ResultBlockResults{1: {Height: 1, TxsResults: []*abci.ExecTxResult{{Events: []abci.Event{event}}}}},
	}
	source := CosmosTxSource{RPC: rpc, Decoder: func([]byte) (sdk.Tx, error) {
		return sourceTestTx{messages: []sdk.Msg{&govv1.MsgSubmitProposal{}}}, nil
	}, Cache: &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}, Network: network, ChainID: "nested-chain", FromHeight: 1, ToHeight: 1}
	_, err := source.SuccessfulAuditTransactions(context.Background(), 0)
	require.ErrorContains(t, err, "top-level audit message")
}

func TestCosmosTxSourceCollectsMultipleAuthenticatedDelegatedDeposits(t *testing.T) {
	network := [32]byte{8}
	creator := sdk.AccAddress(make([]byte, 20)).String()
	funder := sdk.AccAddress(append(make([]byte, 19), 9))
	key := testAuditKey(t)
	size, err := auditfield.KindDeposit.EnvelopeSize()
	require.NoError(t, err)
	deposit := func(value uint64) *privacyv2.MsgDeposit {
		return &privacyv2.MsgDeposit{
			Creator: creator, Amount: fmt.Sprintf("%duclair", value),
			Output: &privacyv2.OutputEffect{Commitment: auditfield.Field32FromUint64(value).Bytes(), Ciphertext: testEnvelope(t, privacytypes.EnvelopeDepositNoteV1)},
			Proof:  make([]byte, 164), ExpiresAtUnix: 1,
			Audit: &privacyv2.AuditAuthorization{KeyId: key.IDBytes(), Epoch: 1, Envelope: make([]byte, size)},
		}
	}
	deposit1, deposit2, failedDeposit := deposit(1), deposit(2), deposit(3)
	rawSuccess, rawFailed := cmttypes.Tx("delegated-success"), cmttypes.Tx("delegated-failed")
	event := func(raw cmttypes.Tx, global uint64, messageIndex uint32, message *privacyv2.MsgDeposit) abci.Event {
		payload, marshalErr := proto.Marshal(message)
		require.NoError(t, marshalErr)
		var hash [32]byte
		copy(hash[:], raw.Hash())
		id := collectedExecutionID(network, hash, global, messageIndex)
		return abci.Event{Type: privacytypes.EventTypeDeposit, Attributes: []abci.EventAttribute{
			{Key: "execution_id", Value: hex.EncodeToString(id[:])},
			{Key: "global_sequence", Value: fmt.Sprint(global)},
			{Key: "message_index", Value: fmt.Sprint(messageIndex)},
			{Key: "tx_hash", Value: hex.EncodeToString(raw.Hash())},
			{Key: privacytypes.AttributeKeyDelegatedDepositPayload, Value: base64.StdEncoding.EncodeToString(payload)},
			{Key: privacytypes.AttributeKeyDelegatedFunder, Value: funder.String()},
		}}
	}
	rpc := &sourceMockRPC{
		blocks: map[int64]*coretypes.ResultBlock{1: sourceBlock(1, "delegated-chain", cmttypes.Txs{rawSuccess, rawFailed})},
		results: map[int64]*coretypes.ResultBlockResults{1: {Height: 1, TxsResults: []*abci.ExecTxResult{
			{Events: []abci.Event{event(rawSuccess, 10, 0, deposit1), event(rawSuccess, 11, 0, deposit2)}},
			{Code: 9, Events: []abci.Event{event(rawFailed, 12, 0, failedDeposit)}},
		}}},
	}
	decoder := func(raw []byte) (sdk.Tx, error) {
		return sourceTestTx{messages: []sdk.Msg{&banktypes.MsgSend{}}}, nil
	}
	verified := 0
	source := CosmosTxSource{
		RPC: rpc, Decoder: decoder, Cache: &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")},
		Network: network, ChainID: "delegated-chain", FromHeight: 1, ToHeight: 1,
		VerifyDelegatedExecution: func(_ context.Context, evidence DelegatedExecutionEvidence) error {
			verified++
			require.Equal(t, rawSuccess, cmttypes.Tx(evidence.RawTx))
			require.Zero(t, evidence.MessageIndex)
			require.Equal(t, funder, evidence.Funder)
			return nil
		},
	}
	rows, err := source.SuccessfulAuditTransactions(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, 4, verified, "candidate validation and final extraction both fail closed")
	require.Equal(t, []uint64{10, 11}, []uint64{rows[0].Event.GlobalSequence, rows[1].Event.GlobalSequence})
	require.Equal(t, []uint32{0, 0}, []uint32{rows[0].Event.MessageIndex, rows[1].Event.MessageIndex})
	require.Equal(t, funder.String(), rows[0].Event.Funder)
	require.Equal(t, deposit1.Amount, rows[0].Message.(*privacyv2.MsgDeposit).Amount)

	rpc.calls = nil
	verified = 0
	rows, err = source.SuccessfulAuditTransactions(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 11, rows[0].Event.GlobalSequence)
	require.Empty(t, rpc.calls, "completed cache reuse must not refetch blocks")

	withoutVerifier := source
	withoutVerifier.Cache = &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}
	withoutVerifier.VerifyDelegatedExecution = nil
	_, err = withoutVerifier.SuccessfulAuditTransactions(context.Background(), 0)
	require.ErrorContains(t, err, "AUDIT_INCOMPLETE: delegated deposit execution verifier is required")

	rejected := source
	rejected.Cache = &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}
	rejected.VerifyDelegatedExecution = func(context.Context, DelegatedExecutionEvidence) error {
		return errors.New("EVM call reverted")
	}
	_, err = rejected.SuccessfulAuditTransactions(context.Background(), 0)
	require.ErrorContains(t, err, "AUDIT_INCOMPLETE")
	require.ErrorContains(t, err, "EVM call reverted")
	state, loadErr := rejected.Cache.Load(CosmosCacheIdentity{Network: network, ChainID: "delegated-chain", FromHeight: 1, ToHeight: 1})
	require.NoError(t, loadErr)
	require.Zero(t, state.LastProcessedHeight)
	require.Empty(t, state.Transactions)
}

func TestCosmosTxSourceRejectsDelegatedExecutionAndSequenceDuplicates(t *testing.T) {
	network := [32]byte{9}
	creator := sdk.AccAddress(make([]byte, 20)).String()
	funder := sdk.AccAddress(append(make([]byte, 19), 7))
	key := testAuditKey(t)
	size, err := auditfield.KindDeposit.EnvelopeSize()
	require.NoError(t, err)
	message := &privacyv2.MsgDeposit{
		Creator: creator, Amount: "1uclair",
		Output: &privacyv2.OutputEffect{Commitment: auditfield.Field32FromUint64(1).Bytes(), Ciphertext: testEnvelope(t, privacytypes.EnvelopeDepositNoteV1)},
		Proof:  make([]byte, 164), ExpiresAtUnix: 1,
		Audit: &privacyv2.AuditAuthorization{KeyId: key.IDBytes(), Epoch: 1, Envelope: make([]byte, size)},
	}
	payload, err := proto.Marshal(message)
	require.NoError(t, err)
	makeEvent := func(raw cmttypes.Tx, global uint64, index uint32) abci.Event {
		var hash [32]byte
		copy(hash[:], raw.Hash())
		id := collectedExecutionID(network, hash, global, index)
		return abci.Event{Type: privacytypes.EventTypeDeposit, Attributes: []abci.EventAttribute{
			{Key: "execution_id", Value: hex.EncodeToString(id[:])}, {Key: "global_sequence", Value: fmt.Sprint(global)},
			{Key: "message_index", Value: fmt.Sprint(index)}, {Key: "tx_hash", Value: hex.EncodeToString(raw.Hash())},
			{Key: privacytypes.AttributeKeyDelegatedDepositPayload, Value: base64.StdEncoding.EncodeToString(payload)},
			{Key: privacytypes.AttributeKeyDelegatedFunder, Value: funder.String()},
		}}
	}
	for _, tc := range []struct {
		name, want string
		indices    []uint32
	}{
		{name: "execution", want: "duplicate successful audit execution ID", indices: []uint32{0, 0}},
		{name: "sequence", want: "duplicate successful audit global sequence", indices: []uint32{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := cmttypes.Tx("delegated-duplicate-" + tc.name)
			events := []abci.Event{makeEvent(raw, 20, tc.indices[0]), makeEvent(raw, 20, tc.indices[1])}
			rpc := &sourceMockRPC{
				blocks:  map[int64]*coretypes.ResultBlock{1: sourceBlock(1, "duplicate-chain", cmttypes.Txs{raw})},
				results: map[int64]*coretypes.ResultBlockResults{1: {Height: 1, TxsResults: []*abci.ExecTxResult{{Events: events}}}},
			}
			source := CosmosTxSource{
				RPC: rpc, Decoder: func([]byte) (sdk.Tx, error) {
					return sourceTestTx{messages: []sdk.Msg{&banktypes.MsgSend{}, &banktypes.MsgSend{}}}, nil
				},
				Cache: &CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}, Network: network,
				ChainID: "duplicate-chain", FromHeight: 1, ToHeight: 1,
				VerifyDelegatedExecution: func(context.Context, DelegatedExecutionEvidence) error { return nil },
			}
			_, err := source.SuccessfulAuditTransactions(context.Background(), 0)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func sourceBlock(height int64, chainID string, txs cmttypes.Txs) *coretypes.ResultBlock {
	return &coretypes.ResultBlock{Block: &cmttypes.Block{Header: cmttypes.Header{Height: height, ChainID: chainID}, Data: cmttypes.Data{Txs: txs}}}
}
