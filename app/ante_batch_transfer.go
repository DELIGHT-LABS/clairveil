package app

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/protobuf/encoding/protowire"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	govSubmitProposalMsgTypeURL = "/cosmos.gov.v1.MsgSubmitProposal"
	authzExecMsgTypeURL         = "/cosmos.authz.v1beta1.MsgExec"
	maxBatchWrapperDepth        = 8
)

var batchTransferMsgTypeURL = sdk.MsgTypeURL(&privacytypes.MsgBatchTransfer{})

// BatchTransferRawFramingDecorator enforces the per-message wire cap against
// the signed Any.value bytes. Msg.Size() only sees the decoded last value for a
// duplicate singular protobuf field and therefore remains a secondary decoded
// shape check, not the consensus raw-wire bound.
type BatchTransferRawFramingDecorator struct{}

func (BatchTransferRawFramingDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if err := validateRawBatchTransferMessageSizes(ctx.TxBytes()); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

func validateRawBatchTransferMessageSizes(txBytes []byte) error {
	// BaseApp supplies the signed TxRaw bytes on consensus paths. Keeping the
	// decorator a no-op for an empty direct-test context preserves composability
	// for ante unit tests that do not model BaseApp decoding.
	if len(txBytes) == 0 {
		return nil
	}

	var raw txtypes.TxRaw
	if err := raw.Unmarshal(txBytes); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "decode TxRaw for batch framing: %v", err)
	}
	var decodedBody txtypes.TxBody
	if err := decodedBody.Unmarshal(raw.BodyBytes); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "decode TxBody for batch framing: %v", err)
	}

	return inspectRawEmbeddedAnys(raw.BodyBytes, 1, 0, "tx body")
}

func inspectRawEmbeddedAnys(message []byte, anyFieldNumber protowire.Number, depth int, path string) error {
	index := 0
	return visitRawProtoFields(message, func(number protowire.Number, wireType protowire.Type, value []byte) error {
		if number != anyFieldNumber {
			return nil
		}
		if wireType != protowire.BytesType {
			return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "%s message %d has wire type %d", path, index, wireType)
		}
		currentPath := fmt.Sprintf("%s message %d", path, index)
		index++
		return inspectRawAny(value, depth, currentPath)
	})
}

func inspectRawAny(rawAny []byte, depth int, path string) error {
	var (
		typeURL      string
		typeURLCount int
		lastValue    []byte
		valueCount   int
	)
	if err := visitRawProtoFields(rawAny, func(number protowire.Number, wireType protowire.Type, value []byte) error {
		switch number {
		case 1:
			if wireType != protowire.BytesType {
				return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "%s type_url has wire type %d", path, wireType)
			}
			typeURL = string(value)
			typeURLCount++
		case 2:
			if wireType != protowire.BytesType {
				return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "%s value has wire type %d", path, wireType)
			}
			lastValue = value
			valueCount++
		}
		return nil
	}); err != nil {
		return err
	}

	if typeURL == batchTransferMsgTypeURL {
		if typeURLCount != 1 || valueCount != 1 {
			return errorsmod.Wrapf(
				sdkerrors.ErrTxDecode,
				"%s batch transfer Any must contain exactly one type_url and one raw value; got %d and %d",
				path,
				typeURLCount,
				valueCount,
			)
		}
		if len(lastValue) > privacytypes.MaxBatchTransferMessageBytesV1 {
			return errorsmod.Wrap(
				sdkerrors.ErrTxTooLarge,
				fmt.Sprintf(
					"%s batch transfer exceeds %d-byte raw Any.value hard cap: got %d",
					path,
					privacytypes.MaxBatchTransferMessageBytesV1,
					len(lastValue),
				),
			)
		}
		return nil
	}

	if valueCount == 0 {
		return nil
	}
	var nestedAnyField protowire.Number
	switch typeURL {
	case govSubmitProposalMsgTypeURL:
		nestedAnyField = 1
	case authzExecMsgTypeURL:
		nestedAnyField = 2
	default:
		return nil
	}
	if depth >= maxBatchWrapperDepth {
		return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "%s exceeds nested message depth %d", path, maxBatchWrapperDepth)
	}
	return inspectRawEmbeddedAnys(lastValue, nestedAnyField, depth+1, path+" nested")
}

func visitRawProtoFields(message []byte, visit func(protowire.Number, protowire.Type, []byte) error) error {
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "decode raw protobuf tag: %v", protowire.ParseError(tagLength))
		}
		message = message[tagLength:]

		var (
			value       []byte
			fieldLength int
		)
		if wireType == protowire.BytesType {
			value, fieldLength = protowire.ConsumeBytes(message)
		} else {
			fieldLength = protowire.ConsumeFieldValue(number, wireType, message)
		}
		if fieldLength < 0 {
			return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "decode raw protobuf field %d: %v", number, protowire.ParseError(fieldLength))
		}
		if err := visit(number, wireType, value); err != nil {
			return err
		}
		message = message[fieldLength:]
	}
	return nil
}
