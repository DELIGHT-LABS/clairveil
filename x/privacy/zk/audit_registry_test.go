package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

type auditLoaderCircuit struct {
	Public []frontend.Variable `gnark:",public"`
	Secret frontend.Variable
}

func (c *auditLoaderCircuit) Define(api frontend.API) error {
	for _, p := range c.Public {
		api.AssertIsEqual(api.Mul(c.Secret, c.Secret), p)
	}
	return nil
}
func auditLoaderFixture(t *testing.T, n int) (map[string][]byte, RuntimeArtifactManifest) {
	t.Helper()
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &auditLoaderCircuit{Public: make([]frontend.Variable, n)})
	require.NoError(t, err)
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)
	data := map[string][]byte{}
	sums := map[string]string{}
	for _, d := range AuditFieldArtifactDescriptors() {
		var obj io.WriterTo
		switch d.ArtifactType {
		case "r1cs":
			obj = ccs
		case "proving_key":
			obj = pk
		case "verifying_key":
			obj = vk
		}
		var b bytes.Buffer
		_, err := obj.WriteTo(&b)
		require.NoError(t, err)
		data[d.Filename] = b.Bytes()
		s := sha256.Sum256(b.Bytes())
		sums[d.ChecksumEnv] = hex.EncodeToString(s[:])
	}
	m, err := AuditFieldManifestFromChecksums("development-test", "", sums)
	require.NoError(t, err)
	return data, m
}
func auditTestRegistry(t *testing.T, data map[string][]byte, m RuntimeArtifactManifest) (*ArtifactRegistry, map[string]int) {
	t.Helper()
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return auditTestRegistryRaw(t, data, raw)
}
func auditTestRegistryRaw(t *testing.T, data map[string][]byte, raw []byte) (*ArtifactRegistry, map[string]int) {
	t.Helper()
	reads := map[string]int{}
	r, err := NewArtifactRegistry(ArtifactRegistryConfig{CircuitSetID: AuditFieldCircuitSetID, RuntimeEnvironment: ZKRuntimeEnvironmentDevelopment, LookupEnv: func(string) (string, bool) { return "", false }, ReadFile: func(path string) ([]byte, error) {
		name := filepath.Base(path)
		reads[name]++
		if name == ArtifactManifestFile {
			return raw, nil
		}
		b, ok := data[name]
		if !ok {
			return nil, os.ErrNotExist
		}
		return b, nil
	}})
	require.NoError(t, err)
	return r, reads
}
func TestAuditRegistrySelectionAndManifest(t *testing.T) {
	for _, c := range []ArtifactRegistryConfig{{CircuitSetID: AuditFieldCircuitSetID}, {CircuitSetID: "unknown"}, {CircuitSetID: AuditFieldCircuitSetID, RuntimeEnvironment: ZKRuntimeEnvironmentProduction}, {CircuitSetID: AuditFieldCircuitSetID, RuntimeEnvironment: ZKRuntimeEnvironmentDevelopment, AllowDevelopmentOverride: true}} {
		_, err := NewArtifactRegistry(c)
		require.Error(t, err)
	}
	r, err := NewArtifactRegistry(ArtifactRegistryConfig{})
	require.NoError(t, err)
	require.Equal(t, ActiveCircuitSetID, r.circuitSetID)
	data, m := auditLoaderFixture(t, 23)
	for name, mutate := range map[string]func(*RuntimeArtifactManifest){"dev marker": func(m *RuntimeArtifactManifest) { m.DevelopmentOnly = false }, "set": func(m *RuntimeArtifactManifest) { m.ActiveSetID = ActiveCircuitSetID }, "circuit": func(m *RuntimeArtifactManifest) { m.Artifacts[0].CircuitID = "initial-note-audit-field-v1" }, "missing": func(m *RuntimeArtifactManifest) { m.Artifacts = m.Artifacts[:11] }, "schema": func(m *RuntimeArtifactManifest) {
		m.CircuitSetIdentity.Circuits[0].PublicInputSchemaSha256 = strings.Repeat("00", 32)
	}, "VK identity": func(m *RuntimeArtifactManifest) {
		m.CircuitSetIdentity.Circuits[0].VerifyingKeySha256 = strings.Repeat("00", 32)
	}} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(m)
			var bad RuntimeArtifactManifest
			require.NoError(t, json.Unmarshal(raw, &bad))
			mutate(&bad)
			r, _ := auditTestRegistry(t, data, bad)
			require.Error(t, r.CheckReadiness(ArtifactRoleProver, AuditFieldCircuitIDs(), nil))
		})
	}
	raw, _ := json.Marshal(m)
	for _, bad := range [][]byte{append(append([]byte(nil), raw...), []byte(" {}")...), append([]byte(`{"unknown":true,`), raw[1:]...)} {
		r, reads := auditTestRegistryRaw(t, data, bad)
		_, err := r.LocalCircuitSetIdentity()
		require.Error(t, err)
		_, err = r.LocalCircuitSetIdentity()
		require.Error(t, err)
		require.Equal(t, 1, reads[ArtifactManifestFile])
	}
	require.Error(t, ValidateRuntimeArtifactManifest(&m))
}
func TestAuditRegistryRoleCacheAndIntegrity(t *testing.T) {
	data, m := auditLoaderFixture(t, 23)
	ids := AuditFieldCircuitIDs()
	for _, role := range []ArtifactRole{ArtifactRoleValidator, ArtifactRoleProver} {
		t.Run(string(role), func(t *testing.T) {
			r, reads := auditTestRegistry(t, data, m)
			require.NoError(t, r.CheckReadiness(role, ids, m.CircuitSetIdentity))
			require.NoError(t, r.CheckReadiness(role, ids, m.CircuitSetIdentity))
			for _, d := range m.Artifacts {
				want := 0
				if (role == ArtifactRoleValidator) == (d.ArtifactType == "verifying_key") {
					want = 1
				}
				require.Equal(t, want, reads[d.Filename], d.Filename)
			}
			require.Error(t, r.CheckReadiness(ArtifactRoleValidator, ids, nil))
			require.Error(t, r.CheckReadiness(role, []CircuitID{CircuitDeposit}, m.CircuitSetIdentity))
			require.Error(t, r.CheckReadiness(role, []CircuitID{ids[0], ids[0]}, m.CircuitSetIdentity))
			bad := privacytypes.CloneCircuitSetIdentity(m.CircuitSetIdentity)
			bad.Circuits[0].VerifyingKeySha256 = strings.Repeat("00", 32)
			require.Error(t, r.CheckReadiness(role, ids, bad))
		})
	}
	for _, d := range m.Artifacts[:3] {
		for _, mode := range []string{"missing", "corrupt", "trailing"} {
			t.Run(d.ArtifactType+"/"+mode, func(t *testing.T) {
				copied := map[string][]byte{}
				for k, v := range data {
					copied[k] = v
				}
				bad := m
				bad.Artifacts = append([]ArtifactDescriptor(nil), m.Artifacts...)
				bad.CircuitSetIdentity = privacytypes.CloneCircuitSetIdentity(m.CircuitSetIdentity)
				switch mode {
				case "missing":
					delete(copied, d.Filename)
				case "corrupt":
					copied[d.Filename] = []byte("broken")
				case "trailing":
					copied[d.Filename] = append(append([]byte(nil), copied[d.Filename]...), 0)
					sum := sha256.Sum256(copied[d.Filename])
					hash := hex.EncodeToString(sum[:])
					for i := range bad.Artifacts {
						if bad.Artifacts[i].Filename == d.Filename {
							bad.Artifacts[i].SHA256 = hash
						}
					}
					if d.ArtifactType == "verifying_key" {
						bad.CircuitSetIdentity.Circuits[0].VerifyingKeySha256 = hash
					}
				}
				r, reads := auditTestRegistry(t, copied, bad)
				role := ArtifactRoleProver
				if d.ArtifactType == "verifying_key" {
					role = ArtifactRoleValidator
				}
				require.Error(t, r.CheckReadiness(role, ids[:1], bad.CircuitSetIdentity))
				copied[d.Filename] = data[d.Filename]
				require.Error(t, r.CheckReadiness(role, ids[:1], bad.CircuitSetIdentity))
				require.Equal(t, 1, reads[d.Filename])
			})
		}
	}
	r, _ := auditTestRegistry(t, data, m)
	require.NoError(t, r.CheckReadiness(ArtifactRoleValidator, ids, m.CircuitSetIdentity))
	filename := m.Artifacts[2].Filename
	data[filename] = []byte("replacement")
	require.NoError(t, r.CheckReadiness(ArtifactRoleValidator, ids, m.CircuitSetIdentity))
	fresh, _ := auditTestRegistry(t, data, m)
	require.Error(t, fresh.CheckReadiness(ArtifactRoleValidator, ids, m.CircuitSetIdentity))
}
func TestAuditRegistryRejectsPI25(t *testing.T) {
	data, m := auditLoaderFixture(t, 25)
	for _, role := range []ArtifactRole{ArtifactRoleValidator, ArtifactRoleProver} {
		r, _ := auditTestRegistry(t, data, m)
		require.ErrorContains(t, r.CheckReadiness(role, AuditFieldCircuitIDs()[:1], m.CircuitSetIdentity), "PI23")
	}
}
func TestAuditRegistryMixedSetupProofAndWitness(t *testing.T) {
	data, m := auditLoaderFixture(t, 23)
	other, _ := auditLoaderFixture(t, 23)
	r, _ := auditTestRegistry(t, data, m)
	id := AuditFieldCircuitIDs()[0]
	ccs, err := r.R1CS(id)
	require.NoError(t, err)
	pk, err := r.ProvingKey(id)
	require.NoError(t, err)
	assignment := &auditLoaderCircuit{Public: make([]frontend.Variable, 23), Secret: 3}
	for i := range assignment.Public {
		assignment.Public[i] = 9
	}
	full, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	public, err := full.Public()
	require.NoError(t, err)
	proof, err := groth16.Prove(ccs, pk, full)
	require.NoError(t, err)
	var b bytes.Buffer
	_, err = proof.WriteTo(&b)
	require.NoError(t, err)
	require.NoError(t, r.VerifyProof(id, b.Bytes(), public, m.CircuitSetIdentity))
	for _, n := range []int{0, 22, 25} {
		a := &auditLoaderCircuit{Public: make([]frontend.Variable, n), Secret: 3}
		for i := range a.Public {
			a.Public[i] = 9
		}
		w, err := frontend.NewWitness(a, ecc.BN254.ScalarField(), frontend.PublicOnly())
		require.NoError(t, err)
		require.Error(t, r.VerifyProof(id, b.Bytes(), w, m.CircuitSetIdentity), fmt.Sprint(n))
	}
	// An independently generated PK is canonical but cannot produce a valid proof
	// for this bundle's pinned VK; no loader can infer this relation from metadata.
	data[m.Artifacts[1].Filename] = other[m.Artifacts[1].Filename]
	sum := sha256.Sum256(data[m.Artifacts[1].Filename])
	m.Artifacts[1].SHA256 = hex.EncodeToString(sum[:])
	mixedRegistry, _ := auditTestRegistry(t, data, m)
	mixed, err := mixedRegistry.ProvingKey(id)
	require.NoError(t, err)
	p, err := groth16.Prove(ccs, mixed, full)
	require.NoError(t, err)
	b.Reset()
	_, err = p.WriteTo(&b)
	require.NoError(t, err)
	require.Error(t, r.VerifyProof(id, b.Bytes(), public, m.CircuitSetIdentity))
}

