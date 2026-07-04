package payroll

import (
	"encoding/hex"
	"fmt"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type ChunkOptions struct {
	MaxMessagesPerTx int
	ChunkIDPrefix    string
}

type MessageChunk struct {
	ChunkID string
	Results []ProofResult
}

func ChunkProofResults(results []ProofResult, options ChunkOptions) ([]MessageChunk, error) {
	maxMessages := options.MaxMessagesPerTx
	if maxMessages <= 0 {
		maxMessages = 1
	}
	prefix := options.ChunkIDPrefix
	if prefix == "" {
		prefix = "payroll-chunk"
	}

	seenNullifiers := make(map[string]string)
	chunks := make([]MessageChunk, 0, (len(results)+maxMessages-1)/maxMessages)
	for _, result := range results {
		if result.Message == nil {
			return nil, fmt.Errorf("proof result for operation %s has no message", result.Item.OperationID)
		}
		if err := checkDuplicateNullifiers(result.Message, result.Item.OperationID, seenNullifiers); err != nil {
			return nil, err
		}

		if len(chunks) == 0 || len(chunks[len(chunks)-1].Results) >= maxMessages {
			chunks = append(chunks, MessageChunk{
				ChunkID: fmt.Sprintf("%s-%06d", prefix, len(chunks)+1),
			})
		}
		current := &chunks[len(chunks)-1]
		current.Results = append(current.Results, result)
	}

	return chunks, nil
}

func checkDuplicateNullifiers(msg *privacytypes.MsgTransfer, operationID string, seen map[string]string) error {
	for _, nullifier := range msg.Nullifiers {
		key := hex.EncodeToString(nullifier)
		if previousOperationID, ok := seen[key]; ok {
			return fmt.Errorf("duplicate nullifier in payroll chunk: operation %s conflicts with %s", operationID, previousOperationID)
		}
		seen[key] = operationID
	}
	return nil
}
