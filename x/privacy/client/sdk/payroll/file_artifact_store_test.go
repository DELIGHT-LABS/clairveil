package payroll

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileArtifactStoreWritesAndReadsPayrollPlan(t *testing.T) {
	ctx := context.Background()
	store := FileArtifactStore{Dir: t.TempDir()}
	plan := PayrollPlan{
		CompanyID: "company-a",
		PayrollID: "payroll-a",
		BatchID:   "batch-a",
		Denom:     "uclair",
		Status:    PlanStatusDraft,
		CreatedAt: testNow(),
		UpdatedAt: testNow(),
		Items: []PayrollPlanItem{{
			ItemID:           "item-1",
			OperationID:      "operation-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(70),
			Denom:            "uclair",
			Status:           ItemStatusPlanned,
		}},
	}

	path, err := store.WritePayrollPlan(ctx, "company-a.payroll-a.batch-a", plan)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	loaded, err := store.ReadPayrollPlan(ctx, "company-a.payroll-a.batch-a")
	require.NoError(t, err)
	require.Equal(t, plan.CompanyID, loaded.CompanyID)
	require.Equal(t, plan.Items[0].Amount.String(), loaded.Items[0].Amount.String())
}

func TestFileArtifactStoreLoadsDisclosureKeyRegistry(t *testing.T) {
	ctx := context.Background()
	store := FileArtifactStore{Dir: t.TempDir()}
	entries := []DisclosureKeyEntry{{
		KeyID:        "employee-1-v1",
		Scope:        DisclosureKeyScopeEmployee,
		SubjectID:    "employee-1",
		PublicKeyHex: strings.Repeat("a", disclosurePubKeyHexLength),
		Version:      "v1",
		Active:       true,
	}}

	_, err := store.WriteDisclosureKeyEntries(ctx, "keys", entries)
	require.NoError(t, err)

	registry, err := store.ReadDisclosureKeyRegistry(ctx, "keys")
	require.NoError(t, err)
	entry, err := registry.LookupDisclosureKey(ctx, DisclosureKeyScopeEmployee, "employee-1")
	require.NoError(t, err)
	require.Equal(t, "employee-1-v1", entry.KeyID)
	require.Equal(t, strings.Repeat("a", disclosurePubKeyHexLength), entry.PublicKeyHex)
}

func TestFileArtifactStoreRejectsUnsafeArtifactID(t *testing.T) {
	store := FileArtifactStore{Dir: t.TempDir()}
	_, err := store.WritePlanReport(context.Background(), "../report", PlanReport{})
	require.ErrorIs(t, err, ErrInvalidPayrollInput)
}
