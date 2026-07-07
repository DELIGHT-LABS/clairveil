package payroll

import (
	"encoding/hex"
	"fmt"
	"strings"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const disclosurePubKeyHexLength = 64

func ValidateDisclosurePolicy(policy PayrollDisclosurePolicy) error {
	policy = normalizeDisclosurePolicy(policy)
	if err := validateUserPrivacyPolicy(policy.UserPrivacyPolicy); err != nil {
		return err
	}
	switch policy.UserPrivacyPolicy {
	case privacytypes.TransferPrivacyPolicyAllPrivate:
		if policy.UserDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE {
			return fmt.Errorf("%w: all-private disclosure policy must use mode none", ErrInvalidPayrollInput)
		}
		if policy.UserDisclosureTargetPubKeyHex != "" || policy.UserDisclosureTargetKeyID != "" {
			return fmt.Errorf("%w: all-private disclosure policy must not include a user disclosure target key", ErrInvalidPayrollInput)
		}
	default:
		switch policy.UserDisclosureMode {
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
			if policy.UserDisclosureTargetPubKeyHex != "" {
				return fmt.Errorf("%w: public user disclosure must not include a target pubkey", ErrInvalidPayrollInput)
			}
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
			if err := validateDisclosurePubKeyHex(policy.UserDisclosureTargetPubKeyHex); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: non-private user disclosure requires public or recipient-encrypted mode", ErrInvalidPayrollInput)
		}
	}
	if err := validateOptionalDigestHex(policy.ExpectedUserDisclosureDigest, "expected_user_disclosure_digest"); err != nil {
		return err
	}
	if err := validateOptionalDigestHex(policy.ExpectedAuditDisclosureDigest, "expected_audit_disclosure_digest"); err != nil {
		return err
	}
	if err := validateOptionalDigestHex(policy.ExpectedSelfViewDisclosureDigest, "expected_self_view_disclosure_digest"); err != nil {
		return err
	}
	return nil
}

func validateUserPrivacyPolicy(policy uint32) error {
	switch policy {
	case privacytypes.TransferPrivacyPolicyAllPrivate,
		privacytypes.TransferPrivacyPolicyDiscloseAmount,
		privacytypes.TransferPrivacyPolicyDiscloseTo,
		privacytypes.TransferPrivacyPolicyDiscloseAmountTo,
		privacytypes.TransferPrivacyPolicyDiscloseFrom,
		privacytypes.TransferPrivacyPolicyDiscloseAmountFrom,
		privacytypes.TransferPrivacyPolicyDiscloseToFrom,
		privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom:
		return nil
	default:
		return fmt.Errorf("%w: unsupported user privacy policy %d", ErrInvalidPayrollInput, policy)
	}
}

func validateDisclosurePubKeyHex(pubKeyHex string) error {
	if strings.TrimSpace(pubKeyHex) == "" {
		return fmt.Errorf("%w: recipient-encrypted user disclosure requires a target pubkey", ErrInvalidPayrollInput)
	}
	if len(pubKeyHex) != disclosurePubKeyHexLength {
		return fmt.Errorf("%w: user disclosure target pubkey must be %d hex chars", ErrInvalidPayrollInput, disclosurePubKeyHexLength)
	}
	if _, err := hex.DecodeString(pubKeyHex); err != nil {
		return fmt.Errorf("%w: user disclosure target pubkey must be hex: %v", ErrInvalidPayrollInput, err)
	}
	return nil
}

func validateOptionalDigestHex(value string, name string) error {
	if value == "" {
		return nil
	}
	if _, err := privacyfield.DecodeCanonicalHex(value, name); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPayrollInput, err)
	}
	return nil
}

func normalizeDisclosurePolicy(policy PayrollDisclosurePolicy) PayrollDisclosurePolicy {
	policy.UserDisclosureTargetPubKeyHex = strings.ToLower(strings.TrimSpace(policy.UserDisclosureTargetPubKeyHex))
	policy.UserDisclosureTargetKeyID = strings.TrimSpace(policy.UserDisclosureTargetKeyID)
	policy.ExpectedUserDisclosureDigest = strings.ToLower(strings.TrimSpace(policy.ExpectedUserDisclosureDigest))
	policy.ExpectedAuditDisclosureDigest = strings.ToLower(strings.TrimSpace(policy.ExpectedAuditDisclosureDigest))
	policy.ExpectedSelfViewDisclosureDigest = strings.ToLower(strings.TrimSpace(policy.ExpectedSelfViewDisclosureDigest))
	return policy
}

func effectiveDisclosurePolicy(defaultPolicy PayrollDisclosurePolicy, itemPolicy PayrollDisclosurePolicy) PayrollDisclosurePolicy {
	if isZeroDisclosurePolicy(itemPolicy) {
		return normalizeDisclosurePolicy(defaultPolicy)
	}
	return normalizeDisclosurePolicy(itemPolicy)
}

func isZeroDisclosurePolicy(policy PayrollDisclosurePolicy) bool {
	policy = normalizeDisclosurePolicy(policy)
	return policy.UserPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate &&
		policy.UserDisclosureMode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE &&
		policy.UserDisclosureTargetPubKeyHex == "" &&
		policy.UserDisclosureTargetKeyID == "" &&
		policy.ExpectedUserDisclosureDigest == "" &&
		policy.ExpectedAuditDisclosureDigest == "" &&
		policy.ExpectedSelfViewDisclosureDigest == ""
}
