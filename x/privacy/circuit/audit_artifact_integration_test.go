package circuit

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/stretchr/testify/require"
)

// Uses the product setup command's external development bundle and the native
// encrypted/signed P2 fixtures. No setup or second fixture implementation here.
func TestAuditArtifactProductIntegration(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to a development setup bundle")
	}
	require.Equal(t, 32, MerkleDepth, "gas policy uses 33 node writes per output")
	r, err := zk.NewArtifactRegistry(zk.ArtifactRegistryConfig{ArtifactDir: dir, CircuitSetID: zk.AuditFieldCircuitSetID, RuntimeEnvironment: zk.ZKRuntimeEnvironmentDevelopment})
	require.NoError(t, err)
	identity, err := r.LocalCircuitSetIdentity()
	require.NoError(t, err)
	for i, id := range zk.AuditFieldCircuitIDs() {
		t.Run(string(id), func(t *testing.T) {
			start := time.Now()
			require.NoError(t, r.CheckReadiness(zk.ArtifactRoleProver, []zk.CircuitID{id}, identity))
			require.NoError(t, r.CheckReadiness(zk.ArtifactRoleValidator, []zk.CircuitID{id}, identity))
			ccs, err := r.R1CS(id)
			require.NoError(t, err)
			pk, err := r.ProvingKey(id)
			require.NoError(t, err)
			load := time.Since(start)
			f := newAuditFixture(t, auditfield.Kind(i+1), 16, 32)
			full, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
			require.NoError(t, err)
			public, err := full.Public()
			require.NoError(t, err)
			start = time.Now()
			proof, err := groth16.Prove(ccs, pk, full)
			require.NoError(t, err)
			prove := time.Since(start)
			var encoded bytes.Buffer
			_, err = proof.WriteTo(&encoded)
			require.NoError(t, err)
			start = time.Now()
			require.NoError(t, r.VerifyProof(id, encoded.Bytes(), public, identity))
			t.Logf("constraints=%d public=%d proof_bytes=%d load_ms=%d prove_ms=%d verify_us=%d", ccs.GetNbConstraints(), ccs.GetNbPublicVariables()-1, encoded.Len(), load.Milliseconds(), prove.Milliseconds(), time.Since(start).Microseconds())
			wrong := privacytypes.CloneCircuitSetIdentity(identity)
			wrong.Circuits[i].VerifyingKeySha256 = identity.Circuits[(i+1)%4].VerifyingKeySha256
			require.Error(t, r.VerifyProof(id, encoded.Bytes(), public, wrong))
			require.Error(t, r.VerifyProof(zk.AuditFieldCircuitIDs()[(i+1)%4], encoded.Bytes(), public, identity))
			require.Error(t, r.VerifyProof(zk.CircuitDeposit, encoded.Bytes(), public, identity))
			require.Error(t, r.VerifyProof(id, encoded.Bytes(), public, nil))
			require.Error(t, r.VerifyProof(id, encoded.Bytes(), nil, identity))
			f.public.NetworkHi = new(big.Int).Add(fieldBig(f.public.NetworkHi), big.NewInt(1))
			changed, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
			require.NoError(t, err)
			require.Error(t, r.VerifyProof(id, encoded.Bytes(), changed, identity))
			for name, mutate := range map[string]func([]byte) []byte{
				"trailing":     func(b []byte) []byte { return append(b, 0) },
				"oversize":     func(b []byte) []byte { return make([]byte, 4097) },
				"commitment":   func(b []byte) []byte { binary.BigEndian.PutUint32(b[128:132], 1); return b },
				"uncompressed": func(b []byte) []byte { b[0] &= 0x3f; return b },
				"invalid point": func(b []byte) []byte {
					for j := 1; j < 32; j++ {
						b[j] = 255
					}
					return b
				},
			} {
				t.Run(name, func(t *testing.T) {
					require.Error(t, r.VerifyProof(id, mutate(append([]byte(nil), encoded.Bytes()...)), public, identity))
				})
			}
		})
	}
}
