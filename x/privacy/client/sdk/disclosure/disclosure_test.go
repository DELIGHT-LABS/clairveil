package disclosure

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestVerifyPayloadForAmountOnlyUserDisclosure(t *testing.T) {
	commitment := big.NewInt(12345)
	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(commitment)
	require.NoError(t, err)

	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(commitment)
	require.NoError(t, err)

	assetID := privacytypes.ComputeAssetIDV1("uclair")
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(assetID)
	require.NoError(t, err)
	blinding := big.NewInt(77)
	blindingHex, err := privacyfield.CanonicalHexFromBigInt(blinding)
	require.NoError(t, err)

	digestHex, err := privacytypes.ComputeTransferDisclosureDigestHex(
		privacytypes.TransferPrivacyPolicyDiscloseAmount,
		privacytypes.TransferDisclosureRecipientOutputIndex,
		commitmentBytes,
		big.NewInt(7),
		assetID,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
		blinding,
	)
	require.NoError(t, err)

	payload := &Payload{
		Version:             PayloadVersion,
		Plane:               PlaneUser,
		Policy:              privacytypes.TransferPrivacyPolicyDiscloseAmount,
		OutputIndex:         privacytypes.TransferDisclosureRecipientOutputIndex,
		CommitmentHex:       commitmentHex,
		DisclosureDigestHex: digestHex,
		BlindingHex:         blindingHex,
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
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(privacytypes.ComputeAssetIDV1("uclair"))
	require.NoError(t, err)

	_, _, err = DisclosureAmountAndAsset(&Payload{
		Amount:     "7",
		AssetIDHex: assetIDHex,
		AssetDenom: "ulegacy",
	})
	require.ErrorContains(t, err, "does not match")
}

func TestVerifyPayloadRejectsFieldOutsideUserPolicy(t *testing.T) {
	payload, digestHex := amountOnlyPayload(t)
	payload.ToShieldedAddress = "not-authenticated-by-the-amount-only-digest"

	_, err := VerifyPayload(payload, digestHex)
	require.ErrorContains(t, err, "does not authenticate to_shielded_address")
}

func TestVerifyPayloadRejectsNonCanonicalAmount(t *testing.T) {
	payload, digestHex := amountOnlyPayload(t)
	payload.Amount = "07"

	_, err := VerifyPayload(payload, digestHex)
	require.ErrorContains(t, err, "canonical non-negative decimal string")
}

func TestComputeExpectedDisclosureDigestRejectsInvalidPlanePolicy(t *testing.T) {
	payload, _ := amountOnlyPayload(t)
	payload.Plane = PlaneAudit

	_, _, err := ComputeExpectedDisclosureDigest(payload)
	require.ErrorContains(t, err, "must use the full disclosure policy")
}

func TestComputeExpectedDisclosureDigestRejectsMissingPlane(t *testing.T) {
	payload, _ := amountOnlyPayload(t)
	payload.Plane = ""

	_, _, err := ComputeExpectedDisclosureDigest(payload)
	require.ErrorContains(t, err, "unsupported disclosure payload plane")
}

func amountOnlyPayload(t *testing.T) (*Payload, string) {
	t.Helper()

	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(12345))
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(big.NewInt(12345))
	require.NoError(t, err)
	assetID := privacytypes.ComputeAssetIDV1("uclair")
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(assetID)
	require.NoError(t, err)
	blinding := big.NewInt(77)
	blindingHex, err := privacyfield.CanonicalHexFromBigInt(blinding)
	require.NoError(t, err)
	digestHex, err := privacytypes.ComputeTransferDisclosureDigestHex(
		privacytypes.TransferPrivacyPolicyDiscloseAmount,
		privacytypes.TransferDisclosureRecipientOutputIndex,
		commitmentBytes,
		big.NewInt(7),
		assetID,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
		blinding,
	)
	require.NoError(t, err)

	return &Payload{
		Version:             PayloadVersion,
		Plane:               PlaneUser,
		Policy:              privacytypes.TransferPrivacyPolicyDiscloseAmount,
		OutputIndex:         privacytypes.TransferDisclosureRecipientOutputIndex,
		CommitmentHex:       commitmentHex,
		DisclosureDigestHex: digestHex,
		BlindingHex:         blindingHex,
		Amount:              "7",
		AssetIDHex:          assetIDHex,
		AssetDenom:          "uclair",
	}, digestHex
}
