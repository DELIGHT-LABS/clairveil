package types

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdkbech32 "github.com/cosmos/cosmos-sdk/types/bech32"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
)

// ShieldedBech32Prefix is the human-readable prefix for full shielded addresses.
const ShieldedBech32Prefix = clairveiltypes.ShieldedBech32Prefix
const shieldedPubKeyByteLength = 32
const shieldedAddressWithViewPayloadLength = shieldedPubKeyByteLength * 2

type ShieldedAddressBundle struct {
	SpendPubKey *twistededwards.PointAffine
	ViewPubKey  *twistededwards.PointAffine
}

func EncodeShieldedAddressWithView(spendPubKey, viewPubKey *twistededwards.PointAffine) (string, error) {
	spendBytes := spendPubKey.Bytes()
	viewBytes := viewPubKey.Bytes()

	payload := make([]byte, 0, shieldedAddressWithViewPayloadLength)
	payload = append(payload, spendBytes[:]...)
	payload = append(payload, viewBytes[:]...)

	return sdkbech32.ConvertAndEncode(ShieldedBech32Prefix, payload)
}

func DecodeShieldedAddressBundle(address string) (*ShieldedAddressBundle, error) {
	hrp, decodedBytes, err := sdkbech32.DecodeAndConvert(address)
	if err != nil {
		return nil, err
	}

	if hrp != ShieldedBech32Prefix {
		return nil, fmt.Errorf("invalid prefix: expected %s, got %s", ShieldedBech32Prefix, hrp)
	}

	if len(decodedBytes) != shieldedAddressWithViewPayloadLength {
		return nil, fmt.Errorf("invalid decoded length: expected %d bytes, got %d", shieldedAddressWithViewPayloadLength, len(decodedBytes))
	}

	spendPubKey, err := decodePublicKey(decodedBytes[:shieldedPubKeyByteLength])
	if err != nil {
		return nil, err
	}

	viewPubKey, err := decodePublicKey(decodedBytes[shieldedPubKeyByteLength:])
	if err != nil {
		return nil, err
	}

	return &ShieldedAddressBundle{
		SpendPubKey: spendPubKey,
		ViewPubKey:  viewPubKey,
	}, nil
}

func decodePublicKey(bz []byte) (*twistededwards.PointAffine, error) {
	var pubKey twistededwards.PointAffine
	_, err := pubKey.SetBytes(bz)
	if err != nil {
		return nil, fmt.Errorf("invalid public key bytes: %w", err)
	}

	return &pubKey, nil
}
