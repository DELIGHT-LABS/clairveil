package crypto

import (
	"errors"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

func scalarFromByte(t *testing.T, b byte) SecretScalar {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = b
	s, err := ImportNonzeroScalarBE32(raw)
	require.NoError(t, err)
	return s
}

func TestAsymEncryptDecrypt(t *testing.T) {
	bob := scalarFromByte(t, 7)
	bobPub, err := PublicKey(bob)
	require.NoError(t, err)
	msg := []byte("legacy ECIES bytes remain stable")
	ciphertext, err := AsymEncrypt(msg, *bobPub)
	require.NoError(t, err)
	plain, err := AsymDecrypt(ciphertext, bob)
	require.NoError(t, err)
	require.Equal(t, msg, plain)
}

func TestAsymEncryptDecryptWithViewTag(t *testing.T) {
	receiver := scalarFromByte(t, 9)
	pub, err := PublicKey(receiver)
	require.NoError(t, err)
	commitment := make([]byte, 32)
	commitment[31] = 0x11
	msg := []byte("view-tagged")
	ciphertext, tag, err := AsymEncryptWithViewTag(msg, *pub, commitment, 1)
	require.NoError(t, err)
	plain, err := AsymDecryptWithViewTag(ciphertext, receiver, commitment, 1, tag)
	require.NoError(t, err)
	require.Equal(t, msg, plain)
	_, err = AsymDecryptWithViewTag(ciphertext, receiver, commitment, 0, tag)
	require.ErrorIs(t, err, ErrViewTagMismatch)
	wrong := append([]byte(nil), tag...)
	wrong[0] ^= 1
	_, err = AsymDecryptWithViewTag(ciphertext, receiver, commitment, 1, wrong)
	require.True(t, errors.Is(err, ErrViewTagMismatch))
}

func TestAsymDecryptRejectsMalformedEphemeralAndZeroSecret(t *testing.T) {
	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	identityBytes := identity.Bytes()
	envelope := make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize)
	copy(envelope, identityBytes[:])
	_, err := AsymDecrypt(envelope, scalarFromByte(t, 1))
	require.Error(t, err)
	_, err = AsymDecrypt(make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize-1), SecretScalar{})
	require.Error(t, err)
}

func FuzzAsymDecryptMalformedEnvelopeNoPanic(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize))
	f.Fuzz(func(t *testing.T, envelope []byte) {
		s := scalarFromByte(t, 1)
		_, _ = AsymDecrypt(envelope, s)
		_, _ = AsymDecryptWithViewTag(envelope, s, make([]byte, 32), 0, make([]byte, ViewTagLength))
	})
}
