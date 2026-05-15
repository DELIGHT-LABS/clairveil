package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type CosmosTxBroadcaster struct {
	ClientContext client.Context
	Flags         *pflag.FlagSet
	FromName      string
}

func (b CosmosTxBroadcaster) PrepareFactory(msg sdk.Msg) (tx.Factory, error) {
	if msg == nil {
		return tx.Factory{}, fmt.Errorf("an sdk message is required to prepare a tx factory")
	}
	if b.Flags == nil {
		return tx.Factory{}, fmt.Errorf("tx flags are required to prepare a tx factory")
	}
	if b.ClientContext.TxConfig == nil {
		return tx.Factory{}, fmt.Errorf("tx config is required to prepare a tx factory")
	}
	if b.ClientContext.AccountRetriever == nil {
		return tx.Factory{}, fmt.Errorf("account retriever is required to prepare a tx factory")
	}

	fromAddress := b.ClientContext.GetFromAddress()
	if fromAddress.Empty() {
		return tx.Factory{}, fmt.Errorf("from address is required to prepare a tx factory")
	}

	if _, err := b.resolveFromName(); err != nil {
		return tx.Factory{}, err
	}

	txf, _ := tx.NewFactoryCLI(b.ClientContext, b.Flags)
	txf = txf.WithTxConfig(b.ClientContext.TxConfig).WithAccountRetriever(b.ClientContext.AccountRetriever)

	if err := txf.AccountRetriever().EnsureExists(b.ClientContext, fromAddress); err != nil {
		return txf, err
	}
	initNum, initSeq, err := txf.AccountRetriever().GetAccountNumberSequence(b.ClientContext, fromAddress)
	if err != nil {
		return txf, err
	}
	txf = txf.WithAccountNumber(initNum).WithSequence(initSeq)

	if txf.Gas() == flags.DefaultGasLimit || txf.Gas() == 0 {
		txf = txf.WithGasAdjustment(1.5)
		_, adjusted, err := tx.CalculateGas(b.ClientContext, txf, msg)
		if err != nil {
			return txf, fmt.Errorf("failed to calculate tx gas: %w", err)
		}
		txf = txf.WithGas(adjusted)
	}

	return txf, nil
}

func (b CosmosTxBroadcaster) BroadcastSDKMessage(ctx context.Context, msg sdk.Msg) (*sdk.TxResponse, error) {
	txf, err := b.PrepareFactory(msg)
	if err != nil {
		return nil, err
	}

	fromName, err := b.resolveFromName()
	if err != nil {
		return nil, err
	}

	txBuilder, err := txf.BuildUnsignedTx(msg)
	if err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := tx.Sign(ctx, txf, fromName, txBuilder, true); err != nil {
		return nil, err
	}

	txBytes, err := b.ClientContext.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	return b.ClientContext.BroadcastTx(txBytes)
}

func (b CosmosTxBroadcaster) GenerateOrBroadcast(msgs ...sdk.Msg) error {
	if len(msgs) == 0 {
		return fmt.Errorf("at least one sdk message is required to generate or broadcast a tx")
	}
	if b.Flags == nil {
		return fmt.Errorf("tx flags are required to generate or broadcast a tx")
	}

	return tx.GenerateOrBroadcastTxCLI(b.ClientContext, b.Flags, msgs...)
}

func (b CosmosTxBroadcaster) resolveFromName() (string, error) {
	fromName := strings.TrimSpace(b.FromName)
	if fromName == "" {
		fromName = strings.TrimSpace(b.ClientContext.GetFromName())
	}
	if fromName == "" {
		return "", fmt.Errorf("from name is required to sign the tx")
	}
	return fromName, nil
}
