package cli

import (
	"encoding/hex"
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
)

func decodeMerkleProof(path []string, pathHelper []uint32) ([circuit.MerkleDepth]*big.Int, [circuit.MerkleDepth]int) {
	var nodes [circuit.MerkleDepth]*big.Int
	var helpers [circuit.MerkleDepth]int

	for i := 0; i < circuit.MerkleDepth; i++ {
		if i < len(path) {
			pBytes, _ := hex.DecodeString(path[i])
			nodes[i] = new(big.Int).SetBytes(pBytes)
		} else {
			nodes[i] = big.NewInt(0)
		}

		if i < len(pathHelper) {
			helpers[i] = int(pathHelper[i])
		}
	}

	return nodes, helpers
}

func summarizeSpendableNotesByDenom(notes []FoundNote, denom string) ([]FoundNote, *big.Int) {
	return privacytransfer.SummarizeSpendableNotesByDenom(notes, denom)
}

func plannerStateFingerprint(notes []FoundNote, denom string, targetAmount *big.Int) string {
	return privacytransfer.PlannerStateFingerprint(notes, denom, targetAmount)
}
