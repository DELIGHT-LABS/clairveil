package batchtransfer

import (
	"fmt"
	"io"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// SecretBatchTransferOutputRequest is a pre-prover output with fixed field
// coordinates and Amount128 amount only.
type SecretBatchTransferOutputRequest struct {
	Kind           string
	SpendX, SpendY privacycrypto.FieldValue
	ViewX, ViewY   privacycrypto.FieldValue
	Amount         privacyamount.Amount128
}

// SecretBatchTransferRequest is the wallet-side batch selection. Existing
// PreparedBatchTransfer remains the legacy prover adapter.
type SecretBatchTransferRequest struct {
	Inputs  []privacyscan.SecretFoundNote
	Outputs []SecretBatchTransferOutputRequest
}

type SecretBatchTransferOutputs struct {
	Notes       []privacytypes.SecretNoteV1
	Commitments []privacycrypto.FieldValue
}

// PrepareSecretBatchTransferOutputs samples output randomness and computes all
// secret commitments in the fixed backend. Callers convert each result through
// ToProverWitnessV1 only while constructing the proof assignment.
func PrepareSecretBatchTransferOutputs(reader io.Reader, request SecretBatchTransferRequest) (*SecretBatchTransferOutputs, error) {
	if reader == nil {
		return nil, fmt.Errorf("secret note randomness reader is required")
	}
	if len(request.Inputs) == 0 || len(request.Outputs) == 0 {
		return nil, fmt.Errorf("batch requires inputs and outputs")
	}
	asset := request.Inputs[0].Note.AssetID
	assetBytes := asset.Bytes()
	var inputTotal, outputTotal privacyamount.Amount128
	var err error
	for i := range request.Inputs {
		if request.Inputs[i].Note.AssetID.Bytes() != assetBytes {
			return nil, fmt.Errorf("input %d asset differs", i)
		}
		inputTotal, err = inputTotal.Add(request.Inputs[i].Note.Amount)
		if err != nil {
			return nil, fmt.Errorf("input amount overflow: %w", err)
		}
	}
	for i := range request.Outputs {
		outputTotal, err = outputTotal.Add(request.Outputs[i].Amount)
		if err != nil {
			return nil, fmt.Errorf("output amount overflow: %w", err)
		}
	}
	if inputTotal != outputTotal {
		return nil, fmt.Errorf("batch output total differs from input total")
	}
	result := &SecretBatchTransferOutputs{Notes: make([]privacytypes.SecretNoteV1, len(request.Outputs)), Commitments: make([]privacycrypto.FieldValue, len(request.Outputs))}
	for i, output := range request.Outputs {
		note, err := privacytypes.NewRandomSecretNoteV1(reader, output.SpendX, output.SpendY, output.ViewX, output.ViewY, output.Amount, asset, output.Kind)
		if err != nil {
			return nil, fmt.Errorf("output %d: %w", i, err)
		}
		commitment, err := privacytypes.SecretNoteCommitmentV1(*note)
		if err != nil {
			return nil, err
		}
		result.Notes[i], result.Commitments[i] = *note, commitment
	}
	return result, nil
}
