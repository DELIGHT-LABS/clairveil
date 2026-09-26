package circuit

import (
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	ecced "github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"
)

// AuditPublic is the exact PI23 order for the inactive next circuit set.
// The active registry is intentionally unchanged until the v2 release.
type AuditPublic struct {
	NetworkHi, NetworkLo             frontend.Variable
	KeyIDHi, KeyIDLo                 frontend.Variable
	KeyEpoch                         frontend.Variable
	PKx, PKy                         frontend.Variable
	ExpiresAtUnix                    frontend.Variable
	MerkleRoot                       frontend.Variable
	InputCount, OutputCount          frontend.Variable
	NullifierRoot, CommitmentRoot    frontend.Variable
	UserDisclosureRoot, SelfViewRoot frontend.Variable
	PublicAmount, PublicAsset        frontend.Variable
	PublicTargetHi, PublicTargetLo   frontend.Variable
	AuxHi, AuxLo                     frontend.Variable
	CipherRoot0, CipherRoot1         frontend.Variable
}

func (p *AuditPublic) fields() []frontend.Variable {
	return []frontend.Variable{p.NetworkHi, p.NetworkLo, p.KeyIDHi, p.KeyIDLo, p.KeyEpoch, p.PKx, p.PKy, p.ExpiresAtUnix, p.MerkleRoot, p.InputCount, p.OutputCount, p.NullifierRoot, p.CommitmentRoot, p.UserDisclosureRoot, p.SelfViewRoot, p.PublicAmount, p.PublicAsset, p.PublicTargetHi, p.PublicTargetLo, p.AuxHi, p.AuxLo, p.CipherRoot0, p.CipherRoot1}
}

type AuditRandomness struct{ R, Nonce frontend.Variable }

// KeyID derivation, registered network/key/epoch and actual-message validation
// remain host checks. The circuit binds those supplied values through T/root.
func (p *AuditPublic) constrain(api frontend.API, kind auditfield.Kind) {
	for _, v := range []frontend.Variable{p.NetworkHi, p.NetworkLo, p.KeyIDHi, p.KeyIDLo, p.PublicTargetHi, p.PublicTargetLo, p.AuxHi, p.AuxLo} {
		api.ToBinary(v, 128)
	}
	for _, v := range []frontend.Variable{p.KeyEpoch, p.ExpiresAtUnix, p.PublicAmount} {
		api.ToBinary(v, 64)
	}
	api.AssertIsDifferent(p.KeyEpoch, 0)
	api.AssertIsDifferent(p.ExpiresAtUnix, 0)
	api.ToBinary(p.InputCount, 5)
	api.ToBinary(p.OutputCount, 6)
	switch kind {
	case auditfield.KindDeposit:
		api.AssertIsEqual(p.InputCount, 0)
		api.AssertIsEqual(p.OutputCount, 1)
		api.AssertIsEqual(p.MerkleRoot, 0)
		api.AssertIsEqual(p.NullifierRoot, 0)
		api.AssertIsEqual(p.UserDisclosureRoot, 0)
		api.AssertIsEqual(p.SelfViewRoot, 0)
	case auditfield.KindWithdraw:
		api.AssertIsEqual(p.InputCount, 1)
		api.AssertIsEqual(p.OutputCount, 0)
		api.AssertIsEqual(p.CommitmentRoot, 0)
		api.AssertIsEqual(p.UserDisclosureRoot, 0)
		api.AssertIsEqual(p.SelfViewRoot, 0)
		api.AssertIsDifferent(p.PublicAmount, 0)
	case auditfield.KindTransfer2x2:
		api.AssertIsEqual(p.InputCount, 2)
		api.AssertIsEqual(p.OutputCount, 2)
		fallthrough
	case auditfield.KindBatch16x32:
		api.AssertIsEqual(p.PublicAmount, 0)
		api.AssertIsEqual(p.PublicAsset, 0)
		api.AssertIsEqual(p.PublicTargetHi, 0)
		api.AssertIsEqual(p.PublicTargetLo, 0)
	}
}

