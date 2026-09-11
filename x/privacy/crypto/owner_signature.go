package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
	"io"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

// SignOwnerIntent creates the existing R||S EdDSA owner signature with a
// nonzero scalar nonce. It retries a fresh nonce if the response is zero.
func SignOwnerIntent(message [32]byte, secret SecretScalar, reader io.Reader) (out []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = signOwnerIntent(message, secret, reader) })
	return out, err
}

func signOwnerIntent(message [32]byte, secret SecretScalar, reader io.Reader) ([]byte, error) {
	if err := secretprofile.Check(); err != nil {
		return nil, err
	}
	msg, err := ParseFieldValueBE32(message[:])
	if err != nil {
		return nil, fmt.Errorf("owner intent must be canonical Fr: %w", err)
	}
	if reader == nil {
		reader = rand.Reader
	}
	return signOwnerIntentWithChallenge(msg, secret, reader, ownerIntentChallenge)
}

type ownerChallengeFunc func(r, public SecretPoint, message FieldValue) (scalarct.Scalar, error)

func signOwnerIntentWithChallenge(message FieldValue, secret SecretScalar, reader io.Reader, challenge ownerChallengeFunc) ([]byte, error) {
	a, err := secret.scalar()
	defer secretmem.Clear(&a)
	defer secretmem.Clear(&secret)
	if err != nil {
		return nil, err
	}
	public, err := publicPointForSecret(secret)
	if err != nil {
		return nil, err
	}
	for {
		k, err := SampleSecretScalar(reader)
		if err != nil {
			return nil, err
		}
		r, err := publicPointForSecret(k)
		if err != nil {
			secretmem.Clear(&k)
			return nil, err
		}
		h, err := challenge(r, public, message)
		if err != nil {
			secretmem.Clear(&k)
			return nil, err
		}
		var response scalarct.Scalar
		subtle.WithDataIndependentTiming(func() { response = scalarct.PoPResponse(k.value.Scalar(), a.Scalar(), scalarBytes(h)) })
		secretmem.Clear(&k)
		secretmem.Clear(&h)
		if response.IsZero() == 1 {
			continue
		}
		rb, err := r.legacyCompressed()
		if err != nil {
			return nil, err
		}
		sb := response.Bytes()
		secretmem.Clear(&response)
		defer clear(sb[:])
		out := make([]byte, 64)
		copy(out[:32], rb[:])
		copy(out[32:], sb[:])
		return out, nil
	}
}

func ownerIntentChallenge(r, public SecretPoint, message FieldValue) (scalarct.Scalar, error) {
	rx, ry, err := r.affineFieldValues()
	if err != nil {
		return scalarct.Zero(), err
	}
	ax, ay, err := public.affineFieldValues()
	if err != nil {
		return scalarct.Zero(), err
	}
	h := legacyMiMCCore(rx, ry, ax, ay, message)
	return scalarct.Reduce256(h.Bytes()), nil
}

func scalarBytes(s scalarct.Scalar) [32]byte { return s.Bytes() }
