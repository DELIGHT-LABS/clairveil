package zk

import (
	"strings"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestT13AuditFieldSchemaGolden(t *testing.T) {
	hashes := []string{
		"80b74ba0aa18f13e9379bf18df7a7828f1d0ae997da8339dc47dc88a5306ca9c",
		"6e7576dea2a86f54d8606f0c88d64b5b814b5f4c36c0889dcc54af4a63546ce7",
		"1a2108c9101745f83aca90d0aca2f90cca143243c58364732c879329883518b7",
		"e405f7e1c0bbed8168a93d9853ec6da3cb7acd5c35bb593e597a2839cebfd724",
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
