package reservation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func NullifierLookupKey(indexKey []byte, nullifier []byte) (string, error) {
	if len(indexKey) == 0 {
		return "", fmt.Errorf("index key is required")
	}
	if len(nullifier) == 0 {
		return "", fmt.Errorf("nullifier is required")
	}

	mac := hmac.New(sha256.New, indexKey)
	if _, err := mac.Write(nullifier); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func NullifierLookupKeyFromHex(indexKey []byte, nullifierHex string) (string, error) {
	nullifier, err := hex.DecodeString(nullifierHex)
	if err != nil {
		return "", fmt.Errorf("nullifier must be hex: %w", err)
	}
	return NullifierLookupKey(indexKey, nullifier)
}
