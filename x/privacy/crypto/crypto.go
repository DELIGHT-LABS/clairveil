package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
	"io"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
)

// MimcHash is the variable-time public/witness compatibility hash. Private
// wallet preimages must use LegacyMiMCHash with fixed FieldValue inputs.
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

// HashToField is the public-only legacy MiMC adapter retained for consensus
// verification and differential tests. Secret wallet paths use
// LegacyMiMCHash(FieldValue...) instead.
func HashToField(data ...*big.Int) *big.Int { return MimcHash(data...) }

// Encrypt derives an AES-GCM key from the shared secret seed.
func Encrypt(plaintext []byte, secret []byte) (out []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = encryptSeed(plaintext, secret) })
	return out, err
}

func encryptSeed(plaintext []byte, secret []byte) ([]byte, error) {
	keyHash := sha256.Sum256(secret)
	defer clear(keyHash[:])

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

func Decrypt(ciphertext []byte, keySeed []byte) (out []byte, err error) {
	if err = secretprofile.Check(); err != nil {
		return nil, err
	}
	subtle.WithDataIndependentTiming(func() { out, err = decryptSeed(ciphertext, keySeed) })
	return out, err
}

func decryptSeed(ciphertext []byte, keySeed []byte) ([]byte, error) {
	key := sha256.Sum256(keySeed)
	defer clear(key[:])

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

// HashStringToField is the public-only spelling used by compatibility KATs.
func HashStringToField(s string) *big.Int { return HashString(s) }
