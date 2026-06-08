package types

import (
	"fmt"
	"math/big"
)

const ShieldedAmountBitLength = 64

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
