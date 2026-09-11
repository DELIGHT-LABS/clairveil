package auditfield

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
)

const (
	proofOfPossessionSize = PointSize + FieldSize
	popDomain             = "clairveil.audit.key-pop.v1"
)

// PoP96 is an offline Schnorr proof of audit key possession. Its response is
// a canonical Scalar, deliberately allowing zero as required by §13.
type PoP96 struct {
	r Point64
	s scalarct.Scalar
}

func ParsePoP96(raw []byte) (PoP96, error) {
	if len(raw) != proofOfPossessionSize {
		return PoP96{}, fmt.Errorf("PoP must be exactly %d bytes", proofOfPossessionSize)
	}
	r, err := ParsePoint64(raw[:PointSize])
	if err != nil {
		return PoP96{}, err
	}
	var scalar scalarct.Scalar
	if err := scalar.SetBytes(raw[PointSize:]); err != nil {
		return PoP96{}, fmt.Errorf("invalid PoP response")
	}
	return PoP96{r: r, s: scalar}, nil
}

func (p PoP96) Bytes() ([]byte, error) {
	if _, err := p.r.validPoint(); err != nil {
		return nil, err
	}
	response := p.s.Bytes()
	out := make([]byte, 0, proofOfPossessionSize)
	out = append(out, p.r.encoded[:]...)
	out = append(out, response[:]...)
	return out, nil
}

// CreatePoP96 binds key possession to the exact network, epoch, activation
// height and suite transcript. nonce must be freshly sampled from q* by the
// caller; s=0 remains a valid output.
func CreatePoP96(key AuditKey, sk, nonce scalarct.NonzeroScalar, network [32]byte, epoch, activationHeight uint64) (PoP96, error) {
	defer secretmem.Clear(&sk)
	defer secretmem.Clear(&nonce)
	if err := key.validate(); err != nil || sk.IsValid() != 1 || nonce.IsValid() != 1 {
		return PoP96{}, fmt.Errorf("invalid PoP input")
	}
	base := edwardsct.Base()
	var own, r edwardsct.Point
	own.ScalarMultNonzero(&base, sk)
	registered, err := key.point.validPoint()
	if err != nil || !own.Equal(&registered) {
		return PoP96{}, fmt.Errorf("PoP secret does not own audit key")
	}
	r.ScalarMultNonzero(&base, nonce)
	encodedR, err := r.EncodePoint64()
	if err != nil {
		return PoP96{}, err
	}
	r64, err := ParsePoint64(encodedR[:])
	if err != nil {
		return PoP96{}, err
	}
	challenge := popChallenge(network, epoch, activationHeight, key.point, r64)
	response := scalarct.PoPResponse(nonce.Scalar(), sk.Scalar(), challenge)
	return PoP96{r: r64, s: response}, nil
}

func VerifyPoP96(key AuditKey, network [32]byte, epoch, activationHeight uint64, proof PoP96) error {
	if err := key.validate(); err != nil {
		return err
	}
	r, err := proof.r.validPoint()
	if err != nil {
		return err
	}
	pk, err := key.point.validPoint()
	if err != nil {
		return err
	}
	challenge := scalarct.Reduce256(popChallenge(network, epoch, activationHeight, key.point, proof.r))
	base := edwardsct.Base()
	var left, challengePK, right edwardsct.Point
	left.ScalarMult(&base, proof.s)
	challengePK.ScalarMult(&pk, challenge)
	right.Add(&r, &challengePK)
	if !left.Equal(&right) {
		return fmt.Errorf("invalid audit key PoP")
	}
	return nil
}

func popChallenge(network [32]byte, epoch, activationHeight uint64, pk, r Point64) [32]byte {
	var values [18]byte
	binary.BigEndian.PutUint64(values[:8], epoch)
	binary.BigEndian.PutUint64(values[8:16], activationHeight)
	binary.BigEndian.PutUint16(values[16:], AuditSuite)
	h := sha256.New()
	_, _ = h.Write([]byte(popDomain))
	_, _ = h.Write(network[:])
	_, _ = h.Write(values[:])
	_, _ = h.Write(pk.encoded[:])
	_, _ = h.Write(r.encoded[:])
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return digest
}
