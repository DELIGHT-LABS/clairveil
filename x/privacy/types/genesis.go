package types

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

const genesisFieldElementByteSize = 32

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Commitments:     [][]byte{},
		HistoricalRoots: [][]byte{},
		Nullifiers:      [][]byte{},
	}
}

// Validate performs basic genesis state validation returning an error upon any failure.
func (gs GenesisState) Validate() error {
	for i, commitment := range gs.Commitments {
		if err := validateGenesisFieldElementBytes(commitment); err != nil {
			return fmt.Errorf("commitments[%d]: %w", i, err)
		}
	}

	for i, root := range gs.HistoricalRoots {
		if err := validateGenesisFieldElementBytes(root); err != nil {
			return fmt.Errorf("historical_roots[%d]: %w", i, err)
		}
	}

	for i, nullifier := range gs.Nullifiers {
		if err := validateGenesisFieldElementBytes(nullifier); err != nil {
			return fmt.Errorf("nullifiers[%d]: %w", i, err)
		}
	}

	if len(gs.AuditMasterPubkey) != 0 {
		if _, err := decodePublicKey(gs.AuditMasterPubkey); err != nil {
			return fmt.Errorf("audit_master_pubkey: %w", err)
		}
	}

	return nil
}

func validateGenesisFieldElementBytes(bz []byte) error {
	if len(bz) != genesisFieldElementByteSize {
		return fmt.Errorf("must be %d bytes", genesisFieldElementByteSize)
	}

	var elem fr.Element
	if err := elem.SetBytesCanonical(bz); err != nil {
		return fmt.Errorf("must be canonical field bytes")
	}

	return nil
}
