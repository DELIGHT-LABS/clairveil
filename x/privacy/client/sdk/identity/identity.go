package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

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
	return []byte(fmt.Sprintf(
		"%s\naddress:%s\npubkey:%s",
		RootSigningDomain,
		address,
		hex.EncodeToString(transparentPubKey),
	))
}

func ComputeRootSeed(address string, transparentPubKey, signature []byte) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s/root\naddress:%s\npubkey:%s\nsignature:%s",
		RootSigningDomain,
		address,
		hex.EncodeToString(transparentPubKey),
		hex.EncodeToString(signature),
	)))
	return sum[:]
}

func DeriveDomainSeed(rootSeed []byte, domain string) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s/derive\ndomain:%s\nroot:%s",
		RootSigningDomain,
		domain,
		hex.EncodeToString(rootSeed),
	)))
	return sum[:]
}

func DeriveScalarFromSeed(seed []byte) *big.Int {
	scalar := new(big.Int).SetBytes(seed)
	curve := crypto_tedwards.GetEdwardsCurve()
	scalar.Mod(scalar, &curve.Order)
	if scalar.Sign() == 0 {
		scalar.SetInt64(1)
	}
	return scalar
}

func DerivePubKeyFromScalar(scalar *big.Int) *crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()

	var g crypto_tedwards.PointAffine
	g.X.Set(&curve.Base.X)
	g.Y.Set(&curve.Base.Y)

	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&g, scalar)

	return &pubKey
}

func DeriveSpendKeys(rootSeed []byte) (*big.Int, *crypto_tedwards.PointAffine, []byte) {
	spendSeed := DeriveDomainSeed(rootSeed, SpendDomain)
	spendScalar := DeriveScalarFromSeed(spendSeed)
	spendPubKey := DerivePubKeyFromScalar(spendScalar)
	return spendScalar, spendPubKey, spendSeed
}

func DeriveViewKeys(rootSeed []byte) (*big.Int, *crypto_tedwards.PointAffine, []byte) {
	viewSeed := DeriveDomainSeed(rootSeed, ViewDomain)
	viewScalar := DeriveScalarFromSeed(viewSeed)
	viewPubKey := DerivePubKeyFromScalar(viewScalar)
	return viewScalar, viewPubKey, viewSeed
}

func DeriveDisclosureKeys(rootSeed []byte) (*big.Int, *crypto_tedwards.PointAffine, []byte) {
	disclosureSeed := DeriveDomainSeed(rootSeed, DisclosureDomain)
	disclosureScalar := DeriveScalarFromSeed(disclosureSeed)
	disclosurePubKey := DerivePubKeyFromScalar(disclosureScalar)
	return disclosureScalar, disclosurePubKey, disclosureSeed
}

func ScalarToFixedHex(scalar *big.Int) string {
	bz := scalar.Bytes()
	fixed := make([]byte, 32)
	copy(fixed[32-len(bz):], bz)
	return hex.EncodeToString(fixed)
}

func DeriveShieldedAddress(rootSeed []byte) (string, error) {
	_, spendPubKey, _ := DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := DeriveViewKeys(rootSeed)
	return privacytypes.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
}
