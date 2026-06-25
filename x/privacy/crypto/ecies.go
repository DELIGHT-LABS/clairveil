package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

const ViewTagLength = 2

var ErrViewTagMismatch = errors.New("view tag mismatch")

// AsymEncrypt encrypts msg to receiverPubKey and returns
// [ephemeral public key | nonce | ciphertext].
func AsymEncrypt(msg []byte, receiverPubKey twistededwards.PointAffine) ([]byte, error) {
	cipherText, _, err := asymEncrypt(msg, receiverPubKey)
	return cipherText, err
}

func AsymEncryptWithViewTag(msg []byte, receiverPubKey twistededwards.PointAffine, outputCommitment []byte, outputIndex uint32) ([]byte, []byte, error) {
	cipherText, sharedPoint, err := asymEncrypt(msg, receiverPubKey)
	if err != nil {
		return nil, nil, err
	}
	viewTag, err := DeriveViewTag(sharedPoint, outputCommitment, outputIndex)
	if err != nil {
		return nil, nil, err
	}
	return cipherText, viewTag, nil
}

func asymEncrypt(msg []byte, receiverPubKey twistededwards.PointAffine) ([]byte, twistededwards.PointAffine, error) {
	curve := twistededwards.GetEdwardsCurve()

	ephemeralPriv, err := rand.Int(rand.Reader, &curve.Order)
	if err != nil {
		return nil, twistededwards.PointAffine{}, err
	}

	var ephemeralPub twistededwards.PointAffine
	ephemeralPub.ScalarMultiplication(&curve.Base, ephemeralPriv)

	var sharedPoint twistededwards.PointAffine
	sharedPoint.ScalarMultiplication(&receiverPubKey, ephemeralPriv)

	sharedBytes := sharedPoint.Bytes()
	sharedSecret := sha256.Sum256(sharedBytes[:])

	block, err := aes.NewCipher(sharedSecret[:])
	if err != nil {
		return nil, twistededwards.PointAffine{}, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, twistededwards.PointAffine{}, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, twistededwards.PointAffine{}, err
	}

	ciphertext := gcm.Seal(nil, nonce, msg, nil)

	ephemeralPubBytes := ephemeralPub.Bytes()

	result := make([]byte, 0, len(ephemeralPubBytes)+len(nonce)+len(ciphertext))
	result = append(result, ephemeralPubBytes[:]...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, sharedPoint, nil
}

func DeriveViewTag(sharedPoint twistededwards.PointAffine, outputCommitment []byte, outputIndex uint32) ([]byte, error) {
	if len(outputCommitment) != 32 {
		return nil, fmt.Errorf("output commitment must be exactly 32 bytes")
	}

	viewTagFull := MimcHash(
		HashString("clairveil.view_tag.v1"),
		sharedPoint.X.BigInt(new(big.Int)),
		sharedPoint.Y.BigInt(new(big.Int)),
		new(big.Int).SetBytes(outputCommitment),
		new(big.Int).SetUint64(uint64(outputIndex)),
	)
	viewTagBytes := viewTagFull.FillBytes(make([]byte, 32))
	return append([]byte(nil), viewTagBytes[:ViewTagLength]...), nil
}

// AsymDecrypt decrypts a ciphertext produced by AsymEncrypt.
func AsymDecrypt(fullCipherBytes []byte, myPrivKey *big.Int) ([]byte, error) {
	ephemeralPub, nonce, ciphertext, err := splitAsymCiphertext(fullCipherBytes)
	if err != nil {
		return nil, err
	}
	return decryptAsymCiphertext(ephemeralPub, nonce, ciphertext, myPrivKey)
}

func AsymDecryptWithViewTag(fullCipherBytes []byte, myPrivKey *big.Int, outputCommitment []byte, outputIndex uint32, expectedViewTag []byte) ([]byte, error) {
	if len(expectedViewTag) != ViewTagLength {
		return nil, fmt.Errorf("expected view tag must be exactly %d bytes", ViewTagLength)
	}

	ephemeralPub, nonce, ciphertext, err := splitAsymCiphertext(fullCipherBytes)
	if err != nil {
		return nil, err
	}

	sharedPoint := deriveSharedPoint(ephemeralPub, myPrivKey)
	viewTag, err := DeriveViewTag(sharedPoint, outputCommitment, outputIndex)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(viewTag, expectedViewTag) != 1 {
		return nil, ErrViewTagMismatch
	}

	return decryptWithSharedPoint(sharedPoint, nonce, ciphertext)
}

func splitAsymCiphertext(fullCipherBytes []byte) (twistededwards.PointAffine, []byte, []byte, error) {
	pointSize := 32
	nonceSize := 12

	if len(fullCipherBytes) < pointSize+nonceSize {
		return twistededwards.PointAffine{}, nil, nil, errors.New("invalid ciphertext length")
	}

	ephemeralPubBytes := fullCipherBytes[:pointSize]
	nonce := fullCipherBytes[pointSize : pointSize+nonceSize]
	ciphertext := fullCipherBytes[pointSize+nonceSize:]

	var ephemeralPub twistededwards.PointAffine
	if _, err := ephemeralPub.SetBytes(ephemeralPubBytes); err != nil {
		return twistededwards.PointAffine{}, nil, nil, fmt.Errorf("invalid ephemeral public key: %w", err)
	}

	return ephemeralPub, nonce, ciphertext, nil
}

func decryptAsymCiphertext(ephemeralPub twistededwards.PointAffine, nonce []byte, ciphertext []byte, myPrivKey *big.Int) ([]byte, error) {
	sharedPoint := deriveSharedPoint(ephemeralPub, myPrivKey)
	return decryptWithSharedPoint(sharedPoint, nonce, ciphertext)
}

func deriveSharedPoint(ephemeralPub twistededwards.PointAffine, myPrivKey *big.Int) twistededwards.PointAffine {
	var sharedPoint twistededwards.PointAffine
	sharedPoint.ScalarMultiplication(&ephemeralPub, myPrivKey)
	return sharedPoint
}

func decryptWithSharedPoint(sharedPoint twistededwards.PointAffine, nonce []byte, ciphertext []byte) ([]byte, error) {
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
