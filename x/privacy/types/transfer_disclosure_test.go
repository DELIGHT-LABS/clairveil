package types

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeTransferDisclosureDigestBytes(t *testing.T) {
	commitment := validFieldBytes()
	amount := big.NewInt(10)
	assetID := big.NewInt(7)

	digest, err := ComputeTransferDisclosureDigestBytes(
		TransferPrivacyPolicyDiscloseAmountToFrom,
		TransferDisclosureRecipientOutputIndex,
		commitment,
		amount,
		assetID,
		big.NewInt(17),
		big.NewInt(19),
		big.NewInt(23),
		big.NewInt(29),
		big.NewInt(11),
		big.NewInt(13),
		big.NewInt(31),
		big.NewInt(37),
	)
	require.NoError(t, err)
	require.Len(t, digest, expectedFieldElementBytes)
	require.NoError(t, validateFieldElementBytesStrict("digest", digest))
}

func TestComputeTransferDisclosureDigestBytesZerosHiddenFields(t *testing.T) {
	commitment := validFieldBytes()

	digestAllPrivate, err := ComputeTransferDisclosureDigestHex(
		TransferPrivacyPolicyAllPrivate,
		TransferDisclosureRecipientOutputIndex,
		commitment,
		big.NewInt(10),
		big.NewInt(7),
		big.NewInt(17),
		big.NewInt(19),
		big.NewInt(23),
		big.NewInt(29),
		big.NewInt(11),
		big.NewInt(13),
		big.NewInt(31),
		big.NewInt(37),
	)
	require.NoError(t, err)

	digestWithDifferentSecrets, err := ComputeTransferDisclosureDigestHex(
		TransferPrivacyPolicyAllPrivate,
		TransferDisclosureRecipientOutputIndex,
		commitment,
		big.NewInt(999),
		big.NewInt(777),
		big.NewInt(123),
		big.NewInt(456),
		big.NewInt(789),
		big.NewInt(321),
		big.NewInt(654),
		big.NewInt(987),
		big.NewInt(111),
		big.NewInt(222),
	)
	require.NoError(t, err)

	require.Equal(t, digestAllPrivate, digestWithDifferentSecrets)
	_, err = hex.DecodeString(digestAllPrivate)
	require.NoError(t, err)
}

func TestComputeTransferDisclosureDigestBytesRequiresPolicyInputs(t *testing.T) {
	commitment := validFieldBytes()

	_, err := ComputeTransferDisclosureDigestBytes(
		TransferPrivacyPolicyDiscloseAmount,
		TransferDisclosureRecipientOutputIndex,
		commitment,
		nil,
		big.NewInt(7),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.Error(t, err)

	_, err = ComputeTransferDisclosureDigestBytes(
		TransferPrivacyPolicyDiscloseTo,
		TransferDisclosureRecipientOutputIndex,
		commitment,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.Error(t, err)
}

func TestComputeAuditTransferDisclosureDigestBytes(t *testing.T) {
	commitment := validFieldBytes()

	digest, err := ComputeAuditTransferDisclosureDigestBytes(
		TransferDisclosureRecipientOutputIndex,
		commitment,
		big.NewInt(10),
		big.NewInt(7),
		big.NewInt(17),
		big.NewInt(19),
		big.NewInt(23),
		big.NewInt(29),
		big.NewInt(11),
		big.NewInt(13),
		big.NewInt(31),
		big.NewInt(37),
	)
	require.NoError(t, err)
	require.Len(t, digest, expectedFieldElementBytes)
	require.NoError(t, validateFieldElementBytesStrict("audit digest", digest))
}

func TestComputeAuditTransferDisclosureDigestBytesRequiresFullAddresses(t *testing.T) {
	commitment := validFieldBytes()

	_, err := ComputeAuditTransferDisclosureDigestBytes(
		TransferDisclosureRecipientOutputIndex,
		commitment,
		big.NewInt(10),
		big.NewInt(7),
		nil,
		nil,
		nil,
		nil,
		big.NewInt(11),
		big.NewInt(13),
		big.NewInt(31),
		big.NewInt(37),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sender shielded address")
}
