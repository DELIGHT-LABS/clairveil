package provertransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
)

const (
	TransferProofRequestVersion       = "v2"
	TransferProofResponseVersion      = "v2"
	WithdrawProofRequestVersion       = "v2"
	WithdrawProofResponseVersion      = "v2"
	BatchTransferProofRequestVersion  = "v1"
	BatchTransferProofResponseVersion = "v1"
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

type BatchTransferProofRequest struct {
	Version string                                            `json:"version"`
	Payload privacybatchtransfer.PreparedBatchTransferPayload `json:"payload"`
}

type BatchTransferProofResponse struct {
	Version string                                          `json:"version"`
	Proof   privacybatchtransfer.PreparedBatchTransferProof `json:"proof"`
}

func NewBatchTransferProofRequest(payload privacybatchtransfer.PreparedBatchTransferPayload) (*BatchTransferProofRequest, error) {
	return NewBatchTransferProofRequestAt(payload, time.Now())
}

func NewBatchTransferProofRequestAt(payload privacybatchtransfer.PreparedBatchTransferPayload, now time.Time) (*BatchTransferProofRequest, error) {
	request := BatchTransferProofRequest{Version: BatchTransferProofRequestVersion, Payload: payload}
	if err := ValidateBatchTransferProofRequestAt(request, now); err != nil {
		return nil, err
	}
	return &request, nil
}

func ValidateBatchTransferProofRequest(request BatchTransferProofRequest) error {
	return ValidateBatchTransferProofRequestAt(request, time.Now())
}

func ValidateBatchTransferProofRequestAt(request BatchTransferProofRequest, now time.Time) error {
	if request.Version != BatchTransferProofRequestVersion {
		return fmt.Errorf("unsupported batch transfer proof request version %q (expected %q)", request.Version, BatchTransferProofRequestVersion)
	}
	return privacybatchtransfer.ValidatePreparedBatchTransferPayloadMetadataAt(&request.Payload, now)
}

func BuildBatchTransferProofResponse(
	request BatchTransferProofRequest,
	artifacts privacybatchtransfer.BatchJoinSplitArtifactProvider,
	runner privacybatchtransfer.BatchJoinSplitProofRunner,
) (*BatchTransferProofResponse, error) {
	return BuildBatchTransferProofResponseAt(request, artifacts, runner, time.Now())
}

func BuildBatchTransferProofResponseAt(
	request BatchTransferProofRequest,
	artifacts privacybatchtransfer.BatchJoinSplitArtifactProvider,
	runner privacybatchtransfer.BatchJoinSplitProofRunner,
	now time.Time,
) (*BatchTransferProofResponse, error) {
	if err := ValidateBatchTransferProofRequestAt(request, now); err != nil {
		return nil, err
	}
	proof, err := privacybatchtransfer.BuildPreparedBatchTransferProofAt(&request.Payload, artifacts, runner, now)
	if err != nil {
		return nil, err
	}
	return &BatchTransferProofResponse{Version: BatchTransferProofResponseVersion, Proof: *proof}, nil
}

func ValidateBatchTransferProofResponse(request BatchTransferProofRequest, response BatchTransferProofResponse) error {
	return ValidateBatchTransferProofResponseAt(request, response, time.Now())
}

func ValidateBatchTransferProofResponseAt(request BatchTransferProofRequest, response BatchTransferProofResponse, now time.Time) error {
	if response.Version != BatchTransferProofResponseVersion {
		return fmt.Errorf("unsupported batch transfer proof response version %q (expected %q)", response.Version, BatchTransferProofResponseVersion)
	}
	if response.Proof.Version != privacybatchtransfer.PreparedBatchTransferProofVersion {
		return fmt.Errorf("unsupported prepared batch transfer proof version %q", response.Proof.Version)
	}
	if response.Proof.RequestPayloadHash != request.Payload.PayloadHash {
		return fmt.Errorf("batch transfer proof response payload hash mismatch")
	}
	if err := ValidateBatchTransferProofRequestAt(request, now); err != nil {
		return err
	}
	return privacybatchtransfer.ValidatePreparedBatchTransferProofAt(&request.Payload, &response.Proof, now)
}

func DecodeBatchTransferProofRequestJSON(payloadBytes []byte) (*BatchTransferProofRequest, error) {
	var request BatchTransferProofRequest
	if err := decodeStrictJSON(payloadBytes, &request); err != nil {
		return nil, fmt.Errorf("invalid batch transfer proof request JSON: %w", err)
	}
	return &request, nil
}

func DecodeBatchTransferProofResponseJSON(payloadBytes []byte) (*BatchTransferProofResponse, error) {
	var response BatchTransferProofResponse
	if err := decodeStrictJSON(payloadBytes, &response); err != nil {
		return nil, fmt.Errorf("invalid batch transfer proof response JSON: %w", err)
	}
	return &response, nil
}

func ReadBatchTransferProofRequestFile(path string) (*BatchTransferProofRequest, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBatchTransferProofRequestJSON(payloadBytes)
}

func ReadBatchTransferProofResponseFile(path string) (*BatchTransferProofResponse, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBatchTransferProofResponseJSON(payloadBytes)
}

func decodeStrictJSON(payloadBytes []byte, target any) error {
	if err := rejectDuplicateJSONKeys(payloadBytes); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(payloadBytes []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	var walkValue func() error
	walkValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key must be a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				if err := walkValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil {
				return err
			}
			if closing != json.Delim('}') {
				return fmt.Errorf("invalid JSON object framing")
			}
		case '[':
			for decoder.More() {
				if err := walkValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil {
				return err
			}
			if closing != json.Delim(']') {
				return fmt.Errorf("invalid JSON array framing")
			}
		default:
			return fmt.Errorf("invalid JSON delimiter")
		}
		return nil
	}
	if err := walkValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func (r BatchTransferProofRequest) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r BatchTransferProofRequest) WriteJSONFile(path string) error {
	payloadBytes, err := r.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return writePrivateFile(path, payloadBytes)
}

func (r BatchTransferProofResponse) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r BatchTransferProofResponse) WriteJSONFile(path string) error {
	payloadBytes, err := r.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return writePrivateFile(path, payloadBytes)
}

