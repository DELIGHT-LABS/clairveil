package circuit

import (
	"math/big"
	"reflect"
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/signature/eddsa"
	"github.com/stretchr/testify/require"
)

type auditFixture struct {
	assignment frontend.Circuit
	public     *AuditPublic
	random     *AuditRandomness
	signature  *eddsa.Signature
	kind       auditfield.Kind
	plain      []auditfield.Field32
	envelope   auditfield.EnvelopeFrame
}

func fieldBig(v frontend.Variable) *big.Int {
	switch x := v.(type) {
	case *big.Int:
		return new(big.Int).Set(x)
	case int:
		return big.NewInt(int64(x))
	case uint64:
		return new(big.Int).SetUint64(x)
	case int64:
		return big.NewInt(x)
	default:
		panic("unexpected fixture field type")
	}
}
func auditField(t testing.TB, v frontend.Variable) auditfield.Field32 {
	t.Helper()
	raw := fieldBig(v).FillBytes(make([]byte, 32))
	f, err := auditfield.ParseField32(raw)
	require.NoError(t, err)
	return f
}
func copyNoteFixture(dst, src interface{}) {
	d, s := reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem()
	for i := 0; i < d.NumField(); i++ {
		f := s.FieldByName(d.Type().Field(i).Name)
		if f.IsValid() {
			d.Field(i).Set(f)
		}
	}
}
func nativeAuditVector(t testing.TB, kind privacytypes.BatchVectorKindV1, values ...frontend.Variable) *big.Int {
	t.Helper()
	v := make([]*big.Int, len(values))
	for i := range v {
		v[i] = fieldBig(values[i])
	}
	root, err := privacytypes.ComputeBatchVectorRootV1(kind, uint32(len(v)), v)
	require.NoError(t, err)
	return root
}
func fixtureCommitment(spend, view eddsa.PublicKey, amount, asset, rho frontend.Variable) *big.Int {
	return privacytypes.ComputeNoteCommitmentV1(fieldBig(spend.A.X), fieldBig(spend.A.Y), fieldBig(view.A.X), fieldBig(view.A.Y), fieldBig(amount), fieldBig(asset), fieldBig(rho))
}
func newAuditFixture(t testing.TB, kind auditfield.Kind, inputs, outputs int, override ...frontend.Circuit) *auditFixture {
	t.Helper()
	f := &auditFixture{kind: kind}
	var plain []frontend.Variable
	switch kind {
	case auditfield.KindDeposit:
		old := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
		if len(override) > 0 {
			old = override[0].(*DepositCircuit)
		}
		c := &DepositAuditFieldV1{}
		copyNoteFixture(c, old)
		f.assignment = c
		f.public = &c.Public
		f.random = &c.Random
		initializeAuditPublic(t, f.public)
		c.Public.PublicAmount = old.Amount
		c.Public.PublicAsset = old.AssetID
		c.Public.OutputCount = 1
		c.Public.CommitmentRoot = nativeAuditVector(t, privacytypes.BatchVectorCommitmentV1, c.Commitment)
		plain = []frontend.Variable{old.AssetID, old.Amount, c.ReceiverSpendPubKey.X, c.ReceiverSpendPubKey.Y, c.ReceiverViewPubKey.X, c.ReceiverViewPubKey.Y}
	case auditfield.KindWithdraw:
		old := buildValidSpendAssignment(t, big.NewInt(107))
		if len(override) > 0 {
			old = override[0].(*SpendCircuit)
		}
		c := &SpendAuditFieldV1{}
		copyNoteFixture(c, old)
		f.assignment = c
		f.public = &c.Public
		f.random = &c.Random
		f.signature = &c.Signature
		initializeAuditPublic(t, f.public)
		c.Public.MerkleRoot = old.MerkleRoot
		c.Public.PublicAmount = old.Amount
		c.Public.PublicAsset = old.AssetID
		c.Public.InputCount = 1
		c.Public.NullifierRoot = nativeAuditVector(t, privacytypes.BatchVectorNullifierV1, c.Nullifier)
		plain = []frontend.Variable{old.AssetID, fixtureCommitment(c.ReceiverSpendPubKey, c.ReceiverViewPubKey, old.Amount, old.AssetID, c.Randomness)}
	case auditfield.KindTransfer2x2:
		old := buildValidJoinSplitAssignment(t)
		if len(override) > 0 {
			old = override[0].(*JoinSplitCircuit)
		}
		c := &JoinSplitAuditFieldV1{}
		copyNoteFixture(c, old)
		f.assignment = c
		f.public = &c.Public
		f.random = &c.Random
		f.signature = &c.OwnerSignature
		initializeAuditPublic(t, f.public)
		c.Public.MerkleRoot = old.MerkleRoot
		c.Public.InputCount = 2
		c.Public.OutputCount = 2
		c.Public.NullifierRoot = nativeAuditVector(t, privacytypes.BatchVectorNullifierV1, c.Nullifiers[:]...)
		c.Public.CommitmentRoot = nativeAuditVector(t, privacytypes.BatchVectorCommitmentV1, c.Commitments[:]...)
		c.Public.UserDisclosureRoot = privacycrypto.MimcHash(privacytypes.DomainFieldV1("clairveil.audit.user-2x2.v1"), fieldBig(c.UserPrivacyPolicy), fieldBig(c.UserDisclosureDigest))
		c.Public.SelfViewRoot = c.FullDisclosureDigest
		plain = []frontend.Variable{c.AssetID}
		for i := 0; i < NumInputs; i++ {
			plain = append(plain, fixtureCommitment(c.InputSpendPubKeys[i], c.InputViewPubKeys[i], c.InputAmounts[i], c.AssetID, c.InputRandomness[i]))
		}
		for i := 0; i < NumOutputs; i++ {
			plain = append(plain, c.OutputAmounts[i], c.OutputSpendPubKeys[i].A.X, c.OutputSpendPubKeys[i].A.Y, c.OutputViewPubKeys[i].A.X, c.OutputViewPubKeys[i].A.Y)
		}
	case auditfield.KindBatch16x32:
		old := buildBatchFeasibilityAssignment(t, inputs, outputs)
		if len(override) > 0 {
			old = override[0].(*BatchJoinSplit16x32)
		}
		c := &BatchJoinSplitAuditFieldV1{}
		copyNoteFixture(c, old)
		f.assignment = c
		f.public = &c.Public
		f.random = &c.Random
		f.signature = &c.OwnerSignature
		initializeAuditPublic(t, f.public)
		c.Public.MerkleRoot = old.MerkleRoot
		c.Public.InputCount = old.InputCount
		c.Public.OutputCount = old.OutputCount
		c.Public.NullifierRoot = old.NullifierRoot
		c.Public.CommitmentRoot = old.CommitmentRoot
		c.Public.UserDisclosureRoot = old.UserDisclosureRoot
		c.Public.SelfViewRoot = old.FullDisclosureRoot
		plain = []frontend.Variable{c.AssetID}
		for i := 0; i < MaxBatchJoinSplitInputs; i++ {
			var cin frontend.Variable = 0
			if i < inputs {
				cin = fixtureCommitment(c.InputSpendPubKeys[i], c.InputViewPubKeys[i], c.InputAmounts[i], c.AssetID, c.InputRandomness[i])
			}
			plain = append(plain, cin)
		}
		for i := 0; i < MaxBatchJoinSplitOutputs; i++ {
			if i < outputs {
				plain = append(plain, c.OutputAmounts[i], c.OutputSpendPubKeys[i].A.X, c.OutputSpendPubKeys[i].A.Y, c.OutputViewPubKeys[i].A.X, c.OutputViewPubKeys[i].A.Y)
			} else {
				plain = append(plain, 0, 0, 0, 0, 0)
			}
		}
	}
	pvalue := reflect.ValueOf(f.public).Elem()
	for i := 0; i < pvalue.NumField(); i++ {
		pvalue.Field(i).Set(reflect.ValueOf(frontend.Variable(fieldBig(pvalue.Field(i).Interface()))))
	}
	for _, v := range plain {
		f.plain = append(f.plain, auditField(t, v))
	}
	f.encrypt(t)
	return f
}
func initializeAuditPublic(t testing.TB, p *AuditPublic) {
	v := reflect.ValueOf(p).Elem()
	for i := 0; i < v.NumField(); i++ {
		v.Field(i).Set(reflect.ValueOf(frontend.Variable(big.NewInt(0))))
	}
	p.NetworkHi = 101
	p.NetworkLo = 103
	p.KeyEpoch = 1
	p.ExpiresAtUnix = 2_000_000_000
	p.AuxHi = 109
	p.AuxLo = 113
	x, y := pointBigInts(scalarMulBase(big.NewInt(37)))
	point, err := auditfield.NewPoint64Frame(auditField(t, x), auditField(t, y))
	require.NoError(t, err)
	key, err := auditfield.NewAuditKey(point)
	require.NoError(t, err)
	id := key.ID()
	p.PKx = x
	p.PKy = y
	p.KeyIDHi = new(big.Int).SetBytes(id[:16])
	p.KeyIDLo = new(big.Int).SetBytes(id[16:])
}
func (f *auditFixture) context(t testing.TB) auditfield.AuditContext {
	pi := make([]auditfield.Field32, 21)
	for i, v := range f.public.fields()[:21] {
		pi[i] = auditField(t, v)
	}
	ctx, err := auditfield.NewAuditContext(f.kind, pi)
	require.NoError(t, err)
	return ctx
}
func (f *auditFixture) encrypt(t testing.TB) {
	plain, err := auditfield.NewAuditPlain(f.kind, f.plain)
	require.NoError(t, err)
	envelope, root, w, err := auditfield.EncryptAuditForProver(f.context(t), plain)
	require.NoError(t, err)
	defer w.Clear()
	r, err := w.ToProverWitnessBE32()
	require.NoError(t, err)
	f.random.R = new(big.Int).SetBytes(r[:])
	clear(r[:])
	nonce := envelope.Nonce()
	f.random.Nonce = new(big.Int).SetBytes(nonce[:])
	f.envelope = envelope
	f.public.CipherRoot0 = new(big.Int).SetBytes(root.Left[:])
	f.public.CipherRoot1 = new(big.Int).SetBytes(root.Right[:])
	f.resign(t)
}
func (f *auditFixture) resign(t testing.TB) {
	if f.signature == nil {
		return
	}
	transcript, err := f.context(t).T(f.envelope.Nonce())
	require.NoError(t, err)
	fields := []*big.Int{privacytypes.DomainFieldV1(auditfield.AuditOwnerIntentDomain)}
	for _, v := range transcript {
		fields = append(fields, new(big.Int).SetBytes(v[:]))
	}
	fields = append(fields, fieldBig(f.public.CipherRoot0), fieldBig(f.public.CipherRoot1))
	*f.signature = signSpendMessage(t, privacycrypto.MimcHash(fields...), big.NewInt(17), scalarMulBase(big.NewInt(17)))
}

