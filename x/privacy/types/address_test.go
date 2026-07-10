package types

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdkbech32 "github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/stretchr/testify/require"
)

func TestShieldedAddressWithViewRoundTrip(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	var spendPubKey twistededwards.PointAffine
	var viewPubKey twistededwards.PointAffine
	spendPubKey.ScalarMultiplication(&curve.Base, big.NewInt(3))
	viewPubKey.ScalarMultiplication(&curve.Base, big.NewInt(7))

	addr, err := EncodeShieldedAddressWithView(&spendPubKey, &viewPubKey)
	require.NoError(t, err)

	bundle, err := DecodeShieldedAddressBundle(addr)
	require.NoError(t, err)
	require.Equal(t, spendPubKey.Bytes(), bundle.SpendPubKey.Bytes())
	require.Equal(t, viewPubKey.Bytes(), bundle.ViewPubKey.Bytes())
}

func TestShieldedAddressInvalidPrefix(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	var spendPubKey twistededwards.PointAffine
	var viewPubKey twistededwards.PointAffine
	spendPubKey.ScalarMultiplication(&curve.Base, big.NewInt(5))
	viewPubKey.ScalarMultiplication(&curve.Base, big.NewInt(9))

	spendBytes := spendPubKey.Bytes()
	viewBytes := viewPubKey.Bytes()
	payload := append(spendBytes[:], viewBytes[:]...)

	wrongAddr, err := sdkbech32.ConvertAndEncode("wrong", payload)
	require.NoError(t, err)

	_, err = DecodeShieldedAddressBundle(wrongAddr)
	require.EqualError(t, err, fmt.Sprintf("invalid prefix: expected %s, got %s", ShieldedBech32Prefix, "wrong"))
}

func TestShieldedAddressInvalidLength(t *testing.T) {
	shortBytes := make([]byte, 31)
	addr, err := sdkbech32.ConvertAndEncode(ShieldedBech32Prefix, shortBytes)
	require.NoError(t, err)

	_, err = DecodeShieldedAddressBundle(addr)
	require.EqualError(t, err, "invalid decoded length: expected 64 bytes, got 31")
}

func TestShieldedAddressRejectsIdentityPublicKey(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	valid := curve.Base

	_, err := EncodeShieldedAddressWithView(&identity, &valid)
	require.ErrorContains(t, err, "identity point is not allowed")

	identityBytes := identity.Bytes()
	validBytes := valid.Bytes()
	payload := append(append([]byte(nil), identityBytes[:]...), validBytes[:]...)
	address, err := sdkbech32.ConvertAndEncode(ShieldedBech32Prefix, payload)
	require.NoError(t, err)

	_, err = DecodeShieldedAddressBundle(address)
	require.ErrorContains(t, err, "identity point is not allowed")
}

func TestShieldedAddressRejectsNonSubgroupPublicKey(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	orderTwo := twistededwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	require.True(t, orderTwo.IsOnCurve())
	require.False(t, orderTwo.IsZero())
	valid := curve.Base

	_, err := EncodeShieldedAddressWithView(&valid, &orderTwo)
	require.ErrorContains(t, err, "point is not in the prime-order subgroup")

	validBytes := valid.Bytes()
	orderTwoBytes := orderTwo.Bytes()
	payload := append(append([]byte(nil), validBytes[:]...), orderTwoBytes[:]...)
	address, err := sdkbech32.ConvertAndEncode(ShieldedBech32Prefix, payload)
	require.NoError(t, err)

	_, err = DecodeShieldedAddressBundle(address)
	require.ErrorContains(t, err, "point is not in the prime-order subgroup")
}
