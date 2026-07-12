package types

import (
	"errors"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestValidateDisclosureBlindingSeparationV1(t *testing.T) {
	validDisclosed := DisclosureBlindingSeparationV1Input{
		OutputIndex:            3,
		Enabled:                true,
		PrivacyPolicy:          TransferPrivacyPolicyDiscloseAmount,
		OutputRandomness:       big.NewInt(11),
		UserDisclosureBlinding: big.NewInt(13),
		FullDisclosureBlinding: big.NewInt(17),
	}
	validAllPrivate := DisclosureBlindingSeparationV1Input{
		OutputIndex:            4,
		Enabled:                true,
		PrivacyPolicy:          TransferPrivacyPolicyAllPrivate,
		OutputRandomness:       big.NewInt(0),
		UserDisclosureBlinding: big.NewInt(0),
		FullDisclosureBlinding: big.NewInt(17),
	}
	validDisabled := DisclosureBlindingSeparationV1Input{
		OutputIndex:            5,
		Enabled:                false,
		PrivacyPolicy:          TransferPrivacyPolicyAllPrivate,
		OutputRandomness:       big.NewInt(0),
		UserDisclosureBlinding: big.NewInt(0),
		FullDisclosureBlinding: big.NewInt(0),
	}

	require.NoError(t, ValidateDisclosureBlindingSeparationV1(validDisclosed))
	require.NoError(t, ValidateDisclosureBlindingSeparationV1(validAllPrivate))
	require.NoError(t, ValidateDisclosureBlindingSeparationV1(validDisabled))

	tests := []struct {
		name  string
		input DisclosureBlindingSeparationV1Input
		code  DisclosureBlindingErrorCodeV1
		field string
	}{
		{
			name: "invalid policy", input: withDisclosurePolicy(validDisclosed, 8),
			code: DisclosureBlindingErrorInvalidPolicyV1, field: "privacy_policy",
		},
		{
			name: "nil randomness", input: withDisclosureRandomness(validDisclosed, nil),
			code: DisclosureBlindingErrorNonCanonicalFieldV1, field: "output_randomness",
		},
		{
			name: "field modulus", input: withDisclosureFullBlinding(validDisclosed, new(big.Int).Set(fr.Modulus())),
			code: DisclosureBlindingErrorNonCanonicalFieldV1, field: "full_disclosure_blinding",
		},
		{
			name: "disabled policy", input: withDisclosurePolicy(validDisabled, TransferPrivacyPolicyDiscloseAmount),
			code: DisclosureBlindingErrorDisabledSentinelV1, field: "privacy_policy",
		},
		{
			name: "disabled randomness", input: withDisclosureRandomness(validDisabled, big.NewInt(1)),
			code: DisclosureBlindingErrorDisabledSentinelV1, field: "output_randomness",
		},
		{
			name: "disabled user blinding", input: withDisclosureUserBlinding(validDisabled, big.NewInt(1)),
			code: DisclosureBlindingErrorDisabledSentinelV1, field: "user_disclosure_blinding",
		},
		{
			name: "disabled full blinding", input: withDisclosureFullBlinding(validDisabled, big.NewInt(1)),
			code: DisclosureBlindingErrorDisabledSentinelV1, field: "full_disclosure_blinding",
		},
		{
			name: "all-private user sentinel", input: withDisclosureUserBlinding(validAllPrivate, big.NewInt(1)),
			code: DisclosureBlindingErrorAllPrivateUserSentinelV1, field: "user_disclosure_blinding",
		},
		{
			name: "enabled user missing", input: withDisclosureUserBlinding(validDisclosed, big.NewInt(0)),
			code: DisclosureBlindingErrorUserBlindingRequiredV1, field: "user_disclosure_blinding",
		},
		{
			name: "active full missing", input: withDisclosureFullBlinding(validDisclosed, big.NewInt(0)),
			code: DisclosureBlindingErrorFullBlindingRequiredV1, field: "full_disclosure_blinding",
		},
		{
			name: "user randomness reuse", input: withDisclosureUserBlinding(validDisclosed, big.NewInt(11)),
			code: DisclosureBlindingErrorUserRandomnessReuseV1, field: "user_disclosure_blinding",
		},
		{
			name: "full randomness reuse", input: withDisclosureFullBlinding(validDisclosed, big.NewInt(11)),
			code: DisclosureBlindingErrorFullRandomnessReuseV1, field: "full_disclosure_blinding",
		},
		{
			name: "user full reuse", input: withDisclosureFullBlinding(validDisclosed, big.NewInt(13)),
			code: DisclosureBlindingErrorUserFullBlindingReuseV1, field: "full_disclosure_blinding",
		},
		{
			name: "all-private full randomness reuse", input: withDisclosureFullBlinding(withDisclosureRandomness(validAllPrivate, big.NewInt(11)), big.NewInt(11)),
			code: DisclosureBlindingErrorFullRandomnessReuseV1, field: "full_disclosure_blinding",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDisclosureBlindingSeparationV1(tc.input)
			require.Error(t, err)
			var invariantErr *DisclosureBlindingErrorV1
			require.True(t, errors.As(err, &invariantErr))
			require.Equal(t, tc.code, invariantErr.Code)
			require.Equal(t, tc.input.OutputIndex, invariantErr.OutputIndex)
			require.Equal(t, tc.field, invariantErr.Field)
			for _, secret := range []*big.Int{tc.input.OutputRandomness, tc.input.UserDisclosureBlinding, tc.input.FullDisclosureBlinding} {
				if secret != nil {
					require.NotContains(t, err.Error(), secret.String())
				}
			}
		})
	}
}

func withDisclosurePolicy(input DisclosureBlindingSeparationV1Input, policy uint32) DisclosureBlindingSeparationV1Input {
	input.PrivacyPolicy = policy
	return input
}

func withDisclosureRandomness(input DisclosureBlindingSeparationV1Input, value *big.Int) DisclosureBlindingSeparationV1Input {
	input.OutputRandomness = value
	return input
}

func withDisclosureUserBlinding(input DisclosureBlindingSeparationV1Input, value *big.Int) DisclosureBlindingSeparationV1Input {
	input.UserDisclosureBlinding = value
	return input
}

func withDisclosureFullBlinding(input DisclosureBlindingSeparationV1Input, value *big.Int) DisclosureBlindingSeparationV1Input {
	input.FullDisclosureBlinding = value
	return input
}
