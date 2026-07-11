package payroll

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	txtypes "github.com/cosmos/cosmos-sdk/types/tx"

	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// CosmosBatchTxAdapter connects the durable one-proof broadcaster to the
// ordinary Cosmos SDK signer while keeping signing and network submission as
// separate operations.
type CosmosBatchTxAdapter struct {
	Broadcaster privacyprovider.CosmosTxBroadcaster
}

func (a CosmosBatchTxAdapter) BuildSignedBatchTx(ctx context.Context, msg *privacytypes.MsgBatchTransfer) (*SignedBatchTx, error) {
	if msg == nil {
		return nil, fmt.Errorf("batch transfer message is required")
	}
	signed, err := a.Broadcaster.BuildSignedSDKMessages(ctx, msg)
	if err != nil {
		return nil, err
	}
	return &SignedBatchTx{
		Bytes: append([]byte(nil), signed.Bytes...), TxBytesHash: signed.TxBytesHash,
		SignDocHash: signed.SignDocHash, TxHash: signed.TxHash, AccountSequence: signed.AccountSequence,
	}, nil
}

func (a CosmosBatchTxAdapter) BroadcastSignedBatchTx(ctx context.Context, signedTxBytes []byte) (*BatchBroadcastReceipt, error) {
	response, err := a.Broadcaster.BroadcastSignedTxBytes(ctx, signedTxBytes)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("Cosmos broadcast returned no response")
	}
	return &BatchBroadcastReceipt{TxHash: response.TxHash, Height: response.Height, Code: response.Code, RawLog: response.RawLog}, nil
}

type BatchTxService interface {
	GetTx(ctx context.Context, in *txtypes.GetTxRequest, opts ...grpc.CallOption) (*txtypes.GetTxResponse, error)
}

type BatchNullifierStatusProvider interface {
	CheckNullifiersUsed(ctx context.Context, nullifierHexes []string) (map[string]bool, error)
}

// CosmosBatchChainReconciler checks the canonical tx service before querying
// the typed batch nullifier endpoint. It treats only an explicit gRPC NotFound
// as absence; transport and server errors remain unknown.
type CosmosBatchChainReconciler struct {
	TxService  BatchTxService
	Nullifiers BatchNullifierStatusProvider
}

func (r CosmosBatchChainReconciler) LookupBatchTx(ctx context.Context, txHash string) (*BatchTxLookupResult, error) {
	if r.TxService == nil {
		return nil, fmt.Errorf("Cosmos tx service is required")
	}
	txHash = strings.TrimSpace(strings.TrimPrefix(txHash, "0x"))
	if txHash == "" {
		return nil, fmt.Errorf("tx hash is required")
	}
	response, err := r.TxService.GetTx(ctx, &txtypes.GetTxRequest{Hash: txHash})
	if status.Code(err) == codes.NotFound {
		return &BatchTxLookupResult{Found: false}, nil
	}
	if err != nil {
		return nil, err
	}
	if response == nil || response.TxResponse == nil {
		return nil, fmt.Errorf("Cosmos tx lookup returned no response")
	}
	txResponse := response.TxResponse
	return &BatchTxLookupResult{
		Found: true, Succeeded: txResponse.Code == 0, Failed: txResponse.Code != 0,
		TxHash: txResponse.TxHash, Height: txResponse.Height, Code: txResponse.Code,
	}, nil
}

func (r CosmosBatchChainReconciler) CheckBatchNullifiers(ctx context.Context, nullifierHexes []string) (map[string]bool, error) {
	if r.Nullifiers == nil {
		return nil, fmt.Errorf("typed batch nullifier provider is required")
	}
	return r.Nullifiers.CheckNullifiersUsed(ctx, nullifierHexes)
}
