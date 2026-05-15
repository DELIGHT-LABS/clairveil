package disclosure

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestVerifyPayloadForAmountOnlyUserDisclosure(t *testing.T) {
	commitment := big.NewInt(12345)
	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(commitment)
	require.NoError(t, err)

	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(commitment)
	require.NoError(t, err)

	assetID := privacycrypto.HashString("uclair")
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(assetID)
	require.NoError(t, err)

	digestHex, err := privacytypes.ComputeTransferDisclosureDigestHex(
		privacytypes.TransferPrivacyPolicyDiscloseAmount,
		privacytypes.TransferDisclosureRecipientOutputIndex,
		commitmentBytes,
		big.NewInt(7),
		assetID,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	payload := &Payload{
		Version:             PayloadVersion,
		Plane:               PlaneUser,
		Policy:              privacytypes.TransferPrivacyPolicyDiscloseAmount,
		OutputIndex:         privacytypes.TransferDisclosureRecipientOutputIndex,
		CommitmentHex:       commitmentHex,
		DisclosureDigestHex: digestHex,
		Amount:              "7",
		AssetIDHex:          assetIDHex,
		AssetDenom:          "uclair",
	}

	verification, err := VerifyPayload(payload, digestHex)
	require.NoError(t, err)
	require.True(t, verification.Verified)
	require.True(t, verification.AssetDenomVerified)
	require.True(t, verification.LocalDisclosureDigestMatch)
	require.True(t, verification.OnChainDisclosureDigestUsed)
	require.True(t, verification.OnChainDisclosureDigestMatch)
}

func TestDisclosureAmountAndAssetRejectsMismatchedDenom(t *testing.T) {
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(privacycrypto.HashString("uclair"))
	require.NoError(t, err)

	_, _, err = DisclosureAmountAndAsset(&Payload{
		Amount:     "7",
		AssetIDHex: assetIDHex,
		AssetDenom: "ulegacy",
	})
	require.ErrorContains(t, err, "does not match")
}
