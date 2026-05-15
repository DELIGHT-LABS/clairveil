package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
)

// MimcHash returns a BN254-field-compatible MiMC hash.
func MimcHash(data ...*big.Int) *big.Int {
	f := mimc.NewMiMC()
	var bFr fr.Element

	for _, d := range data {
		if d == nil {
			d = big.NewInt(0)
		}
		bFr.SetBigInt(d)
		b := bFr.Bytes()
		f.Write(b[:])
	}
	hashBytes := f.Sum(nil)
	return new(big.Int).SetBytes(hashBytes)
}

// Encrypt derives an AES-GCM key from the shared secret seed.
func Encrypt(plaintext []byte, secret []byte) ([]byte, error) {
	keyHash := sha256.Sum256(secret)

	block, err := aes.NewCipher(keyHash[:])
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

func Decrypt(ciphertext []byte, keySeed []byte) ([]byte, error) {
	key := sha256.Sum256(keySeed)

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, io.ErrUnexpectedEOF
	}

	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// HashString maps a string into a canonical BN254 field element.
func HashString(s string) *big.Int {
	h := sha256.Sum256([]byte(s))

	var elem fr.Element
	elem.SetBytes(h[:])

	return elem.BigInt(new(big.Int))
}