func writePrivateFile(path string, payloadBytes []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(payloadBytes); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func NewTransferProofRequest(payload privacytransfer.PreparedTransferPayload) (*TransferProofRequest, error) {
	return NewTransferProofRequestAt(payload, time.Now())
}

func NewTransferProofRequestAt(payload privacytransfer.PreparedTransferPayload, now time.Time) (*TransferProofRequest, error) {
	if err := ValidateTransferProofRequestAt(TransferProofRequest{
		Version: TransferProofRequestVersion,
		Payload: payload,
	}, now); err != nil {
		return nil, err
	}
	return &TransferProofRequest{
		Version: TransferProofRequestVersion,
		Payload: payload,
	}, nil
}

func ValidateTransferProofRequest(request TransferProofRequest) error {
	return ValidateTransferProofRequestAt(request, time.Now())
}

func ValidateTransferProofRequestAt(request TransferProofRequest, now time.Time) error {
	if request.Version != TransferProofRequestVersion {
		return fmt.Errorf("unsupported transfer proof request version %q (expected %q)", request.Version, TransferProofRequestVersion)
	}
	if err := privacytransfer.ValidatePreparedTransferPayloadMetadataAt(request.Payload, now); err != nil {
		return err
	}
	return nil
}

func BuildTransferProofResponse(
	request TransferProofRequest,
	artifacts privacytransfer.JoinSplitArtifactProvider,
	runner privacytransfer.JoinSplitProofRunner,
) (*TransferProofResponse, error) {
	return BuildTransferProofResponseAt(request, artifacts, runner, time.Now())
}

func BuildTransferProofResponseAt(
	request TransferProofRequest,
	artifacts privacytransfer.JoinSplitArtifactProvider,
	runner privacytransfer.JoinSplitProofRunner,
	now time.Time,
) (*TransferProofResponse, error) {
	if err := ValidateTransferProofRequestAt(request, now); err != nil {
		return nil, err
	}
	proof, err := privacytransfer.BuildPreparedTransferProofAt(request.Payload, artifacts, runner, now)
	if err != nil {
		return nil, err
	}
	return &TransferProofResponse{
		Version: TransferProofResponseVersion,
		Proof:   *proof,
	}, nil
}

func ValidateTransferProofResponse(request TransferProofRequest, response TransferProofResponse) error {
	return ValidateTransferProofResponseAt(request, response, time.Now())
}

func ValidateTransferProofResponseAt(request TransferProofRequest, response TransferProofResponse, now time.Time) error {
	if response.Version != TransferProofResponseVersion {
		return fmt.Errorf("unsupported transfer proof response version %q (expected %q)", response.Version, TransferProofResponseVersion)
	}
	if err := ValidateTransferProofRequestAt(request, now); err != nil {
		return err
	}
	return privacytransfer.ValidatePreparedTransferProofAt(request.Payload, response.Proof, now)
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
