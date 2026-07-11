package app

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBatchTransferRawFramingRejectsDuplicateFieldWireCapBypass(t *testing.T) {
	oversizedProof := bytes.Repeat([]byte{0x41}, privacytypes.MaxBatchTransferMessageBytesV1)
	validProof := bytes.Repeat([]byte{0x42}, privacytypes.BatchTransferProofSizeV1)
	rawMessage := appendProtoBytesField(nil, 2, oversizedProof)
	rawMessage = appendProtoBytesField(rawMessage, 2, validProof)
	require.Greater(t, len(rawMessage), privacytypes.MaxBatchTransferMessageBytesV1)

	// The generated decoder keeps the last singular proof value, demonstrating
	// why the decoded Size() check alone cannot enforce the raw wire cap.
	var decoded privacytypes.MsgBatchTransfer
	require.NoError(t, decoded.Unmarshal(rawMessage))
	require.Equal(t, validProof, decoded.Proof)
	require.Less(t, decoded.Size(), privacytypes.MaxBatchTransferMessageBytesV1)

	ctx := sdk.Context{}.WithTxBytes(batchRawTxBytes(t, rawMessage))
	nextCalled := false
	_, err := (BatchTransferRawFramingDecorator{}).AnteHandle(ctx, nil, false, func(nextCtx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		nextCalled = true
		return nextCtx, nil
	})
	require.ErrorContains(t, err, "raw Any.value hard cap")
	require.False(t, nextCalled)
}

func TestBatchTransferRawFramingAllowsBoundedBatchAndIgnoresOtherTypes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		typeURL string
		value   []byte
	}{
		{
			name:    "bounded batch",
			typeURL: batchTransferMsgTypeURL,
			value:   bytes.Repeat([]byte{0x11}, privacytypes.MaxBatchTransferMessageBytesV1),
		},
		{
			name:    "other message uses chain-level tx bound",
			typeURL: "/example.v1.Other",
			value:   bytes.Repeat([]byte{0x22}, privacytypes.MaxBatchTransferMessageBytesV1+1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &txtypes.TxBody{Messages: []*types.Any{{TypeUrl: tc.typeURL, Value: tc.value}}}
			bodyBytes, err := body.Marshal()
			require.NoError(t, err)
			rawBytes, err := (&txtypes.TxRaw{BodyBytes: bodyBytes}).Marshal()
			require.NoError(t, err)
			require.NoError(t, validateRawBatchTransferMessageSizes(rawBytes))
		})
	}
}

func batchRawTxBytes(t testing.TB, messageValue []byte) []byte {
	t.Helper()
	body := &txtypes.TxBody{Messages: []*types.Any{{
		TypeUrl: batchTransferMsgTypeURL,
		Value:   messageValue,
	}}}
	bodyBytes, err := body.Marshal()
	require.NoError(t, err)
	rawBytes, err := (&txtypes.TxRaw{BodyBytes: bodyBytes}).Marshal()
	require.NoError(t, err)
	return rawBytes
}

func appendProtoBytesField(dst []byte, fieldNumber uint64, value []byte) []byte {
	dst = binary.AppendUvarint(dst, fieldNumber<<3|2)
	dst = binary.AppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}
