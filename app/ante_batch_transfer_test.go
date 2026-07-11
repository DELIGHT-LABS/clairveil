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

func TestBatchTransferRawFramingRejectsDuplicateAnyValueCapBypass(t *testing.T) {
	oversizedValue := bytes.Repeat([]byte{0x41}, privacytypes.MaxBatchTransferMessageBytesV1+1)
	boundedValue := bytes.Repeat([]byte{0x42}, privacytypes.BatchTransferProofSizeV1)
	rawAny := rawAnyBytes(batchTransferMsgTypeURL, oversizedValue, boundedValue)

	var decoded types.Any
	require.NoError(t, decoded.Unmarshal(rawAny))
	require.Equal(t, boundedValue, decoded.Value)

	err := validateRawBatchTransferMessageSizes(rawTxBytesWithRawAny(t, rawAny))
	require.ErrorContains(t, err, "exactly one type_url and one raw value")
}

func TestBatchTransferRawFramingRejectsNestedWrapperCapBypass(t *testing.T) {
	oversizedValue := bytes.Repeat([]byte{0x41}, privacytypes.MaxBatchTransferMessageBytesV1+1)
	innerBatchAny := rawAnyBytes(batchTransferMsgTypeURL, oversizedValue)

	for _, test := range []struct {
		name           string
		wrapperTypeURL string
		messagesField  uint64
	}{
		{name: "governance proposal", wrapperTypeURL: govSubmitProposalMsgTypeURL, messagesField: 1},
		{name: "authz exec", wrapperTypeURL: authzExecMsgTypeURL, messagesField: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			wrapperValue := appendProtoBytesField(nil, test.messagesField, innerBatchAny)
			wrapperAny := rawAnyBytes(test.wrapperTypeURL, wrapperValue)

			err := validateRawBatchTransferMessageSizes(rawTxBytesWithRawAny(t, wrapperAny))
			require.ErrorContains(t, err, "raw Any.value hard cap")
		})
	}
}

func TestBatchTransferRawFramingRejectsMalformedWire(t *testing.T) {
	t.Run("truncated bytes field", func(t *testing.T) {
		err := inspectRawAny([]byte{0x0a, 0x80}, 0, "test any")
		require.ErrorContains(t, err, "decode raw protobuf field")
	})

	t.Run("type url uses non-bytes wire type", func(t *testing.T) {
		err := inspectRawAny([]byte{0x08, 0x01}, 0, "test any")
		require.ErrorContains(t, err, "type_url has wire type 0")
	})

	t.Run("value uses non-bytes wire type", func(t *testing.T) {
		rawAny := appendProtoBytesField(nil, 1, []byte(batchTransferMsgTypeURL))
		rawAny = append(rawAny, 0x10, 0x01)
		err := inspectRawAny(rawAny, 0, "test any")
		require.ErrorContains(t, err, "value has wire type 0")
	})
}

func TestBatchTransferRawFramingEnforcesWrapperDepthBoundary(t *testing.T) {
	boundedBatchAny := rawAnyBytes(batchTransferMsgTypeURL, []byte{0x01})
	wrapped := boundedBatchAny
	for range maxBatchWrapperDepth {
		wrapperValue := appendProtoBytesField(nil, 2, wrapped)
		wrapped = rawAnyBytes(authzExecMsgTypeURL, wrapperValue)
	}
	require.NoError(t, validateRawBatchTransferMessageSizes(rawTxBytesWithRawAny(t, wrapped)))

	wrapperValue := appendProtoBytesField(nil, 2, wrapped)
	overDepth := rawAnyBytes(authzExecMsgTypeURL, wrapperValue)
	err := validateRawBatchTransferMessageSizes(rawTxBytesWithRawAny(t, overDepth))
	require.ErrorContains(t, err, "exceeds nested message depth 8")
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

func rawTxBytesWithRawAny(t testing.TB, rawAny []byte) []byte {
	t.Helper()
	bodyBytes := appendProtoBytesField(nil, 1, rawAny)
	rawBytes, err := (&txtypes.TxRaw{BodyBytes: bodyBytes}).Marshal()
	require.NoError(t, err)
	return rawBytes
}

func rawAnyBytes(typeURL string, values ...[]byte) []byte {
	raw := appendProtoBytesField(nil, 1, []byte(typeURL))
	for _, value := range values {
		raw = appendProtoBytesField(raw, 2, value)
	}
	return raw
}

func appendProtoBytesField(dst []byte, fieldNumber uint64, value []byte) []byte {
	dst = binary.AppendUvarint(dst, fieldNumber<<3|2)
	dst = binary.AppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}
