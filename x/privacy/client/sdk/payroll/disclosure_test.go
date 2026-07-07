package payroll

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestValidateInputRejectsEncryptedDisclosureWithoutPubkey(t *testing.T) {
	input := testPayrollInput()
	input.DefaultDisclosurePolicy = PayrollDisclosurePolicy{
		UserPrivacyPolicy:  privacytypes.TransferPrivacyPolicyDiscloseAmount,
		UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
	}

	err := ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayrollInput), "expected invalid input, got %v", err)
	require.Contains(t, err.Error(), "target pubkey")
}

func TestServiceCreatePlanCarriesDefaultDisclosurePolicy(t *testing.T) {
	ctx := context.Background()
	input := testPayrollInput()
	input.DefaultDisclosurePolicy = PayrollDisclosurePolicy{
		UserPrivacyPolicy:             privacytypes.TransferPrivacyPolicyDiscloseAmount,
		UserDisclosureMode:            privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC,
		ExpectedUserDisclosureDigest:  strings.Repeat("0", 63) + "1",
		ExpectedAuditDisclosureDigest: strings.Repeat("0", 63) + "2",
	}
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	}

	svc := Service{}
	plan, err := svc.CreatePlan(ctx, input, notes)
	require.NoError(t, err)
	require.Len(t, plan.Items, 1)
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseAmount, plan.Items[0].DisclosurePolicy.UserPrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC, plan.Items[0].DisclosurePolicy.UserDisclosureMode)
	require.Equal(t, strings.Repeat("0", 63)+"1", plan.Items[0].ExpectedDisclosureDigest)
}

func TestMemoryDisclosureKeyRegistryLookup(t *testing.T) {
	pubKey := strings.Repeat("a", 64)
	registry, err := NewMemoryDisclosureKeyRegistry([]DisclosureKeyEntry{{
		KeyID:        "employee-1-key-v1",
		Scope:        DisclosureKeyScopeEmployee,
		SubjectID:    "employee-1",
		PublicKeyHex: pubKey,
		Version:      "v1",
		Active:       true,
	}})
	require.NoError(t, err)

	entry, err := registry.LookupDisclosureKey(context.Background(), DisclosureKeyScopeEmployee, "employee-1")
	require.NoError(t, err)
	require.Equal(t, "employee-1-key-v1", entry.KeyID)
	require.Equal(t, pubKey, entry.PublicKeyHex)
}