func TestAuditRegistryRejectsAuthenticatedNoncanonicalKeys(t *testing.T) {
	data, m := auditLoaderFixture(t, 23)
	for _, index := range []int{1, 2} {
		t.Run(m.Artifacts[index].ArtifactType, func(t *testing.T) {
			d := m.Artifacts[index]
			var key interface {
				ReadFrom(io.Reader) (int64, error)
				WriteRawTo(io.Writer) (int64, error)
			}
			if index == 1 {
				key = groth16.NewProvingKey(ecc.BN254)
			} else {
				key = groth16.NewVerifyingKey(ecc.BN254)
			}
			_, err := key.ReadFrom(bytes.NewReader(data[d.Filename]))
			require.NoError(t, err)
			var b bytes.Buffer
			_, err = key.WriteRawTo(&b)
			require.NoError(t, err)
			copied := map[string][]byte{}
			for k, v := range data {
				copied[k] = v
			}
			copied[d.Filename] = b.Bytes()
			bad := m
			bad.Artifacts = append([]ArtifactDescriptor(nil), m.Artifacts...)
			bad.CircuitSetIdentity = privacytypes.CloneCircuitSetIdentity(m.CircuitSetIdentity)
			sum := sha256.Sum256(b.Bytes())
			bad.Artifacts[index].SHA256 = hex.EncodeToString(sum[:])
			if index == 2 {
				bad.CircuitSetIdentity.Circuits[0].VerifyingKeySha256 = bad.Artifacts[index].SHA256
			}
			r, _ := auditTestRegistry(t, copied, bad)
			_, err = r.loadArtifact(AuditFieldCircuitIDs()[0], d.ArtifactType, false)
			require.ErrorContains(t, err, "not canonically encoded")
		})
	}
}
func TestAuditRegistryRejectsChecksumEnvironmentOverride(t *testing.T) {
	data, m := auditLoaderFixture(t, 23)
	r, _ := auditTestRegistry(t, data, m)
	r.lookupEnv = func(string) (string, bool) { return strings.Repeat("00", 32), true }
	require.ErrorContains(t, r.CheckReadiness(ArtifactRoleProver, AuditFieldCircuitIDs()[:1], nil), "cannot override")
}
