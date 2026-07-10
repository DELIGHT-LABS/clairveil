package types

import (
	"fmt"
)

// ValidateDistinctCanonicalFieldElements validates canonical BN254 field
// encodings and rejects duplicate values without reducing attacker-controlled
// bytes modulo the field modulus.
func ValidateDistinctCanonicalFieldElements(name string, values [][]byte) error {
	seen := make(map[[expectedFieldElementBytes]byte]int, len(values))
	for i, value := range values {
		if err := validateFieldElementBytesStrict(name, value); err != nil {
			return fmt.Errorf("%s index %d: %w", name, i, err)
		}

		var key [expectedFieldElementBytes]byte
		copy(key[:], value)
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("%s index %d duplicates index %d", name, i, previous)
		}
		seen[key] = i
	}
	return nil
}
