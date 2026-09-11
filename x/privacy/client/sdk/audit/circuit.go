package audit

import (
	"fmt"
	"math/big"

	privacycircuit "github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
)

// CircuitBinding translates the immutable final PI23 plus its one r/nonce
// pair into the private/public fields of the inactive audit-field circuits.
// It is intentionally in the SDK boundary: it consumes a prepared object but
// does not create notes, select inputs, or serialize a witness.
func (p *Prepared) CircuitBinding() (privacycircuit.AuditPublic, privacycircuit.AuditRandomness, error) {
	var public privacycircuit.AuditPublic
	var random privacycircuit.AuditRandomness
	if p == nil || p.cleared {
		return public, random, fmt.Errorf("prepared audit transaction is cleared")
	}
	values := p.PublicInputs()
	if len(values) != 23 {
		return public, random, fmt.Errorf("prepared audit public input count mismatch")
	}
	fields := make([]*big.Int, len(values))
	for i := range values {
		fields[i] = new(big.Int).SetBytes(values[i][:])
	}
	public = privacycircuit.AuditPublic{
		NetworkHi: fields[0], NetworkLo: fields[1], KeyIDHi: fields[2], KeyIDLo: fields[3], KeyEpoch: fields[4], PKx: fields[5], PKy: fields[6],
		ExpiresAtUnix: fields[7], MerkleRoot: fields[8], InputCount: fields[9], OutputCount: fields[10], NullifierRoot: fields[11], CommitmentRoot: fields[12],
		UserDisclosureRoot: fields[13], SelfViewRoot: fields[14], PublicAmount: fields[15], PublicAsset: fields[16], PublicTargetHi: fields[17], PublicTargetLo: fields[18],
		AuxHi: fields[19], AuxLo: fields[20], CipherRoot0: fields[21], CipherRoot1: fields[22],
	}
	r, err := p.ProverScalarBE32()
	if err != nil {
		return privacycircuit.AuditPublic{}, privacycircuit.AuditRandomness{}, err
	}
	defer clear(r[:])
	nonce := p.envelope.Nonce()
	random.R, random.Nonce = new(big.Int).SetBytes(r[:]), new(big.Int).SetBytes(nonce[:])
	return public, random, nil
}
