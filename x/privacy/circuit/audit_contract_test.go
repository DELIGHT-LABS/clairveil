package circuit

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"
)

// This test-only primitive harness consumes fixed KAT plaintext; production
// AuditField circuits never expose that independent witness.
type auditKATCircuit struct {
	Public AuditPublic `gnark:",public"`
	Random AuditRandomness
	Plain  []frontend.Variable
	Kind   auditfield.Kind `gnark:"-"`
}

func (c *auditKATCircuit) Define(api frontend.API) error {
	return auditRelation(api, &c.Public, c.Random, c.Kind, c.Plain, nil, nil)
}
func TestP2FixedNativeKAT(t *testing.T) {
	for kind := auditfield.Kind(1); kind <= 4; kind++ {
		t.Run(kindName(kind), func(t *testing.T) {
			raw, err := os.ReadFile("../crypto/auditfield/testdata/final_" + kindName(kind) + ".json")
			require.NoError(t, err)
			var v struct{ T, Plain, Root []json.Number }
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			require.NoError(t, d.Decode(&v))
			number := func(n json.Number) *big.Int {
				x, ok := new(big.Int).SetString(n.String(), 10)
				require.True(t, ok)
				return x
			}
			a := &auditKATCircuit{Kind: kind, Random: AuditRandomness{R: 101, Nonce: number(v.T[24])}, Plain: make([]frontend.Variable, len(v.Plain))}
			p := reflect.ValueOf(&a.Public).Elem()
			for i := 0; i < 21; i++ {
				p.Field(i).Set(reflect.ValueOf(frontend.Variable(number(v.T[i+3]))))
			}
			a.Public.CipherRoot0 = number(v.Root[0])
			a.Public.CipherRoot1 = number(v.Root[1])
			for i := range a.Plain {
				a.Plain[i] = number(v.Plain[i])
			}
			require.NoError(t, test.IsSolved(&auditKATCircuit{Kind: kind, Plain: make([]frontend.Variable, len(v.Plain))}, a, ecc.BN254.ScalarField()))
		})
	}
}
func TestP2PI23SchemaAndOrder(t *testing.T) {
	schema := auditfield.PublicInputSchema()
	typ := reflect.TypeOf(AuditPublic{})
	require.Equal(t, len(schema), typ.NumField())
	for i, field := range schema {
		require.Equal(t, field.Name, typ.Field(i).Name)
	}
	for kind := auditfield.Kind(1); kind <= 4; kind++ {
		f := newAuditFixture(t, kind, 1, 1)
		w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
		require.NoError(t, err)
		vector := w.Vector().(fr.Vector)
		require.Len(t, vector, 23)
		for i, v := range f.public.fields() {
			require.Equal(t, fieldBig(v).String(), vector[i].String(), "kind %d PI %d", kind, i)
		}
	}
}
