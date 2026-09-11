package zk

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// These descriptors define the next circuit set; they do not register circuits
// or enable proof verification. Activation requires the P2-P8 release work.
const (
	AuditFieldCircuitSetID                    = auditfield.CircuitSetID
	CircuitDepositAuditField        CircuitID = "deposit-audit-field-v1"
	CircuitSpendAuditField          CircuitID = "spend-audit-field-v1"
	CircuitJoinSplitAuditField      CircuitID = "joinsplit-2x2-audit-field-v1"
	CircuitBatchJoinSplitAuditField CircuitID = "batch-joinsplit-16x32-audit-field-v1"
)

func AuditFieldCircuitIDs() []CircuitID {
	return []CircuitID{CircuitDepositAuditField, CircuitSpendAuditField, CircuitJoinSplitAuditField, CircuitBatchJoinSplitAuditField}
}

func isAuditFieldCircuit(id string) bool {
	for _, candidate := range AuditFieldCircuitIDs() {
		if id == string(candidate) {
			return true
		}
	}
	return false
}

// AuditFieldArtifactDescriptors returns the complete, ordered 4x3 artifact
// contract. Hashes must come from canonical serialized artifacts, never from
// these metadata definitions. No development setup or hashes are supplied.
func AuditFieldArtifactDescriptors() []ArtifactDescriptor {
	out := make([]ArtifactDescriptor, 0, 12)
	for _, id := range AuditFieldCircuitIDs() {
		stem := strings.ReplaceAll(string(id), "-", "_")
		for _, kind := range []struct{ name, suffix string }{{"r1cs", "r1cs"}, {"proving_key", "pk"}, {"verifying_key", "vk"}} {
			out = append(out, ArtifactDescriptor{CircuitID: string(id), ArtifactType: kind.name,
				Filename:    "privacy_" + stem + "_" + kind.suffix + ".bin",
				ChecksumEnv: "CLAIRVEIL_" + strings.ToUpper(stem) + "_" + strings.ToUpper(kind.suffix) + "_SHA256"})
		}
	}
	return out
}

// ValidateAuditFieldArtifactManifest validates the new set's metadata contract.
// It does not read artifacts, prove setup integrity, or change the active registry.
func ValidateAuditFieldArtifactManifest(m *RuntimeArtifactManifest) error {
	if m == nil {
		return fmt.Errorf("audit field manifest is required")
	}
	if m.SchemaVersion != CircuitConfigSchemaVersion || m.ActiveSetID != AuditFieldCircuitSetID || m.Curve != CircuitCurve {
		return fmt.Errorf("audit field manifest version, set or curve mismatch")
	}
	expected := AuditFieldArtifactDescriptors()
	if len(m.Artifacts) != len(expected) {
		return fmt.Errorf("audit field manifest requires exactly 12 artifacts for four circuits")
	}
	for i, want := range expected {
		got := m.Artifacts[i]
		if err := validateExpectedSHA256(got.Filename, got.SHA256); err != nil {
			return err
		}
		if got.SHA256 != strings.ToLower(got.SHA256) {
			return fmt.Errorf("audit field artifact hash must be lowercase")
		}
		got.SHA256 = ""
		if got != want {
			return fmt.Errorf("audit field artifact descriptor %d mismatch", i)
		}
	}
	identity := m.CircuitSetIdentity
	if identity == nil || identity.SchemaVersion != privacytypes.CircuitSetIdentitySchemaVersion || identity.CircuitSetId != AuditFieldCircuitSetID || identity.Curve != CircuitCurve {
		return fmt.Errorf("audit field circuit identity mismatch")
	}
	if len(identity.Circuits) != 4 {
		return fmt.Errorf("audit field identity requires exactly four circuits")
	}
	for i, id := range AuditFieldCircuitIDs() {
		hash, err := PublicInputSchemaSHA256(string(id))
		if err != nil {
			return err
		}
		want := &privacytypes.CircuitIdentity{CircuitId: string(id), VerifyingKeySha256: m.Artifacts[3*i+2].SHA256, PublicInputSchemaSha256: hash}
		if !reflect.DeepEqual(identity.Circuits[i], want) {
			return fmt.Errorf("audit field circuit identity %d does not match schema and VK", i)
		}
	}
	return nil
}
