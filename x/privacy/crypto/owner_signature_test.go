package crypto

import (
	"bytes"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
	"github.com/stretchr/testify/require"
)

func TestSignOwnerIntentVerifiesWithLegacyEdDSA(t *testing.T) {
	secret := scalarFromByte(t, 13)
	public, err := PublicKey(secret)
	require.NoError(t, err)
	var message [32]byte
	message[31] = 7
	signature, err := SignOwnerIntent(message, secret, bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
	require.NoError(t, err)
	verifier := cryptoeddsa.PublicKey{A: *public}
	valid, err := verifier.Verify(signature, message[:], mimc.NewMiMC())
	require.NoError(t, err)
	require.True(t, valid)
}

func TestOwnerSignatureRetriesZeroResponse(t *testing.T) {
	secret := scalarFromByte(t, 1)
	var message FieldValue
	called := 0
	randomness := make([]byte, 64)
	randomness[31], randomness[63] = 1, 2
	signature, err := signOwnerIntentWithChallenge(message, secret, bytes.NewReader(randomness), func(_ SecretPoint, _ SecretPoint, _ FieldValue) (scalarct.Scalar, error) {
		called++
		if called == 1 {
			var oneBytes [32]byte
			oneBytes[31] = 1
			one, ok := scalarct.FromCanonicalBE(oneBytes)
			require.True(t, ok)
			var neg scalarct.Scalar
			neg.Neg(&one)
			return neg, nil
		}
		return scalarct.Zero(), nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, called)
	require.Len(t, signature, 64)
}
