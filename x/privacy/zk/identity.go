package zk

import (
	"fmt"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// ValidateLocalVerifierCircuitIdentity compares the complete local manifest
// identity to consensus, then reads only the requested circuit VK.
func ValidateLocalVerifierCircuitIdentity(expected *privacytypes.CircuitSetIdentity, circuitID CircuitID) error {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return err
	}
	if err := registry.CheckReadiness(ArtifactRoleValidator, []CircuitID{circuitID}, expected); err != nil {
		return fmt.Errorf("validate local verifier circuit %s: %w", circuitID, err)
	}
	return nil
}

// ValidateLocalVerifierIdentity is the startup compatibility entrypoint. All
// active circuits are required by the validator role, while each VK remains a
// separate lazy cache entry.
func ValidateLocalVerifierIdentity(expected *privacytypes.CircuitSetIdentity) error {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return err
	}
	return registry.CheckReadiness(ArtifactRoleValidator, RequiredCircuitIDs(), expected)
}

func ValidateLocalVerifierArtifacts() error {
	registry, err := NewRuntimeArtifactRegistry()
	if err != nil {
		return err
	}
	identity, err := registry.LocalCircuitSetIdentity()
	if err != nil {
		return err
	}
	return registry.CheckReadiness(ArtifactRoleValidator, RequiredCircuitIDs(), identity)
}
