package transfer

import (
	"context"
	"fmt"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

func TestDecodeDisclosurePubKeyHexDecodesValidPoint(t *testing.T) {
	_, pubKey := testScalarAndPubKey(313)
	pubKeyBytes := pubKey.Bytes()

	decoded, decodedBytes, err := DecodeDisclosurePubKeyHex(fmt.Sprintf("%x", pubKeyBytes[:]))
	require.NoError(t, err)
	require.Equal(t, pubKey.Bytes(), decoded.Bytes())
	require.Equal(t, pubKeyBytes[:], decodedBytes)
}

func TestDecodeDisclosurePubKeyHexRejectsInvalidHex(t *testing.T) {
	_, _, err := DecodeDisclosurePubKeyHex("zz")
	require.ErrorContains(t, err, "invalid disclosure pubkey hex")
}

func TestDecodeDisclosurePubKeyHexRejectsIdentityAndNonSubgroupPoints(t *testing.T) {
	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	identityBytes := identity.Bytes()
	_, _, err := DecodeDisclosurePubKeyHex(fmt.Sprintf("%x", identityBytes[:]))
	require.ErrorContains(t, err, "identity point is not allowed")

	orderTwo := twistededwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	require.True(t, orderTwo.IsOnCurve())
	require.False(t, orderTwo.IsZero())
	orderTwoBytes := orderTwo.Bytes()
	_, _, err = DecodeDisclosurePubKeyHex(fmt.Sprintf("%x", orderTwoBytes[:]))
	require.ErrorContains(t, err, "point is not in the prime-order subgroup")
}

func TestResolveAuditDisclosureTargetUsesProvider(t *testing.T) {
	_, pubKey := testScalarAndPubKey(317)
	pubKeyBytes := pubKey.Bytes()
	provider := &stubAuditDisclosureTargetProvider{
		pubKeyHex: fmt.Sprintf("%x", pubKeyBytes[:]),
	}

	decoded, decodedBytes, err := ResolveAuditDisclosureTarget(context.Background(), provider)
	require.NoError(t, err)
	require.Equal(t, pubKey.Bytes(), decoded.Bytes())
	require.Equal(t, pubKeyBytes[:], decodedBytes)
}

func TestResolveAuditDisclosureTargetRejectsMissingConfig(t *testing.T) {
	_, _, err := ResolveAuditDisclosureTarget(context.Background(), &stubAuditDisclosureTargetProvider{})
	require.ErrorContains(t, err, "chain audit master pubkey is not configured")
}

func TestResolveAuditDisclosureTargetWrapsProviderError(t *testing.T) {
	_, _, err := ResolveAuditDisclosureTarget(context.Background(), &stubAuditDisclosureTargetProvider{
		err: fmt.Errorf("boom"),
	})
	require.ErrorContains(t, err, "failed to query the audit disclosure config")
	require.ErrorContains(t, err, "boom")
}

type stubAuditDisclosureTargetProvider struct {
	pubKeyHex string
	err       error
}

func (s *stubAuditDisclosureTargetProvider) AuditMasterPubkeyHex(_ context.Context) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.pubKeyHex, nil
}
