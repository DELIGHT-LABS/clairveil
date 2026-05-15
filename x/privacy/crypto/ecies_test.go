package crypto

import (
	"crypto/rand"
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
