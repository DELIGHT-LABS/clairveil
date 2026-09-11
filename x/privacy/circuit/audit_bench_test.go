package circuit

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

// A separate process per kind permits external peak-RSS measurement. All setup
// keys are development-only and written only to the explicitly supplied folder.
func TestP2AuditProofMetrics(t *testing.T) {
	name := os.Getenv("CLAIRVEIL_P2_PROOF_KIND")
	if name == "" {
		t.Skip("set CLAIRVEIL_P2_PROOF_KIND for explicit full Groth16 measurement")
	}
	var kind auditfield.Kind
	for k := auditfield.Kind(1); k <= 4; k++ {
		if kindName(k) == name {
			kind = k
		}
	}
	require.NotZero(t, kind)
	f := newAuditFixture(t, kind, 16, 32)
	measureP2Circuit(t, name, f.assignment)
}

func measureP2Circuit(t *testing.T, name string, assignment frontend.Circuit) {
	t.Helper()
	start := time.Now()
	definition := reflect.New(reflect.TypeOf(assignment).Elem()).Interface().(frontend.Circuit)
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, definition)
	require.NoError(t, err)
	compile := time.Since(start)
	start = time.Now()
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)
	setup := time.Since(start)
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	start = time.Now()
	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(t, err)
	prove := time.Since(start)
	public, err := witness.Public()
	require.NoError(t, err)
	verify := make([]float64, 10)
	for i := range verify {
		start = time.Now()
		require.NoError(t, groth16.Verify(proof, vk, public))
		verify[i] = float64(time.Since(start).Nanoseconds()) / 1e6
	}
	metrics := map[string]interface{}{"kind": name, "go": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0), "constraints": ccs.GetNbConstraints(), "public_including_one": ccs.GetNbPublicVariables(), "compile_ms": float64(compile.Nanoseconds()) / 1e6, "development_setup_ms": float64(setup.Nanoseconds()) / 1e6, "prove_ms": float64(prove.Nanoseconds()) / 1e6, "verify_only_ms": verify, "r1cs_bytes": serializedSize(t, ccs), "pk_bytes": serializedSize(t, pk), "vk_bytes": serializedSize(t, vk), "proof_bytes": serializedSize(t, proof), "production_setup": false}
	encoded, err := json.MarshalIndent(metrics, "", "  ")
	require.NoError(t, err)
	t.Log(string(encoded))
	dir := os.Getenv("CLAIRVEIL_P2_RESULT_DIR")
	if dir != "" {
		require.NoError(t, os.MkdirAll(dir, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), append(encoded, '\n'), 0600))
	}
	// A proof must bind every public field, independently of native host checks.
	if p, ok := auditPublicOf(assignment); ok {
		fields := reflect.ValueOf(p).Elem()
		for i := 0; i < fields.NumField(); i++ {
			original := fields.Field(i).Interface()
			fields.Field(i).Set(reflect.ValueOf(frontend.Variable(new(big.Int).Add(fieldBig(original), big.NewInt(1)))))
			changed, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
			require.NoError(t, err)
			require.Error(t, groth16.Verify(proof, vk, changed), "PI %d", i)
			fields.Field(i).Set(reflect.ValueOf(original))
		}
	}
}
func auditPublicOf(c frontend.Circuit) (*AuditPublic, bool) {
	switch v := c.(type) {
	case *DepositAuditFieldV1:
		return &v.Public, true
	case *SpendAuditFieldV1:
		return &v.Public, true
	case *JoinSplitAuditFieldV1:
		return &v.Public, true
	case *BatchJoinSplitAuditFieldV1:
		return &v.Public, true
	}
	return nil, false
}
