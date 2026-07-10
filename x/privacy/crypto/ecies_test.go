package crypto

import (
	"crypto/rand"
	"errors"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

func TestAsymEncryptDecrypt(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	// 1. Bob(수신자)의 키 생성
	bobPriv, _ := rand.Int(rand.Reader, &curve.Order)
	var bobPub twistededwards.PointAffine
	bobPub.ScalarMultiplication(&curve.Base, bobPriv)

	// 2. 메시지 준비
	originalMsg := []byte(`{"amount": 100, "randomness": "12345", "denom": "uclair"}`)

	// 3. Alice가 Bob의 공개키로 암호화
	ciphertext, err := AsymEncrypt(originalMsg, bobPub)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)

	t.Logf("Ciphertext Hex: %x", ciphertext)

	// 4. Bob이 자신의 비밀키로 복호화
	decryptedMsg, err := AsymDecrypt(ciphertext, bobPriv)
	require.NoError(t, err)

	// 5. 결과 확인
	require.Equal(t, originalMsg, decryptedMsg)
	t.Logf("Decrypted Message: %s", string(decryptedMsg))
}

func TestDecryptFailure(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	// Bob의 키
	bobPriv, _ := rand.Int(rand.Reader, &curve.Order)
	var bobPub twistededwards.PointAffine
	bobPub.ScalarMultiplication(&curve.Base, bobPriv)

	// Eve(해커)의 키
	evePriv, _ := rand.Int(rand.Reader, &curve.Order)

	// 암호화 (for Bob)
	msg := []byte("secret")
	ciphertext, _ := AsymEncrypt(msg, bobPub)

	// Eve가 복호화 시도 -> 실패해야 함
	_, err := AsymDecrypt(ciphertext, evePriv)
	require.Error(t, err)
	t.Log("Successfully failed to decrypt with wrong key")
}

func TestAsymEncryptDecryptWithViewTag(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	receiverPriv, err := rand.Int(rand.Reader, &curve.Order)
	require.NoError(t, err)
	var receiverPub twistededwards.PointAffine
	receiverPub.ScalarMultiplication(&curve.Base, receiverPriv)

	commitment := make([]byte, 32)
	commitment[31] = 0x11
	originalMsg := []byte(`{"amount": 100}`)

	ciphertext, viewTag, err := AsymEncryptWithViewTag(originalMsg, receiverPub, commitment, 1)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)
	require.Len(t, viewTag, ViewTagLength)

	decryptedMsg, err := AsymDecryptWithViewTag(ciphertext, receiverPriv, commitment, 1, viewTag)
	require.NoError(t, err)
	require.Equal(t, originalMsg, decryptedMsg)

	_, err = AsymDecryptWithViewTag(ciphertext, receiverPriv, commitment, 0, viewTag)
	require.ErrorIs(t, err, ErrViewTagMismatch)

	wrongTag := append([]byte(nil), viewTag...)
	wrongTag[0] ^= 0xff
	_, err = AsymDecryptWithViewTag(ciphertext, receiverPriv, commitment, 1, wrongTag)
	require.True(t, errors.Is(err, ErrViewTagMismatch))
}

func TestAsymEncryptRejectsIdentityAndNonSubgroupReceiver(t *testing.T) {
	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	_, err := AsymEncrypt([]byte("secret"), identity)
	require.ErrorContains(t, err, "identity point is not allowed")

	orderTwo := twistededwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	require.True(t, orderTwo.IsOnCurve())
	require.False(t, orderTwo.IsZero())
	_, err = AsymEncrypt([]byte("secret"), orderTwo)
	require.ErrorContains(t, err, "point is not in the prime-order subgroup")
}

func TestAsymDecryptRejectsMalformedEphemeralPoint(t *testing.T) {
	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	identityBytes := identity.Bytes()

	envelope := make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize)
	copy(envelope, identityBytes[:])
	_, err := AsymDecrypt(envelope, big.NewInt(1))
	require.ErrorContains(t, err, "invalid ephemeral public key")
	require.ErrorContains(t, err, "identity point is not allowed")

	orderTwo := twistededwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	orderTwoBytes := orderTwo.Bytes()
	copy(envelope, orderTwoBytes[:])
	_, err = AsymDecrypt(envelope, big.NewInt(1))
	require.ErrorContains(t, err, "point is not in the prime-order subgroup")
}

func TestAsymDecryptRejectsInvalidPrivateScalarWithoutPanic(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	var receiver twistededwards.PointAffine
	receiver.ScalarMultiplication(&curve.Base, big.NewInt(7))
	ciphertext, err := AsymEncrypt([]byte("secret"), receiver)
	require.NoError(t, err)

	invalidScalars := []*big.Int{
		nil,
		big.NewInt(0),
		big.NewInt(-1),
		new(big.Int).Set(&curve.Order),
	}
	for _, scalar := range invalidScalars {
		_, err := AsymDecrypt(ciphertext, scalar)
		require.ErrorContains(t, err, "invalid ECIES private scalar")
	}
}

func TestAsymDecryptRejectsTruncatedEnvelope(t *testing.T) {
	_, err := AsymDecrypt(make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize-1), big.NewInt(1))
	require.ErrorContains(t, err, "invalid ciphertext length")
}

func FuzzAsymDecryptMalformedEnvelopeNoPanic(f *testing.F) {
	curve := twistededwards.GetEdwardsCurve()
	validCiphertext, err := AsymEncrypt([]byte("fuzz seed"), curve.Base)
	if err != nil {
		f.Fatalf("failed to create valid ECIES seed: %v", err)
	}

	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	identityBytes := identity.Bytes()
	identityEnvelope := make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize)
	copy(identityEnvelope, identityBytes[:])

	f.Add([]byte(nil))
	f.Add(make([]byte, CanonicalPointSize+asymNonceSize-1))
	f.Add(make([]byte, CanonicalPointSize+asymNonceSize+asymTagSize))
	f.Add(identityEnvelope)
	f.Add(validCiphertext)

	commitment := make([]byte, 32)
	viewTag := make([]byte, ViewTagLength)
	f.Fuzz(func(t *testing.T, envelope []byte) {
		_, _ = AsymDecrypt(envelope, big.NewInt(1))
		_, _ = AsymDecryptWithViewTag(envelope, big.NewInt(1), commitment, 0, viewTag)
	})
}
