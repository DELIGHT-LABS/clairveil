package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

var maxUint64Amount = new(big.Int).SetUint64(^uint64(0))

// HashRecipient commits to a non-empty canonical recipient string.
func HashRecipient(recipient string) (string, error) {
	normalized := strings.TrimSpace(recipient)
	if normalized == "" {
		return "", fmt.Errorf("recipient is required")
	}
	bundle, err := privacytypes.DecodeShieldedAddressBundle(normalized)
	if err != nil {
		return "", fmt.Errorf("recipient must be a canonical shielded address: %w", err)
	}
	canonical, err := privacytypes.EncodeShieldedAddressWithView(bundle.SpendPubKey, bundle.ViewPubKey)
	if err != nil {
		return "", fmt.Errorf("canonicalize shielded recipient: %w", err)
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:]), nil
}

// HashAmount commits to the canonical denom:amount string for a uint64
// minimal-denom amount. Invalid inputs must not create reusable evidence.
func HashAmount(denom string, amount *big.Int) (string, error) {
	normalizedDenom := strings.TrimSpace(denom)
	if normalizedDenom != denom {
		return "", fmt.Errorf("denom must be canonical without surrounding whitespace")
	}
	if err := sdk.ValidateDenom(normalizedDenom); err != nil {
		return "", fmt.Errorf("invalid Cosmos SDK denom: %w", err)
	}
	if amount == nil {
		return "", fmt.Errorf("amount is required")
	}
	if amount.Sign() < 0 || amount.Cmp(maxUint64Amount) > 0 {
		return "", fmt.Errorf("amount must be within uint64 range")
	}
	sum := sha256.Sum256([]byte(normalizedDenom + ":" + amount.String()))
	return hex.EncodeToString(sum[:]), nil
}
