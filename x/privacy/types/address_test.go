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
