package types

import (
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	"math/big"
)

const ShieldedAmountBitLength = amount.BitLength

var maxShieldedAmount = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), ShieldedAmountBitLength), big.NewInt(1))

func MaxShieldedAmount() *big.Int {
	return new(big.Int).Set(maxShieldedAmount)
}

func ValidateShieldedAmount(name string, amount *big.Int) error {
	if amount == nil {
		return fmt.Errorf("%s is required", name)
	}
	if amount.Sign() < 0 {
		return fmt.Errorf("%s must be non-negative", name)
	}
	if amount.Cmp(maxShieldedAmount) > 0 {
		return fmt.Errorf("%s exceeds %d-bit shielded amount limit", name, ShieldedAmountBitLength)
	}
	return nil
}

func ParseCanonicalShieldedAmount(name string, value string) (*big.Int, error) {
	if !isCanonicalNonNegativeDecimal(value) {
		return nil, fmt.Errorf("%s must be a canonical non-negative decimal string", name)
	}

	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, fmt.Errorf("%s must be a canonical non-negative decimal string", name)
	}
	if err := ValidateShieldedAmount(name, parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func isCanonicalNonNegativeDecimal(value string) bool {
	if value == "" {
		return false
	}
	if value == "0" {
		return true
	}
	if value[0] < '1' || value[0] > '9' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// Amount128FromBigInt validates the public integer boundary before conversion.
func Amount128FromBigInt(value *big.Int) (amount.Amount128, error) {
	if err := ValidateShieldedAmount("amount", value); err != nil {
		return amount.Amount128{}, err
	}
	var raw [16]byte
	value.FillBytes(raw[:])
	return amount.FromBytes16(raw), nil
}

func Amount128BigInt(value amount.Amount128) *big.Int {
	raw := value.Bytes16()
	return new(big.Int).SetBytes(raw[:])
}
