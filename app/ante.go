package app

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
)

// NewAnteHandler returns the standard signature/sequence validation chain.
func NewAnteHandler(options ante.HandlerOptions) (sdk.AnteHandler, error) {
	return newAnteHandler(options, true)
}
func newAnteHandler(options ante.HandlerOptions, legacyFraming bool) (sdk.AnteHandler, error) {
	if options.AccountKeeper == nil {
		return nil, errors.New("account keeper is required for ante builder")
	}
	if options.BankKeeper == nil {
		return nil, errors.New("bank keeper is required for ante builder")
	}
	if options.SignModeHandler == nil {
		return nil, errors.New("sign mode handler is required for ante builder")
	}

	decorators := []sdk.AnteDecorator{ante.NewSetUpContextDecorator()}
	if legacyFraming {
		decorators = append(decorators, BatchTransferRawFramingDecorator{})
	}
	decorators = append(decorators,
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
	)
	if !legacyFraming {
		decorators = append(decorators, ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker))
	}
	decorators = append(decorators,
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
	)
	return sdk.ChainAnteDecorators(decorators...), nil
}
