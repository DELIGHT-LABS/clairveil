package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretFacadesRejectDisabledAcceleration(t *testing.T) {
	sk := scalarFromByte(t, 7)
	pub, err := PublicKey(sk)
	require.NoError(t, err)
	ciphertext, err := AsymEncrypt([]byte("private"), *pub)
	require.NoError(t, err)
	seedCiphertext, err := Encrypt([]byte("private"), make([]byte, 32))
	require.NoError(t, err)
	t.Setenv("GODEBUG", "cpu.all=off")
	_, err = DeriveIdentityScalarSeed32([32]byte{})
	require.Error(t, err)
	_, err = PublicKey(sk)
	require.Error(t, err)
	_, err = LegacyMiMCHash(FieldValueFromUint64(1))
	require.Error(t, err)
	_, err = SignOwnerIntent([32]byte{}, sk, nil)
	require.Error(t, err)
	out, err := AsymEncrypt([]byte("private"), *pub)
	require.Error(t, err)
	require.Nil(t, out)
	out, err = AsymDecrypt(ciphertext, sk)
	require.Error(t, err)
	require.Nil(t, out)
	out, err = Encrypt([]byte("private"), make([]byte, 32))
	require.Error(t, err)
	require.Nil(t, out)
	out, err = Decrypt(seedCiphertext, make([]byte, 32))
	require.Error(t, err)
	require.Nil(t, out)
}
