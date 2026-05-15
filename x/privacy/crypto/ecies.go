package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

// AsymEncrypt encrypts msg to receiverPubKey and returns
// [ephemeral public key | nonce | ciphertext].
func AsymEncrypt(msg []byte, receiverPubKey twistededwards.PointAffine) ([]byte, error) {
	curve := twistededwards.GetEdwardsCurve()

	ephemeralPriv, err := rand.Int(rand.Reader, &curve.Order)
	if err != nil {
		return nil, err
	}

	var ephemeralPub twistededwards.PointAffine
	ephemeralPub.ScalarMultiplication(&curve.Base, ephemeralPriv)

	var sharedPoint twistededwards.PointAffine
	sharedPoint.ScalarMultiplication(&receiverPubKey, ephemeralPriv)

	sharedBytes := sharedPoint.Bytes()
	sharedSecret := sha256.Sum256(sharedBytes[:])

	block, err := aes.NewCipher(sharedSecret[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, msg, nil)

	ephemeralPubBytes := ephemeralPub.Bytes()

	result := make([]byte, 0, len(ephemeralPubBytes)+len(nonce)+len(ciphertext))
	result = append(result, ephemeralPubBytes[:]...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// AsymDecrypt decrypts a ciphertext produced by AsymEncrypt.
func AsymDecrypt(fullCipherBytes []byte, myPrivKey *big.Int) ([]byte, error) {
	pointSize := 32
	nonceSize := 12

	if len(fullCipherBytes) < pointSize+nonceSize {
		return nil, errors.New("invalid ciphertext length")
	}

	ephemeralPubBytes := fullCipherBytes[:pointSize]
	nonce := fullCipherBytes[pointSize : pointSize+nonceSize]
	ciphertext := fullCipherBytes[pointSize+nonceSize:]

	var ephemeralPub twistededwards.PointAffine
	if _, err := ephemeralPub.SetBytes(ephemeralPubBytes); err != nil {
		return nil, fmt.Errorf("invalid ephemeral public key: %w", err)
	}

	var sharedPoint twistededwards.PointAffine
	sharedPoint.ScalarMultiplication(&ephemeralPub, myPrivKey)

	sharedBytes := sharedPoint.Bytes()
	sharedSecret := sha256.Sum256(sharedBytes[:])

	block, err := aes.NewCipher(sharedSecret[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed (wrong key or corrupted data)")
	}

	return plaintext, nil
}
