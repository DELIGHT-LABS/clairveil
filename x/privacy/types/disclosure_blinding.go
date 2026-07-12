package types

import (
	"fmt"
	"math/big"
)

// DisclosureBlindingErrorCodeV1 is the stable, secret-free error contract for
// disclosure blinding separation. Callers may add context by wrapping the
// returned error, but must preserve the code and must not include field values.
type DisclosureBlindingErrorCodeV1 string

const (
	DisclosureBlindingErrorInvalidPolicyV1          DisclosureBlindingErrorCodeV1 = "DBS_INVALID_POLICY"
	DisclosureBlindingErrorNonCanonicalFieldV1      DisclosureBlindingErrorCodeV1 = "DBS_NON_CANONICAL_FIELD"
	DisclosureBlindingErrorDisabledSentinelV1       DisclosureBlindingErrorCodeV1 = "DBS_DISABLED_SENTINEL"
	DisclosureBlindingErrorAllPrivateUserSentinelV1 DisclosureBlindingErrorCodeV1 = "DBS_ALL_PRIVATE_USER_SENTINEL"
	DisclosureBlindingErrorUserBlindingRequiredV1   DisclosureBlindingErrorCodeV1 = "DBS_USER_BLINDING_REQUIRED"
	DisclosureBlindingErrorFullBlindingRequiredV1   DisclosureBlindingErrorCodeV1 = "DBS_FULL_BLINDING_REQUIRED"
	DisclosureBlindingErrorUserRandomnessReuseV1    DisclosureBlindingErrorCodeV1 = "DBS_USER_RANDOMNESS_REUSE"
	DisclosureBlindingErrorFullRandomnessReuseV1    DisclosureBlindingErrorCodeV1 = "DBS_FULL_RANDOMNESS_REUSE"
	DisclosureBlindingErrorUserFullBlindingReuseV1  DisclosureBlindingErrorCodeV1 = "DBS_USER_FULL_BLINDING_REUSE"
)

// DisclosureBlindingErrorV1 identifies a failed invariant without echoing any
// note randomness or disclosure blinding. Code, OutputIndex, and Field are the
// normative machine-readable contract; the diagnostic text is informational.
type DisclosureBlindingErrorV1 struct {
	Code        DisclosureBlindingErrorCodeV1
	OutputIndex uint32
	Field       string
	diagnostic  string
}

func (e *DisclosureBlindingErrorV1) Error() string {
	return fmt.Sprintf("%s: output %d %s", e.Code, e.OutputIndex, e.diagnostic)
}

// DisclosureBlindingSeparationV1Input models one capacity output slot. For an
// active JoinSplit2x2 disclosure this is output 0. For BatchJoinSplit16x32 it
// is evaluated independently for every output slot.
type DisclosureBlindingSeparationV1Input struct {
	OutputIndex            uint32
	Enabled                bool
	PrivacyPolicy          uint32
	OutputRandomness       *big.Int
	UserDisclosureBlinding *big.Int
	FullDisclosureBlinding *big.Int
}

// ValidateDisclosureBlindingSeparationV1 freezes the shared native meaning of
// DISCLOSURE-BLINDING-SEPARATION.
//
// Active non-all-private outputs require three exact inequalities:
//
//	user blinding != output randomness
//	full blinding != output randomness
//	full blinding != user blinding
//
// Active all-private outputs canonicalize the user blinding to zero and gate
// off only the user-vs-randomness inequality. Full blinding remains non-zero
// and distinct from both output randomness and the zero user sentinel.
// Disabled capacity slots canonicalize policy, randomness, and both blindings
// to zero and apply no inequality.
func ValidateDisclosureBlindingSeparationV1(input DisclosureBlindingSeparationV1Input) error {
	if input.PrivacyPolicy > TransferPrivacyPolicyDiscloseAmountToFrom {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorInvalidPolicyV1,
			input.OutputIndex,
			"privacy_policy",
			"privacy policy is outside the canonical range 0..7",
		)
	}

	for _, field := range []struct {
		name  string
		value *big.Int
	}{
		{"output_randomness", input.OutputRandomness},
		{"user_disclosure_blinding", input.UserDisclosureBlinding},
		{"full_disclosure_blinding", input.FullDisclosureBlinding},
	} {
		if err := validateCanonicalNoteField(field.name, field.value); err != nil {
			return newDisclosureBlindingErrorV1(
				DisclosureBlindingErrorNonCanonicalFieldV1,
				input.OutputIndex,
				field.name,
				field.name+" must be a canonical BN254 field element",
			)
		}
	}

	if !input.Enabled {
		for _, sentinel := range []struct {
			field   string
			nonZero bool
		}{
			{"privacy_policy", input.PrivacyPolicy != TransferPrivacyPolicyAllPrivate},
			{"output_randomness", input.OutputRandomness.Sign() != 0},
			{"user_disclosure_blinding", input.UserDisclosureBlinding.Sign() != 0},
			{"full_disclosure_blinding", input.FullDisclosureBlinding.Sign() != 0},
		} {
			if sentinel.nonZero {
				return newDisclosureBlindingErrorV1(
					DisclosureBlindingErrorDisabledSentinelV1,
					input.OutputIndex,
					sentinel.field,
					"disabled slot "+sentinel.field+" must use the zero sentinel",
				)
			}
		}
		return nil
	}

	userEnabled := input.PrivacyPolicy != TransferPrivacyPolicyAllPrivate
	if !userEnabled && input.UserDisclosureBlinding.Sign() != 0 {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorAllPrivateUserSentinelV1,
			input.OutputIndex,
			"user_disclosure_blinding",
			"all-private user disclosure blinding must use the zero sentinel",
		)
	}
	if userEnabled && input.UserDisclosureBlinding.Sign() == 0 {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorUserBlindingRequiredV1,
			input.OutputIndex,
			"user_disclosure_blinding",
			"enabled user disclosure blinding must be non-zero",
		)
	}
	if input.FullDisclosureBlinding.Sign() == 0 {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorFullBlindingRequiredV1,
			input.OutputIndex,
			"full_disclosure_blinding",
			"active full disclosure blinding must be non-zero",
		)
	}
	if userEnabled && input.UserDisclosureBlinding.Cmp(input.OutputRandomness) == 0 {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorUserRandomnessReuseV1,
			input.OutputIndex,
			"user_disclosure_blinding",
			"user disclosure blinding must differ from output randomness",
		)
	}
	if input.FullDisclosureBlinding.Cmp(input.OutputRandomness) == 0 {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorFullRandomnessReuseV1,
			input.OutputIndex,
			"full_disclosure_blinding",
			"full disclosure blinding must differ from output randomness",
		)
	}
	if input.FullDisclosureBlinding.Cmp(input.UserDisclosureBlinding) == 0 {
		return newDisclosureBlindingErrorV1(
			DisclosureBlindingErrorUserFullBlindingReuseV1,
			input.OutputIndex,
			"full_disclosure_blinding",
			"full disclosure blinding must differ from user disclosure blinding",
		)
	}
	return nil
}

func newDisclosureBlindingErrorV1(
	code DisclosureBlindingErrorCodeV1,
	outputIndex uint32,
	field string,
	diagnostic string,
) error {
	return &DisclosureBlindingErrorV1{
		Code:        code,
		OutputIndex: outputIndex,
		Field:       field,
		diagnostic:  diagnostic,
	}
}
