package circuit

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"
)

// Test-only adversarial public rehashing uses the KAT-checked circuit H2.
// It grants the attacker the ability to recompute any altered envelope root.
type auditRootProbe struct {
	Values []frontend.Variable
	Index  int          `gnark:"-"`
	Result *[2]*big.Int `gnark:"-"`
}

func (c *auditRootProbe) Define(api frontend.API) error {
	root := auditH2(api, c.Index, c.Values)
	c.Result[0], _ = api.Compiler().ConstantValue(root[0])
	c.Result[1], _ = api.Compiler().ConstantValue(root[1])
	return nil
}
func TestP2CipherRehashResign(t *testing.T) {
	for _, k := range []int{1, 2, 3, 4} {
		t.Run(kindName(auditfield.Kind(k)), func(t *testing.T) {
			f := newAuditFixture(t, auditfield.Kind(k), 1, 1)
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, reflect.New(reflect.TypeOf(f.assignment).Elem()).Interface().(frontend.Circuit))
			require.NoError(t, err)
			for _, name := range []string{"version", "suite", "nonce", "ephemeral", "cipher", "tag", "cipher_order"} {
				t.Run(name, func(t *testing.T) {
					f := newAuditFixture(t, auditfield.Kind(k), 1, 1)
					transcript, err := f.context(t).T(f.envelope.Nonce())
					require.NoError(t, err)
					values := make([]frontend.Variable, 0, 205)
					for _, v := range transcript {
						values = append(values, new(big.Int).SetBytes(v[:]))
					}
					x, y, err := f.envelope.EphemeralFrame().Coordinates()
					require.NoError(t, err)
					values = append(values, new(big.Int).SetBytes(x[:]), new(big.Int).SetBytes(y[:]))
					for _, v := range f.envelope.Ciphertext() {
						values = append(values, new(big.Int).SetBytes(v[:]))
					}
					tag := f.envelope.Tag()
					values = append(values, new(big.Int).SetBytes(tag[:]))
					switch name {
					case "version":
						values[0] = 3
					case "suite":
						values[1] = 3
					case "nonce":
						values[24] = 0
					case "ephemeral":
						values[25], values[26] = pointBigInts(scalarMulBase(big.NewInt(19)))
					case "cipher":
						values[27] = new(big.Int).Add(fieldBig(values[27]), big.NewInt(1))
					case "tag":
						values[len(values)-1] = 0
					case "cipher_order":
						values[27], values[28] = values[28], values[27]
					}
					var result [2]*big.Int
					probe := &auditRootProbe{Values: values, Index: 4 + k, Result: &result}
					require.NoError(t, test.IsSolved(probe, probe, ecc.BN254.ScalarField(), test.SetAllVariablesAsConstants()))
					f.public.CipherRoot0 = result[0]
					f.public.CipherRoot1 = result[1]
					f.resign(t)
					w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
					require.NoError(t, err)
					require.Error(t, ccs.IsSolved(w))
				})
			}
		})
	}
}
