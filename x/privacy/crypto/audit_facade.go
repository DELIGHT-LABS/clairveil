package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

// The crypto package exposes auditfield's fixed DTOs without introducing a
// reverse import from the leaf codec to parent crypto, types, or SDK layers.
type (
	AuditKey        = auditfield.AuditKey
	AuditContext    = auditfield.AuditContext
	AuditPlain      = auditfield.AuditPlain
	AuditEnvelope   = auditfield.EnvelopeFrame
	AuditCipherRoot = auditfield.CipherRoot
	AuditPoP96      = auditfield.PoP96
	AuditKind       = auditfield.Kind
	AuditPoint64    = auditfield.Point64
)

var ErrAuditDecrypt = auditfield.ErrAuditDecrypt

// ParseAuditKey64 verifies Point64 and recomputes KeyID. An arbitrary ID is
// never trusted independently of its registered public key.
func ParseAuditKey64(point, keyID []byte) (AuditKey, error) {
	return auditfield.ParseAuditKey(point, keyID)
}

func NewAuditKeyFromPoint64(point []byte) (AuditKey, error) {
	return auditfield.NewAuditKeyFromPoint64(point)
}

// AuditKeyFromSecret derives the canonical audit public key from a typed
// secret scalar. It is separate from legacy PublicKey, whose return type is a
// compatibility gnark point used by existing callers.
func AuditKeyFromSecret(sk AuditSecretKey) (out AuditKey, err error) {
	if err = secretprofile.Check(); err != nil {
		return AuditKey{}, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = auditKeyFromSecret(sk) })
	return out, err
}

func auditKeyFromSecret(sk AuditSecretKey) (AuditKey, error) {
	if err := secretprofile.Check(); err != nil {
		return AuditKey{}, err
	}
	scalar, err := sk.scalar()
	defer secretmem.Clear(&scalar)
	if err != nil {
		return AuditKey{}, err
	}
	base := edwardsct.Base()
	var point edwardsct.Point
	var raw [64]byte
	subtle.WithDataIndependentTiming(func() {
		point.ScalarMultNonzero(&base, scalar)
		raw, err = point.EncodePoint64()
	})
	if err != nil {
		return AuditKey{}, err
	}
	decoded, err := auditfield.ParsePoint64(raw[:])
	if err != nil {
		return AuditKey{}, err
	}
	return auditfield.NewAuditKey(decoded)
}

// EncryptAudit samples independent r in q* and a 128-bit nonce from the
// system CSPRNG. The only caller-controlled values are typed fixed context
// and plaintext; arbitrary T, AD, nonce, or raw field cipher APIs are absent.
func EncryptAudit(context AuditContext, plain AuditPlain) (AuditEnvelope, AuditCipherRoot, error) {
	if err := secretprofile.Check(); err != nil {
		return AuditEnvelope{}, AuditCipherRoot{}, err
	}
	var envelope AuditEnvelope
	var root AuditCipherRoot
	var err error
	subtle.WithDataIndependentTiming(func() { envelope, root, err = auditfield.EncryptAudit(context, plain) })
	if err != nil {
		return AuditEnvelope{}, AuditCipherRoot{}, err
	}
	return envelope, root, nil
}

// DecryptAudit returns nil plus ErrAuditDecrypt for every primitive failure.
// It does not claim that successfully decrypted bytes have chain provenance.
func DecryptAudit(sk AuditSecretKey, context AuditContext, envelope AuditEnvelope, expectedRoot AuditCipherRoot) (out *AuditPlain, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = decryptAudit(sk, context, envelope, expectedRoot) })
	return out, err
}

func decryptAudit(sk AuditSecretKey, context AuditContext, envelope AuditEnvelope, expectedRoot AuditCipherRoot) (*AuditPlain, error) {
	scalar, err := sk.scalar()
	defer secretmem.Clear(&scalar)
	if err != nil {
		return nil, ErrAuditDecrypt
	}
	var plain *AuditPlain
	subtle.WithDataIndependentTiming(func() { plain, err = auditfield.DecryptAudit(scalar, context, envelope, expectedRoot) })
	scalar = scalarct.NonzeroScalar{}
	if err != nil {
		return nil, ErrAuditDecrypt
	}
	return plain, nil
}

// CreateAuditPoP96 makes the exact offline §13 key-possession proof with a
// fresh q* nonce. The response may canonically be zero.
func CreateAuditPoP96(sk AuditSecretKey, network [32]byte, epoch, activationHeight uint64) (out AuditPoP96, err error) {
	if err = secretprofile.Check(); err != nil {
		return AuditPoP96{}, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = createAuditPoP96(sk, network, epoch, activationHeight) })
	return out, err
}

func createAuditPoP96(sk AuditSecretKey, network [32]byte, epoch, activationHeight uint64) (AuditPoP96, error) {
	if err := secretprofile.Check(); err != nil {
		return AuditPoP96{}, err
	}
	scalar, err := sk.scalar()
	defer secretmem.Clear(&scalar)
	if err != nil {
		return AuditPoP96{}, err
	}
	nonce, err := scalarct.SampleNonzeroScalar(rand.Reader)
	defer secretmem.Clear(&nonce)
	if err != nil {
		return AuditPoP96{}, err
	}
	key, err := AuditKeyFromSecret(sk)
	if err != nil {
		return AuditPoP96{}, err
	}
	var proof AuditPoP96
	subtle.WithDataIndependentTiming(func() { proof, err = auditfield.CreatePoP96(key, scalar, nonce, network, epoch, activationHeight) })
	scalar, nonce = scalarct.NonzeroScalar{}, scalarct.NonzeroScalar{}
	return proof, err
}

func VerifyAuditPoP96(key AuditKey, network [32]byte, epoch, activationHeight uint64, proof AuditPoP96) error {
	if err := auditfield.VerifyPoP96(key, network, epoch, activationHeight, proof); err != nil {
		return fmt.Errorf("invalid audit key PoP: %w", err)
	}
	return nil
}
