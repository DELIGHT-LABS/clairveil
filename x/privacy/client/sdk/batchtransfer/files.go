package batchtransfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// DecodePreparedBatchTransferPayloadJSON decodes the versioned private payload
// without silently accepting unknown fields, duplicate keys, or trailing JSON.
// Callers must still validate the payload at the time it is proved or built.
func DecodePreparedBatchTransferPayloadJSON(payloadBytes []byte) (*PreparedBatchTransferPayload, error) {
	var payload PreparedBatchTransferPayload
	if err := decodeStrictBatchJSON(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("invalid prepared batch transfer payload JSON: %w", err)
	}
	return &payload, nil
}

func ReadPreparedBatchTransferPayload(path string) (*PreparedBatchTransferPayload, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedBatchTransferPayloadJSON(payloadBytes)
}

// DecodePreparedBatchTransferProofJSON applies the same strict framing rules as
// the prepared payload decoder. Payload-hash and expiry validation requires the
// matching payload and is performed by ValidatePreparedBatchTransferProofAt.
func DecodePreparedBatchTransferProofJSON(proofBytes []byte) (*PreparedBatchTransferProof, error) {
	var proof PreparedBatchTransferProof
	if err := decodeStrictBatchJSON(proofBytes, &proof); err != nil {
		return nil, fmt.Errorf("invalid prepared batch transfer proof JSON: %w", err)
	}
	return &proof, nil
}

func ReadPreparedBatchTransferProof(path string) (*PreparedBatchTransferProof, error) {
	proofBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedBatchTransferProofJSON(proofBytes)
}

func decodeStrictBatchJSON(payloadBytes []byte, target any) error {
	if err := rejectDuplicateBatchJSONKeys(payloadBytes); err != nil {
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

func rejectDuplicateBatchJSONKeys(payloadBytes []byte) error {
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
