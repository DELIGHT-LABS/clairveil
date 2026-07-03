package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

func HashRecipient(recipient string) string {
	sum := sha256.Sum256([]byte(recipient))
	return hex.EncodeToString(sum[:])
}

func HashAmount(denom string, amount *big.Int) string {
	normalized := "0"
	if amount != nil {
		normalized = amount.String()
	}
	sum := sha256.Sum256([]byte(denom + ":" + normalized))
	return hex.EncodeToString(sum[:])
}