// auditRelation receives only values computed from the original note relation.
// There is no independent plaintext, ephemeral point, or ciphertext witness.
func auditRelation(api frontend.API, p *AuditPublic, random AuditRandomness, kind auditfield.Kind, plain []frontend.Variable, owner *eddsa.PublicKey, signature *eddsa.Signature) error {
	p.constrain(api, kind)
	api.ToBinary(random.Nonce, 128)
	curve, err := twistededwards.NewEdCurve(api, ecced.BN254)
	if err != nil {
		return err
	}
	pk := twistededwards.Point{X: p.PKx, Y: p.PKy}
	assertPrimeSubgroupPoint(api, curve, pk)
	api.AssertIsDifferent(random.R, 0)
	api.AssertIsLessOrEqual(random.R, new(big.Int).Sub(curve.Params().Order, big.NewInt(1)))
	bits := api.ToBinary(random.R, curve.Params().Order.BitLen())
	// Fixed double/add avoids the half-GCD hint's panic at malicious r=q.
	mul := func(point twistededwards.Point) twistededwards.Point {
		acc := twistededwards.Point{X: 0, Y: 1}
		for i := len(bits) - 1; i >= 0; i-- {
			acc = curve.Double(acc)
			sum := curve.Add(acc, point)
			acc = twistededwards.Point{X: api.Select(bits[i], sum.X, acc.X), Y: api.Select(bits[i], sum.Y, acc.Y)}
		}
		return acc
	}
	e := mul(twistededwards.Point{X: curve.Params().Base[0], Y: curve.Params().Base[1]})
	z := mul(pk)
	transcript := append([]frontend.Variable{auditfield.AuditVersion, auditfield.AuditSuite, uint8(kind)}, p.fields()[:21]...)
	transcript = append(transcript, random.Nonce)
	kdf := append([]frontend.Variable{z.X, z.Y, e.X, e.Y}, transcript...)
	key := auditH2(api, 0, kdf)
	ae := newAuditSponge(api, int(kind))
	ae.absorb(key[:]...)
	info := append(append([]frontend.Variable{}, transcript...), e.X, e.Y, len(plain))
	ae.absorb(info...)
	cipher := make([]frontend.Variable, len(plain))
	for i, m := range plain {
		cipher[i] = api.Add(m, ae.squeeze())
		ae.absorb(m)
	}
	tag := ae.squeeze()
	rootInput := append(append([]frontend.Variable{}, transcript...), e.X, e.Y)
	rootInput = append(rootInput, cipher...)
	rootInput = append(rootInput, tag)
	root := auditH2(api, 4+int(kind), rootInput)
	api.AssertIsEqual(root[0], p.CipherRoot0)
	api.AssertIsEqual(root[1], p.CipherRoot1)
	if owner != nil {
		h, _ := mimc.NewMiMC(api)
		values := append([]frontend.Variable{privacytypes.DomainFieldV1(auditfield.AuditOwnerIntentDomain)}, transcript...)
		values = append(values, p.CipherRoot0, p.CipherRoot1)
		h.Write(values...)
		intent := h.Sum()
		h.Reset()
		return eddsa.Verify(curve, *signature, intent, *owner, &h)
	}
	return nil
}

type auditSponge struct {
	api                frontend.API
	state              [3]frontend.Variable
	absorbed, squeezed bool
}

func newAuditSponge(api frontend.API, index int) *auditSponge {
	tags := auditfield.StaticTags()
	return &auditSponge{api: api, state: [3]frontend.Variable{0, new(big.Int).SetBytes(tags[index][0][:]), new(big.Int).SetBytes(tags[index][1][:])}}
}
func (s *auditSponge) absorb(values ...frontend.Variable) {
	for _, v := range values {
		if s.absorbed {
			s.permute()
		}
		s.state[0] = s.api.Add(s.state[0], v)
		s.absorbed = true
	}
	s.squeezed = true
}
func (s *auditSponge) squeeze() frontend.Variable {
	if s.squeezed {
		s.permute()
		s.absorbed = false
	}
	s.squeezed = true
	return s.state[0]
}
func auditH2(api frontend.API, index int, values []frontend.Variable) [2]frontend.Variable {
	s := newAuditSponge(api, index)
	s.absorb(values...)
	return [2]frontend.Variable{s.squeeze(), s.squeeze()}
}
func (s *auditSponge) permute() {
	api := s.api
	external := func() {
		sum := api.Add(s.state[0], s.state[1], s.state[2])
		for i := range s.state {
			s.state[i] = api.Add(s.state[i], sum)
		}
	}
	pow5 := func(v frontend.Variable) frontend.Variable { v2 := api.Mul(v, v); return api.Mul(v2, v2, v) }
	external()
	constants := auditfield.Poseidon2RoundConstants()
	index := 0
	for round := 0; round < auditfield.Poseidon2FullRounds+auditfield.Poseidon2PartialRounds; round++ {
		full := round < 4 || round >= 60
		width := 1
		if full {
			width = 3
		}
		for i := 0; i < width; i++ {
			s.state[i] = api.Add(s.state[i], new(big.Int).SetBytes(constants[index][:]))
			index++
		}
		if full {
			for i := range s.state {
				s.state[i] = pow5(s.state[i])
			}
			external()
		} else {
			s.state[0] = pow5(s.state[0])
			sum := api.Add(s.state[0], s.state[1], s.state[2])
			s.state[0] = api.Add(s.state[0], sum)
			s.state[1] = api.Add(s.state[1], sum)
			s.state[2] = api.Add(api.Mul(2, s.state[2]), sum)
		}
	}
}

func auditVector(api frontend.API, kind privacytypes.BatchVectorKindV1, values []frontend.Variable) frontend.Variable {
	h, _ := mimc.NewMiMC(api)
	enabled := make([]frontend.Variable, len(values))
	for i := range enabled {
		enabled[i] = 1
	}
	return batchVectorRootCircuit(api, &h, kind, len(values), values, enabled)
}
