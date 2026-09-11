package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const RootSeedLength = 32

const (
	RootSigningDomain = "clairveil-root-v1"
	SpendDomain       = "privacy-spend"
	ViewDomain        = "privacy-view"
	DisclosureDomain  = "privacy-disclosure"
)

func BuildRootSigningMessage(address string, transparentPubKey []byte) []byte {
	return []byte(fmt.Sprintf("%s\naddress:%s\npubkey:%s", RootSigningDomain, address, hex.EncodeToString(transparentPubKey)))
}

func ComputeRootSeed(address string, transparentPubKey, signature []byte) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/root\naddress:%s\npubkey:%s\nsignature:%s", RootSigningDomain, address, hex.EncodeToString(transparentPubKey), hex.EncodeToString(signature))))
	return sum[:]
}

func DeriveDomainSeed(rootSeed []byte, domain string) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/derive\ndomain:%s\nroot:%s", RootSigningDomain, domain, hex.EncodeToString(rootSeed))))
	return sum[:]
}

// DeriveScalarFromSeed retains seed mod q with zero mapped to one. It returns
// an opaque scalar and has no big.Int compatibility path.
func DeriveScalarFromSeed(seed []byte) (privacycrypto.SecretScalar, error) {
	if len(seed) != RootSeedLength {
		return privacycrypto.SecretScalar{}, fmt.Errorf("identity seed must be exactly %d bytes", RootSeedLength)
	}
	var fixed [RootSeedLength]byte
	copy(fixed[:], seed)
	return privacycrypto.DeriveIdentityScalarSeed32(fixed)
}

func DerivePubKeyFromScalar(scalar privacycrypto.SecretScalar) (*crypto_tedwards.PointAffine, error) {
	return privacycrypto.PublicKey(scalar)
}

func deriveKeys(rootSeed []byte, domain string) (privacycrypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	seed := DeriveDomainSeed(rootSeed, domain)
	scalar, err := DeriveScalarFromSeed(seed)
	if err != nil {
		return privacycrypto.SecretScalar{}, nil, nil, err
	}
	pub, err := DerivePubKeyFromScalar(scalar)
	if err != nil {
		return privacycrypto.SecretScalar{}, nil, nil, err
	}
	return scalar, pub, seed, nil
}

func DeriveSpendKeys(rootSeed []byte) (privacycrypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	return deriveKeys(rootSeed, SpendDomain)
}

func DeriveViewKeys(rootSeed []byte) (privacycrypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	return deriveKeys(rootSeed, ViewDomain)
}

func DeriveDisclosureKeys(rootSeed []byte) (privacycrypto.SecretScalar, *crypto_tedwards.PointAffine, []byte, error) {
	return deriveKeys(rootSeed, DisclosureDomain)
}

func ScalarToFixedHex(scalar privacycrypto.SecretScalar) string {
	bytes := scalar.Bytes()
	return hex.EncodeToString(bytes[:])
}

func DeriveShieldedAddress(rootSeed []byte) (string, error) {
	_, spendPubKey, _, err := DeriveSpendKeys(rootSeed)
	if err != nil {
		return "", err
	}
	_, viewPubKey, _, err := DeriveViewKeys(rootSeed)
	if err != nil {
		return "", err
	}
	return privacytypes.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
}
