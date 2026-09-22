package privacy

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RunAuditGenesis exposes the trusted application-owned V4 genesis boundary
// without exposing the internal context marker or its representation.
func RunAuditGenesis(ctx sdk.Context, initialize func(sdk.Context) error) error {
	if initialize == nil {
		return fmt.Errorf("audit genesis initializer is required")
	}
	return auditinit.Run(ctx, initialize)
}

// ComputeAuditGenesisAnchor derives the stable first-chain provenance anchor
// used by fresh and exported V4 genesis state.
func ComputeAuditGenesisAnchor(chainID string, networkNonce []byte, initialHeight uint64) ([32]byte, error) {
	if len(networkNonce) != 32 {
		return [32]byte{}, fmt.Errorf("audit network nonce must be 32 bytes")
	}
	if initialHeight == 0 || initialHeight > 1<<63-1 {
		return [32]byte{}, fmt.Errorf("audit initial height is invalid")
	}
	var height [8]byte
	binary.BigEndian.PutUint64(height[:], initialHeight)
	prefix := []byte("clairveil/privacy/genesis-anchor/v1")
	input := make([]byte, 0, len(prefix)+len(chainID)+len(networkNonce)+len(height))
	input = append(input, prefix...)
	input = append(input, chainID...)
	input = append(input, networkNonce...)
	input = append(input, height[:]...)
	return sha256.Sum256(input), nil
}
