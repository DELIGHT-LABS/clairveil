package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

const genesisFieldElementByteSize = 32

const (
	CircuitSetIdentitySchemaVersion = "v1"
	CircuitCurveBN254               = "BN254"
)

var RequiredCircuitIdentityOrder = []string{"deposit", "spend", "joinsplit"}

// DefaultGenesis returns the default genesis state for a specific circuit set.
func DefaultGenesis(identity *CircuitSetIdentity) *GenesisState {
	if identity == nil {
		panic("default privacy genesis requires circuit set identity")
	}
	return &GenesisState{
		Commitments:        [][]byte{},
		HistoricalRoots:    [][]byte{},
		Nullifiers:         [][]byte{},
		CircuitSetIdentity: CloneCircuitSetIdentity(identity),
	}
}

// Validate performs basic genesis state validation returning an error upon any failure.
func (gs GenesisState) Validate() error {
	for i, commitment := range gs.Commitments {
		if err := validateGenesisFieldElementBytes(commitment); err != nil {
			return fmt.Errorf("commitments[%d]: %w", i, err)
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("commitment", gs.Commitments); err != nil {
		return fmt.Errorf("commitments: %w", err)
	}

	for i, root := range gs.HistoricalRoots {
		if err := validateGenesisFieldElementBytes(root); err != nil {
			return fmt.Errorf("historical_roots[%d]: %w", i, err)
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("historical root", gs.HistoricalRoots); err != nil {
		return fmt.Errorf("historical_roots: %w", err)
	}

	for i, nullifier := range gs.Nullifiers {
		if err := validateGenesisFieldElementBytes(nullifier); err != nil {
			return fmt.Errorf("nullifiers[%d]: %w", i, err)
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("nullifier", gs.Nullifiers); err != nil {
		return fmt.Errorf("nullifiers: %w", err)
	}

	if len(gs.AuditMasterPubkey) != 0 {
		if _, err := decodePublicKey(gs.AuditMasterPubkey); err != nil {
			return fmt.Errorf("audit_master_pubkey: %w", err)
		}
	}
	if gs.CircuitSetIdentity == nil {
		return fmt.Errorf("circuit_set_identity: is required")
	}
	if err := ValidateCircuitSetIdentity(gs.CircuitSetIdentity); err != nil {
		return fmt.Errorf("circuit_set_identity: %w", err)
	}

	return nil
}

func ValidateCircuitSetIdentity(identity *CircuitSetIdentity) error {
	if identity == nil {
		return fmt.Errorf("is required")
	}
	if identity.SchemaVersion != CircuitSetIdentitySchemaVersion {
		return fmt.Errorf("schema_version must be %q", CircuitSetIdentitySchemaVersion)
	}
	if identity.CircuitSetId != ActiveCircuitSetID {
		return fmt.Errorf("circuit_set_id must be %q", ActiveCircuitSetID)
	}
	if identity.Curve != CircuitCurveBN254 {
		return fmt.Errorf("curve must be %q", CircuitCurveBN254)
	}
	if len(identity.Circuits) != len(RequiredCircuitIdentityOrder) {
		return fmt.Errorf("must contain exactly %d circuits", len(RequiredCircuitIdentityOrder))
	}
	for i, expectedID := range RequiredCircuitIdentityOrder {
		circuit := identity.Circuits[i]
		if circuit == nil {
			return fmt.Errorf("circuits[%d] is required", i)
		}
		if circuit.CircuitId != expectedID {
			return fmt.Errorf("circuits[%d].circuit_id must be %q", i, expectedID)
		}
		if err := validateLowercaseSHA256(circuit.VerifyingKeySha256); err != nil {
			return fmt.Errorf("circuits[%d].verifying_key_sha256: %w", i, err)
		}
		if err := validateLowercaseSHA256(circuit.PublicInputSchemaSha256); err != nil {
			return fmt.Errorf("circuits[%d].public_input_schema_sha256: %w", i, err)
		}
	}
	return nil
}

func CloneCircuitSetIdentity(identity *CircuitSetIdentity) *CircuitSetIdentity {
	if identity == nil {
		return nil
	}
	cloned := &CircuitSetIdentity{
		SchemaVersion: identity.SchemaVersion,
		CircuitSetId:  identity.CircuitSetId,
		Curve:         identity.Curve,
		Circuits:      make([]*CircuitIdentity, len(identity.Circuits)),
	}
	for i, circuit := range identity.Circuits {
		if circuit == nil {
			continue
		}
		copyCircuit := *circuit
		cloned.Circuits[i] = &copyCircuit
	}
	return cloned
}

func validateLowercaseSHA256(value string) error {
	if len(value) != 64 || value != strings.ToLower(value) {
		return fmt.Errorf("must be a lowercase 64-character hex string")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("must be a lowercase 64-character hex string")
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
