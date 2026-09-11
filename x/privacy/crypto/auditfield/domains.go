package auditfield

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"math"
)

// NetworkDigest binds the chain ID, fixed genesis network nonce and circuit
// set. It does not authenticate a genesis document or a remote provider.
func NetworkDigest(chainID string, genesisNonce [32]byte) ([32]byte, error) {
	if len(chainID) == 0 || uint64(len(chainID)) > math.MaxUint32 {
		return [32]byte{}, errors.New("invalid network chain ID length")
	}
	for i := range len(chainID) {
		if chainID[i] < 0x20 || chainID[i] > 0x7e {
			return [32]byte{}, errors.New("network chain ID must be printable ASCII")
		}
	}
	h := sha256.New()
	h.Write([]byte(AuditNetworkDomain))
	writeDomainLP(h, []byte(chainID))
	h.Write(genesisNonce[:])
	writeDomainLP(h, []byte(CircuitSetID))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// PublicTargetDigest binds the actual deposit funder or withdraw recipient.
// The message adapter is responsible for converting a canonical address to
// address bytes. Transfer and batch have literal zero target PI fields.
func PublicTargetDigest(kind Kind, addressBytes []byte) ([32]byte, error) {
	if kind != KindDeposit && kind != KindWithdraw {
		return [32]byte{}, ErrInvalidKind
	}
	if len(addressBytes) == 0 || uint64(len(addressBytes)) > math.MaxUint32 {
		return [32]byte{}, errors.New("invalid public target address length")
	}
	h := sha256.New()
	h.Write([]byte(AuditPublicTargetDomain))
	h.Write([]byte{byte(kind)})
	writeDomainLP(h, addressBytes)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// DigestFields splits a digest losslessly into canonical uint128 Fr values.
func DigestFields(digest [32]byte) (hi, lo Field32) {
	copy(hi[16:], digest[:16])
	copy(lo[16:], digest[16:])
	return
}

func writeDomainLP(h hash.Hash, value []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(value)))
	h.Write(n[:])
	h.Write(value)
}
