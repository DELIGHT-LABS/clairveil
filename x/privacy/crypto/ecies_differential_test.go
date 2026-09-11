package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

// The reference side intentionally uses the previous variable-time backend
// on test-only scalars, independent from the new native implementation.
func TestLegacyECIESAndViewTagDifferential(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	for _, receiver := range []byte{1, 7, 37, 201} {
		sk := scalarFromByte(t, receiver)
		var public, ephemeral, shared twistededwards.PointAffine
		public.ScalarMultiplication(&curve.Base, big.NewInt(int64(receiver)))
		ephemeral.ScalarMultiplication(&curve.Base, big.NewInt(101))
		shared.ScalarMultiplication(&public, big.NewInt(101))
		compressed := shared.Bytes()
		key := sha256.Sum256(compressed[:])
		block, err := aes.NewCipher(key[:])
		require.NoError(t, err)
		gcm, err := cipher.NewGCM(block)
		require.NoError(t, err)
		nonce := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
		plain := []byte("legacy wire differential")
		e := ephemeral.Bytes()
		wire := append([]byte(nil), e[:]...)
		wire = append(wire, nonce...)
		wire = gcm.Seal(wire, nonce, plain, nil)
		got, err := AsymDecrypt(wire, sk)
		require.NoError(t, err)
		require.Equal(t, plain, got)

		commitment := make([]byte, 32)
		commitment[31] = 123
		x, y := new(big.Int), new(big.Int)
		shared.X.BigInt(x)
		shared.Y.BigInt(y)
		digest := MimcHash(HashString("clairveil.view_tag.v1"), x, y, big.NewInt(123), big.NewInt(1))
		var digestBytes [32]byte
		digest.FillBytes(digestBytes[:])
		got, err = AsymDecryptWithViewTag(wire, sk, commitment, 1, digestBytes[:ViewTagLength])
		require.NoError(t, err)
		require.Equal(t, plain, got)
		wire[len(wire)-1] ^= 1
		got, err = AsymDecrypt(wire, sk)
		require.Error(t, err)
		require.Nil(t, got)
	}
}
