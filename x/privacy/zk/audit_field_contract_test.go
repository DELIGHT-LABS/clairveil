package zk

import (
	"strings"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestT13AuditFieldSchemaGolden(t *testing.T) {
	hashes := []string{
		"5582ac050f0aecc4a40d02acfc948deb94932443e9857167b8a15602c5f9df7e",
		"2e9cd75633922450f0aab068b2af26b94b2b3d71cc75426303fcf88554e5b8fa",
		"79c7ff4a0ba411ccfaee0376291f9c1dfe65b4e38acf6ea1384749eefb07e333",
		"93668901c968802861b13658290c2d703f6837de8ae7af7ef0bf65c906bbfd84",
	}
	for i, id := range AuditFieldCircuitIDs() {
		fields, err := PublicInputSchema(string(id))
		if err != nil {
			t.Fatal(err)
		}
		if len(fields) != 23 {
			t.Fatal("not PI23")
		}
		got, err := PublicInputSchemaSHA256(string(id))
		if err != nil || got != hashes[i] {
			t.Fatalf("%s: %s %v", id, got, err)
		}
		fields[0].Name = "mutated"
		again, _ := PublicInputSchema(string(id))
		if again[0].Name == "mutated" {
			t.Fatal("schema aliases caller")
		}
	}
	if _, err := PublicInputSchema("initial-note-audit-field-v1"); err == nil {
		t.Fatal("unknown fifth circuit accepted")
	}
	for _, active := range RequiredCircuitIDs() {
		if isAuditFieldCircuit(string(active)) {
			t.Fatal("new circuit activated prematurely")
		}
	}
	if ActiveCircuitSetID == AuditFieldCircuitSetID {
		t.Fatal("new set activated prematurely")
	}
}

func auditManifestFixture(t *testing.T) *RuntimeArtifactManifest {
	t.Helper()
	m := &RuntimeArtifactManifest{SchemaVersion: CircuitConfigSchemaVersion, Curve: CircuitCurve, ActiveSetID: AuditFieldCircuitSetID, Artifacts: AuditFieldArtifactDescriptors(), CircuitSetIdentity: &privacytypes.CircuitSetIdentity{SchemaVersion: privacytypes.CircuitSetIdentitySchemaVersion, Curve: CircuitCurve, CircuitSetId: AuditFieldCircuitSetID}}
	for i := range m.Artifacts {
		m.Artifacts[i].SHA256 = strings.Repeat("ab", 32)
	}
	for _, id := range AuditFieldCircuitIDs() {
		hash, err := PublicInputSchemaSHA256(string(id))
		if err != nil {
			t.Fatal(err)
		}
		m.CircuitSetIdentity.Circuits = append(m.CircuitSetIdentity.Circuits, &privacytypes.CircuitIdentity{CircuitId: string(id), VerifyingKeySha256: strings.Repeat("ab", 32), PublicInputSchemaSha256: hash})
	}
	return m
}

func TestT13AuditFieldManifestContract(t *testing.T) {
	if err := ValidateAuditFieldArtifactManifest(auditManifestFixture(t)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAuditFieldArtifactManifest(nil); err == nil {
		t.Fatal("nil accepted")
	}
	cases := map[string]func(*RuntimeArtifactManifest){
		"old set":        func(m *RuntimeArtifactManifest) { m.ActiveSetID = ActiveCircuitSetID },
		"version":        func(m *RuntimeArtifactManifest) { m.SchemaVersion = "v1" },
		"curve":          func(m *RuntimeArtifactManifest) { m.Curve = "BLS12-381" },
		"missing":        func(m *RuntimeArtifactManifest) { m.Artifacts = m.Artifacts[:11] },
		"fifth":          func(m *RuntimeArtifactManifest) { m.Artifacts = append(m.Artifacts, m.Artifacts[:3]...) },
		"reordered":      func(m *RuntimeArtifactManifest) { m.Artifacts[0], m.Artifacts[3] = m.Artifacts[3], m.Artifacts[0] },
		"empty hash":     func(m *RuntimeArtifactManifest) { m.Artifacts[0].SHA256 = "" },
		"uppercase hash": func(m *RuntimeArtifactManifest) { m.Artifacts[0].SHA256 = strings.Repeat("AB", 32) },
		"nil identity":   func(m *RuntimeArtifactManifest) { m.CircuitSetIdentity = nil },
		"nil circuit":    func(m *RuntimeArtifactManifest) { m.CircuitSetIdentity.Circuits[0] = nil },
		"fifth identity": func(m *RuntimeArtifactManifest) {
			m.CircuitSetIdentity.Circuits = append(m.CircuitSetIdentity.Circuits, m.CircuitSetIdentity.Circuits[0])
		},
		"schema mismatch": func(m *RuntimeArtifactManifest) {
			m.CircuitSetIdentity.Circuits[0].PublicInputSchemaSha256 = strings.Repeat("ab", 32)
		},
		"VK mismatch": func(m *RuntimeArtifactManifest) { m.Artifacts[2].SHA256 = strings.Repeat("cd", 32) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := auditManifestFixture(t)
			mutate(m)
			if err := ValidateAuditFieldArtifactManifest(m); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	if err := ValidateRuntimeArtifactManifest(auditManifestFixture(t)); err == nil {
		t.Fatal("active registry accepts inactive set")
	}
}
