package app

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
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
	var body txtypes.TxBody
	if err := body.Unmarshal(raw.BodyBytes); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrTxDecode, "decode TxBody for batch framing: %v", err)
	}
	for i, message := range body.Messages {
		if message == nil || message.TypeUrl != batchTransferMsgTypeURL {
			continue
		}
		if len(message.Value) > privacytypes.MaxBatchTransferMessageBytesV1 {
			return errorsmod.Wrap(
				sdkerrors.ErrTxTooLarge,
				fmt.Sprintf(
					"batch transfer message %d exceeds %d-byte raw Any.value hard cap: got %d",
					i,
					privacytypes.MaxBatchTransferMessageBytesV1,
					len(message.Value),
				),
			)
		}
	}
	return nil
}
