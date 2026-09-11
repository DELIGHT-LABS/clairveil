package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretmem"
	"io"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
)

const ViewTagLength = 2

const (
	asymNonceSize = 12
	asymTagSize   = 16
)

var ErrViewTagMismatch = errors.New("view tag mismatch")

// AsymEncrypt preserves the legacy wire [compressed ephemeral|nonce|AES-GCM]
// while performing ECDH in the fixed-schedule Edwards backend.
func AsymEncrypt(msg []byte, receiverPubKey twistededwards.PointAffine) (out []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() {
		var shared SecretPoint
		defer secretmem.Clear(&shared)
		out, shared, err = asymEncrypt(msg, receiverPubKey)
	})
	return out, err
}

func AsymEncryptWithViewTag(msg []byte, receiverPubKey twistededwards.PointAffine, outputCommitment []byte, outputIndex uint32) (out, tag []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, tag, err = asymEncryptWithViewTag(msg, receiverPubKey, outputCommitment, outputIndex) })
	return out, tag, err
}

func asymEncryptWithViewTag(msg []byte, receiverPubKey twistededwards.PointAffine, outputCommitment []byte, outputIndex uint32) ([]byte, []byte, error) {
	cipherText, sharedPoint, err := asymEncrypt(msg, receiverPubKey)
	defer secretmem.Clear(&sharedPoint)
	if err != nil {
		return nil, nil, err
	}
	viewTag, err := deriveViewTag(sharedPoint, outputCommitment, outputIndex)
	if err != nil {
		return nil, nil, err
	}
	return cipherText, viewTag, nil
}

func asymEncrypt(msg []byte, receiverPubKey twistededwards.PointAffine) ([]byte, SecretPoint, error) {
	if err := secretprofile.Check(); err != nil {
		return nil, SecretPoint{}, err
	}
	receiver, err := SecretPointFromPublic(receiverPubKey)
	if err != nil {
		return nil, SecretPoint{}, fmt.Errorf("invalid receiver public key: %w", err)
	}
	ephemeral, err := SampleSecretScalar(rand.Reader)
	defer secretmem.Clear(&ephemeral)
	if err != nil {
		return nil, SecretPoint{}, err
	}
	shared, err := sharedSecretPoint(receiver, ephemeral)
	defer secretmem.Clear(&shared)
	if err != nil {
		return nil, SecretPoint{}, err
	}
	ephemeralPub, err := publicPointForSecret(ephemeral)
	if err != nil {
		return nil, SecretPoint{}, err
	}
	ephemeralBytes, err := ephemeralPub.legacyCompressed()
	if err != nil {
		return nil, SecretPoint{}, err
	}
	ciphertext, err := encryptWithSharedPoint(shared, msg)
	if err != nil {
		return nil, SecretPoint{}, err
	}
	result := make([]byte, 0, len(ephemeralBytes)+len(ciphertext))
	result = append(result, ephemeralBytes[:]...)
	result = append(result, ciphertext...)
	return result, shared, nil
}

func publicPointForSecret(s SecretScalar) (SecretPoint, error) {
	n, err := s.scalar()
	if err != nil {
		return SecretPoint{}, err
	}
	base := secretBasePoint()
	var point SecretPoint
	subtle.WithDataIndependentTiming(func() { point.point.ScalarMultNonzero(&base.point, n) })
	point.valid = true
	return point, nil
}

func secretBasePoint() SecretPoint { return SecretPoint{point: baseEdwardsPoint(), valid: true} }

func deriveViewTag(sharedPoint SecretPoint, outputCommitment []byte, outputIndex uint32) ([]byte, error) {
	if len(outputCommitment) != 32 {
		return nil, fmt.Errorf("output commitment must be exactly 32 bytes")
	}
	commitment, err := ParseFieldValueBE32(outputCommitment)
	if err != nil {
		return nil, fmt.Errorf("output commitment must be canonical Fr: %w", err)
	}
	x, y, err := sharedPoint.affineFieldValues()
	if err != nil {
		return nil, err
	}
	domain := HashStringFieldValue("clairveil.view_tag.v1")
	hash, err := LegacyMiMCHash(domain, x, y, commitment, FieldValueFromUint64(uint64(outputIndex)))
	if err != nil {
		return nil, err
	}
	bytes := hash.Bytes()
	return append([]byte(nil), bytes[:ViewTagLength]...), nil
}