func TestP2AuditRelation(t *testing.T) {
	for _, kind := range []auditfield.Kind{1, 2, 3, 4} {
		t.Run(kindName(kind), func(t *testing.T) {
			f := newAuditFixture(t, kind, 1, 1)
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, reflect.New(reflect.TypeOf(f.assignment).Elem()).Interface().(frontend.Circuit))
			require.NoError(t, err)
			witness, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
			require.NoError(t, err)
			require.NoError(t, ccs.IsSolved(witness))
			for _, attack := range []struct {
				name   string
				mutate func(*auditFixture)
			}{
				{"false_asset_reencrypt_resign", func(f *auditFixture) { f.plain[0] = auditfield.Field32FromUint64(12); f.encrypt(t) }},
				{"false_Cin_or_amount_reencrypt_resign", func(f *auditFixture) { f.plain[1] = auditfield.Field32FromUint64(123); f.encrypt(t) }},
				{"r_zero", func(f *auditFixture) { f.random.R = 0 }},
				{"r_q", func(f *auditFixture) {
					f.random.R, _ = new(big.Int).SetString("2736030358979909402780800718157159386076813972158567259200215660948447373041", 10)
				}},
				{"nonce", func(f *auditFixture) { f.random.Nonce = 0 }},
				{"root", func(f *auditFixture) { f.public.CipherRoot0 = 0 }},
				{"epoch_replay", func(f *auditFixture) { f.public.KeyEpoch = 2 }},
				{"network_replay", func(f *auditFixture) { f.public.NetworkHi = 102 }},
				{"count", func(f *auditFixture) { f.public.InputCount = 31 }},
				{"uint128", func(f *auditFixture) { f.public.AuxHi = new(big.Int).Lsh(big.NewInt(1), 128) }},
			} {
				t.Run(attack.name, func(t *testing.T) {
					bad := newAuditFixture(t, kind, 1, 1)
					attack.mutate(bad)
					w, err := frontend.NewWitness(bad.assignment, ecc.BN254.ScalarField())
					require.NoError(t, err)
					require.NotPanics(t, func() { require.Error(t, ccs.IsSolved(w)) })
				})
			}
		})
	}
}
func kindName(kind auditfield.Kind) string {
	return []string{"", "deposit", "spend", "join", "batch"}[kind]
}
