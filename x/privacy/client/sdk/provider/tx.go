package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

type CosmosTxBroadcaster struct {
	ClientContext client.Context
	Flags         *pflag.FlagSet
	FromName      string
}

type CosmosTxBroadcastResult struct {
	Response        *sdk.TxResponse
	TxBytesHash     string
	SignDocHash     string
	AccountSequence uint64
}

// CosmosSignedTx is the immutable signed transaction artifact used by
// idempotent broadcast workers. TxHash is the CometBFT SHA-256 transaction
// hash and TxBytesHash binds the exact bytes that must be retried.
type CosmosSignedTx struct {
	Bytes           []byte
	TxBytesHash     string
	SignDocHash     string
	TxHash          string
	AccountSequence uint64
}

// PreparedCosmosTxBroadcast separates fallible account/gas/signing work from
// the external BroadcastTx boundary while retaining a deterministic tx identity.
type PreparedCosmosTxBroadcast struct {
	TxBytes []byte
	Result  CosmosTxBroadcastResult
}

func (b CosmosTxBroadcaster) PrepareFactory(msg sdk.Msg) (tx.Factory, error) {
	return b.PrepareFactoryForMessages(msg)
}

func (b CosmosTxBroadcaster) PrepareFactoryForMessages(msgs ...sdk.Msg) (tx.Factory, error) {
	if len(msgs) == 0 {
		return tx.Factory{}, fmt.Errorf("at least one sdk message is required to prepare a tx factory")
	}
	for _, msg := range msgs {
		if msg == nil {
			return tx.Factory{}, fmt.Errorf("sdk messages must not be nil")
		}
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
		_, adjusted, err := tx.CalculateGas(b.ClientContext, txf, msgs...)
		if err != nil {
			return txf, fmt.Errorf("failed to calculate tx gas: %w", err)
		}
		txf = txf.WithGas(adjusted)
	}

	return txf, nil
}

func (b CosmosTxBroadcaster) BroadcastSDKMessage(ctx context.Context, msg sdk.Msg) (*sdk.TxResponse, error) {
	return b.BroadcastSDKMessages(ctx, msg)
}

func (b CosmosTxBroadcaster) BroadcastSDKMessages(ctx context.Context, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
	result, err := b.BroadcastSDKMessagesWithMetadata(ctx, msgs...)
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

func (b CosmosTxBroadcaster) BroadcastSDKMessagesWithMetadata(ctx context.Context, msgs ...sdk.Msg) (*CosmosTxBroadcastResult, error) {
	prepared, err := b.PrepareSDKMessagesWithMetadata(ctx, msgs...)
	if err != nil {
		return nil, err
	}
	return b.BroadcastPreparedSDKMessages(ctx, prepared)
}

// BuildSignedSDKMessages signs once without broadcasting. Callers can durably
// store Bytes before network submission and reuse them byte-for-byte after an
// ambiguous response.
func (b CosmosTxBroadcaster) BuildSignedSDKMessages(ctx context.Context, msgs ...sdk.Msg) (*CosmosSignedTx, error) {
	txf, err := b.PrepareFactoryForMessages(msgs...)
	if err != nil {
		return nil, err
	}

	fromName, err := b.resolveFromName()
	if err != nil {
		return nil, err
	}

	txBuilder, err := txf.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := tx.Sign(ctx, txf, fromName, txBuilder, true); err != nil {
		return nil, err
	}
	signDocHash, err := b.signDocHash(ctx, txf, txBuilder)
	if err != nil {
		return nil, err
	}

	txBytes, err := b.ClientContext.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	txBytesHash := sha256Hex(txBytes)
	return &CosmosSignedTx{
		Bytes:           append([]byte(nil), txBytes...),
		TxBytesHash:     txBytesHash,
		SignDocHash:     signDocHash,
		TxHash:          strings.ToUpper(txBytesHash),
		AccountSequence: txf.Sequence(),
	}, nil
}

func (b CosmosTxBroadcaster) PrepareSDKMessagesWithMetadata(ctx context.Context, msgs ...sdk.Msg) (*PreparedCosmosTxBroadcast, error) {
	signed, err := b.BuildSignedSDKMessages(ctx, msgs...)
	if err != nil {
		return nil, err
	}
	return &PreparedCosmosTxBroadcast{
		TxBytes: append([]byte(nil), signed.Bytes...),
		Result: CosmosTxBroadcastResult{
			TxBytesHash:     signed.TxBytesHash,
			SignDocHash:     signed.SignDocHash,
			AccountSequence: signed.AccountSequence,
		},
	}, nil
}

// BroadcastSignedTxBytes submits the supplied immutable bytes without signing
// or refreshing account state.
func (b CosmosTxBroadcaster) BroadcastSignedTxBytes(ctx context.Context, txBytes []byte) (*sdk.TxResponse, error) {
	if len(txBytes) == 0 {
		return nil, fmt.Errorf("signed tx bytes are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	clientContext := b.ClientContext.WithCmdContext(ctx)
	return clientContext.BroadcastTx(append([]byte(nil), txBytes...))
}

func (b CosmosTxBroadcaster) BroadcastPreparedSDKMessages(_ context.Context, prepared *PreparedCosmosTxBroadcast) (*CosmosTxBroadcastResult, error) {
	if prepared == nil || len(prepared.TxBytes) == 0 {
		return nil, fmt.Errorf("prepared tx bytes are required")
	}
	result := prepared.Result
	txBytes := append([]byte(nil), prepared.TxBytes...)
	if !strings.EqualFold(strings.TrimSpace(result.TxBytesHash), sha256Hex(txBytes)) {
		return &result, fmt.Errorf("prepared tx bytes hash mismatch")
	}
	response, err := b.ClientContext.BroadcastTx(txBytes)
	if err != nil {
		return &result, err
	}
	result.Response = response
	return &result, nil
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

func (b CosmosTxBroadcaster) signDocHash(ctx context.Context, txf tx.Factory, txBuilder client.TxBuilder) (string, error) {
	signatures, err := txBuilder.GetTx().GetSignaturesV2()
	if err != nil {
		return "", err
	}
	if len(signatures) == 0 {
		return "", nil
	}
	signature := signatures[0]
	single, ok := signature.Data.(*signingtypes.SingleSignatureData)
	if !ok || signature.PubKey == nil {
		return "", nil
	}
	signerData := authsigning.SignerData{
		ChainID:       txf.ChainID(),
		AccountNumber: txf.AccountNumber(),
		Sequence:      txf.Sequence(),
		PubKey:        signature.PubKey,
		Address:       sdk.AccAddress(signature.PubKey.Address()).String(),
	}
	signBytes, err := authsigning.GetSignBytesAdapter(ctx, b.ClientContext.TxConfig.SignModeHandler(), single.SignMode, signerData, txBuilder.GetTx())
	if err != nil {
		return "", err
	}
	return sha256Hex(signBytes), nil
}

func sha256Hex(bz []byte) string {
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:])
}