// AsymDecrypt accepts only the opaque typed private scalar. It never falls
// back to math/big and returns no plaintext after a tag failure.
func AsymDecrypt(fullCipherBytes []byte, myPrivKey SecretScalar) (out []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = asymDecrypt(fullCipherBytes, myPrivKey) })
	return out, err
}

func asymDecrypt(fullCipherBytes []byte, myPrivKey SecretScalar) ([]byte, error) {
	ephemeralPub, nonce, ciphertext, err := splitAsymCiphertext(fullCipherBytes)
	if err != nil {
		return nil, err
	}
	return decryptAsymCiphertext(ephemeralPub, nonce, ciphertext, myPrivKey)
}

func AsymDecryptWithViewTag(fullCipherBytes []byte, myPrivKey SecretScalar, outputCommitment []byte, outputIndex uint32, expectedViewTag []byte) (out []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() {
		out, err = asymDecryptWithViewTag(fullCipherBytes, myPrivKey, outputCommitment, outputIndex, expectedViewTag)
	})
	return out, err
}

func asymDecryptWithViewTag(fullCipherBytes []byte, myPrivKey SecretScalar, outputCommitment []byte, outputIndex uint32, expectedViewTag []byte) ([]byte, error) {
	if len(expectedViewTag) != ViewTagLength {
		return nil, fmt.Errorf("expected view tag must be exactly %d bytes", ViewTagLength)
	}
	ephemeralPub, nonce, ciphertext, err := splitAsymCiphertext(fullCipherBytes)
	if err != nil {
		return nil, err
	}
	sharedPoint, err := deriveSharedPoint(ephemeralPub, myPrivKey)
	defer secretmem.Clear(&sharedPoint)
	defer secretmem.Clear(&myPrivKey)
	if err != nil {
		return nil, err
	}
	viewTag, err := deriveViewTag(sharedPoint, outputCommitment, outputIndex)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(viewTag, expectedViewTag) != 1 {
		return nil, ErrViewTagMismatch
	}
	return decryptWithSharedPoint(sharedPoint, nonce, ciphertext)
}

func splitAsymCiphertext(fullCipherBytes []byte) (SecretPoint, []byte, []byte, error) {
	minimumSize := CanonicalPointSize + asymNonceSize + asymTagSize
	if len(fullCipherBytes) < minimumSize {
		return SecretPoint{}, nil, nil, fmt.Errorf("invalid ciphertext length: expected at least %d bytes, got %d", minimumSize, len(fullCipherBytes))
	}
	ephemeral, err := parseLegacySecretPoint(fullCipherBytes[:CanonicalPointSize])
	if err != nil {
		return SecretPoint{}, nil, nil, fmt.Errorf("invalid ephemeral public key: %w", err)
	}
	return ephemeral, fullCipherBytes[CanonicalPointSize : CanonicalPointSize+asymNonceSize], fullCipherBytes[CanonicalPointSize+asymNonceSize:], nil
}

func deriveSharedPoint(ephemeral SecretPoint, myPrivKey SecretScalar) (SecretPoint, error) {
	if !myPrivKey.IsValid() {
		return SecretPoint{}, errors.New("invalid ECIES private scalar")
	}
	return sharedSecretPoint(ephemeral, myPrivKey)
}

func encryptWithSharedPoint(sharedPoint SecretPoint, plaintext []byte) ([]byte, error) {
	sharedBytes, err := sharedPoint.legacyCompressed()
	if err != nil {
		return nil, err
	}
	defer clear(sharedBytes[:])
	key := sha256.Sum256(sharedBytes[:])
	defer clear(key[:])
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptAsymCiphertext(ephemeral SecretPoint, nonce, ciphertext []byte, myPrivKey SecretScalar) ([]byte, error) {
	shared, err := deriveSharedPoint(ephemeral, myPrivKey)
	defer secretmem.Clear(&shared)
	defer secretmem.Clear(&myPrivKey)
	if err != nil {
		return nil, err
	}
	return decryptWithSharedPoint(shared, nonce, ciphertext)
}

func decryptWithSharedPoint(sharedPoint SecretPoint, nonce, ciphertext []byte) ([]byte, error) {
	sharedBytes, err := sharedPoint.legacyCompressed()
	if err != nil {
		return nil, err
	}
	defer clear(sharedBytes[:])
	key := sha256.Sum256(sharedBytes[:])
	defer clear(key[:])
	block, err := aes.NewCipher(key[:])
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
