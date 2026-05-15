package provertransport

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
)

const (
	TransferProofRequestVersion  = "v1"
	TransferProofResponseVersion = "v1"
	WithdrawProofRequestVersion  = "v1"
	WithdrawProofResponseVersion = "v1"
)

type TransferProofRequest struct {
	Version string                                  `json:"version"`
	Payload privacytransfer.PreparedTransferPayload `json:"payload"`
}

type TransferProofResponse struct {
	Version string                                `json:"version"`
	Proof   privacytransfer.PreparedTransferProof `json:"proof"`
}

type WithdrawProofRequest struct {
	Version string                                        `json:"version"`
	Payload privacywithdraw.PreparedWithdrawProverPayload `json:"payload"`
}

type WithdrawProofResponse struct {
	Version string                                `json:"version"`
	Proof   privacywithdraw.PreparedWithdrawProof `json:"proof"`
}

func NewTransferProofRequest(payload privacytransfer.PreparedTransferPayload) (*TransferProofRequest, error) {
	if err := ValidateTransferProofRequest(TransferProofRequest{
		Version: TransferProofRequestVersion,
		Payload: payload,
	}); err != nil {
		return nil, err
	}
	return &TransferProofRequest{
		Version: TransferProofRequestVersion,
		Payload: payload,
	}, nil
}

func ValidateTransferProofRequest(request TransferProofRequest) error {
	if request.Version != TransferProofRequestVersion {
		return fmt.Errorf("unsupported transfer proof request version %q (expected %q)", request.Version, TransferProofRequestVersion)
	}
	return privacytransfer.ValidatePreparedTransferPayloadMetadata(request.Payload)
}

func BuildTransferProofResponse(
	request TransferProofRequest,
	artifacts privacytransfer.JoinSplitArtifactProvider,
	runner privacytransfer.JoinSplitProofRunner,
) (*TransferProofResponse, error) {
	if err := ValidateTransferProofRequest(request); err != nil {
		return nil, err
	}
	proof, err := privacytransfer.BuildPreparedTransferProof(request.Payload, artifacts, runner)
	if err != nil {
		return nil, err
	}
	return &TransferProofResponse{
		Version: TransferProofResponseVersion,
		Proof:   *proof,
	}, nil
}

func ValidateTransferProofResponse(request TransferProofRequest, response TransferProofResponse) error {
	if response.Version != TransferProofResponseVersion {
		return fmt.Errorf("unsupported transfer proof response version %q (expected %q)", response.Version, TransferProofResponseVersion)
	}
	if err := ValidateTransferProofRequest(request); err != nil {
		return err
	}
	return privacytransfer.ValidatePreparedTransferProof(request.Payload, response.Proof)
}

func DecodeTransferProofRequestJSON(payloadBytes []byte) (*TransferProofRequest, error) {
	var request TransferProofRequest
	if err := json.Unmarshal(payloadBytes, &request); err != nil {
		return nil, fmt.Errorf("invalid transfer proof request JSON: %w", err)
	}
	return &request, nil
}

func ReadTransferProofRequestFile(path string) (*TransferProofRequest, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeTransferProofRequestJSON(payloadBytes)
}

func (r TransferProofRequest) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r TransferProofRequest) WriteJSONFile(path string) error {
	payloadBytes, err := r.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0o600)
}

func DecodeTransferProofResponseJSON(payloadBytes []byte) (*TransferProofResponse, error) {
	var response TransferProofResponse
	if err := json.Unmarshal(payloadBytes, &response); err != nil {
		return nil, fmt.Errorf("invalid transfer proof response JSON: %w", err)
	}
	return &response, nil
}

func ReadTransferProofResponseFile(path string) (*TransferProofResponse, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeTransferProofResponseJSON(payloadBytes)
}

func (r TransferProofResponse) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r TransferProofResponse) WriteJSONFile(path string) error {
	payloadBytes, err := r.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0o600)
}

func NewWithdrawProofRequest(payload privacywithdraw.PreparedWithdrawProverPayload, now time.Time) (*WithdrawProofRequest, error) {
	if err := ValidateWithdrawProofRequest(WithdrawProofRequest{
		Version: WithdrawProofRequestVersion,
		Payload: payload,
	}, now); err != nil {
		return nil, err
	}
	return &WithdrawProofRequest{
		Version: WithdrawProofRequestVersion,
		Payload: payload,
	}, nil
}

func ValidateWithdrawProofRequest(request WithdrawProofRequest, now time.Time) error {
	if request.Version != WithdrawProofRequestVersion {
		return fmt.Errorf("unsupported withdraw proof request version %q (expected %q)", request.Version, WithdrawProofRequestVersion)
	}
	return privacywithdraw.ValidatePreparedWithdrawProverPayloadMetadata(request.Payload, now)
}

func BuildWithdrawProofResponse(
	request WithdrawProofRequest,
	artifacts privacywithdraw.SpendArtifactProvider,
	runner privacywithdraw.SpendProofRunner,
	now time.Time,
) (*WithdrawProofResponse, error) {
	if err := ValidateWithdrawProofRequest(request, now); err != nil {
		return nil, err
	}
	proof, err := privacywithdraw.BuildPreparedWithdrawProof(request.Payload, artifacts, runner)
	if err != nil {
		return nil, err
	}
	return &WithdrawProofResponse{
		Version: WithdrawProofResponseVersion,
		Proof:   *proof,
	}, nil
}

func ValidateWithdrawProofResponse(request WithdrawProofRequest, response WithdrawProofResponse, now time.Time) error {
	if response.Version != WithdrawProofResponseVersion {
		return fmt.Errorf("unsupported withdraw proof response version %q (expected %q)", response.Version, WithdrawProofResponseVersion)
	}
	if err := ValidateWithdrawProofRequest(request, now); err != nil {
		return err
	}
	return privacywithdraw.ValidatePreparedWithdrawProof(request.Payload, response.Proof, now)
}

func DecodeWithdrawProofRequestJSON(payloadBytes []byte) (*WithdrawProofRequest, error) {
	var request WithdrawProofRequest
	if err := json.Unmarshal(payloadBytes, &request); err != nil {
		return nil, fmt.Errorf("invalid withdraw proof request JSON: %w", err)
	}
	return &request, nil
}

func ReadWithdrawProofRequestFile(path string) (*WithdrawProofRequest, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeWithdrawProofRequestJSON(payloadBytes)
}

func (r WithdrawProofRequest) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r WithdrawProofRequest) WriteJSONFile(path string) error {
	payloadBytes, err := r.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0o600)
}

func DecodeWithdrawProofResponseJSON(payloadBytes []byte) (*WithdrawProofResponse, error) {
	var response WithdrawProofResponse
	if err := json.Unmarshal(payloadBytes, &response); err != nil {
		return nil, fmt.Errorf("invalid withdraw proof response JSON: %w", err)
	}
	return &response, nil
}

func ReadWithdrawProofResponseFile(path string) (*WithdrawProofResponse, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeWithdrawProofResponseJSON(payloadBytes)
}

func (r WithdrawProofResponse) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r WithdrawProofResponse) WriteJSONFile(path string) error {
	payloadBytes, err := r.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0o600)
}
