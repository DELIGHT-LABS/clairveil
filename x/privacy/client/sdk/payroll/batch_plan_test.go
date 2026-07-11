package payroll

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBatchPayrollPlannerBoundaryShapes(t *testing.T) {
	cases := []struct {
		name          string
		inputAmounts  []int64
		paymentCount  int
		paymentAmount int64
		wantInputs    int
		wantOutputs   int
		wantChange    int64
	}{
		{name: "one_input_one_payment", inputAmounts: []int64{5}, paymentCount: 1, paymentAmount: 5, wantInputs: 1, wantOutputs: 1},
		{name: "three_inputs_four_payments", inputAmounts: []int64{4, 4, 4}, paymentCount: 4, paymentAmount: 3, wantInputs: 3, wantOutputs: 4},
		{name: "thirty_one_payments_plus_change", inputAmounts: []int64{32}, paymentCount: 31, paymentAmount: 1, wantInputs: 1, wantOutputs: 32, wantChange: 1},
		{name: "exact_thirty_two_payments", inputAmounts: []int64{32}, paymentCount: 32, paymentAmount: 1, wantInputs: 1, wantOutputs: 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := batchPlannerInput(tc.paymentCount, tc.paymentAmount)
			notes := make([]TreasuryNote, len(tc.inputAmounts))
			for i, amount := range tc.inputAmounts {
				notes[i] = testTreasuryNote(fmt.Sprintf("batch-note-%02d", i), input.Denom, amount, false, "")
			}
			plan, err := (BatchPayrollPlanner{}).Plan(input, notes)
			require.NoError(t, err)
			require.Len(t, plan.Operations, 1)
			operation := plan.Operations[0]
			require.Len(t, operation.InputNotes, tc.wantInputs)
			require.Len(t, operation.Items, tc.paymentCount)
			require.Equal(t, tc.wantOutputs, operation.OutputCount)
			require.Equal(t, tc.wantChange, operation.Change.Int64())
			require.Equal(t, tc.wantChange > 0, operation.HasChange)
		})
	}
}

func TestBatchPayrollPlannerPreservesMixedDisclosurePerOutput(t *testing.T) {
	input := batchPlannerInput(4, 1)
	targetBundle, err := privacytypes.DecodeShieldedAddressBundle(testRecipientAddress("1"))
	require.NoError(t, err)
	targetBytes := targetBundle.ViewPubKey.Bytes()
	input.DefaultDisclosurePolicy = PayrollDisclosurePolicy{
		UserPrivacyPolicy:  privacytypes.TransferPrivacyPolicyAllPrivate,
		UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
	}
	input.Items[0].DisclosurePolicySet = true
	input.Items[0].DisclosurePolicy = PayrollDisclosurePolicy{
		UserPrivacyPolicy:             privacytypes.TransferPrivacyPolicyDiscloseAmount,
		UserDisclosureMode:            privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
		UserDisclosureTargetPubKeyHex: hex.EncodeToString(targetBytes[:]),
	}
	input.Items[2].DisclosurePolicySet = true
	input.Items[2].DisclosurePolicy = PayrollDisclosurePolicy{
		UserPrivacyPolicy:  privacytypes.TransferPrivacyPolicyDiscloseAmountTo,
		UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC,
	}
	plan, err := (BatchPayrollPlanner{}).Plan(input, []TreasuryNote{testTreasuryNote("mixed-note", input.Denom, 4, false, "")})
	require.NoError(t, err)
	require.Len(t, plan.Operations, 1)
	items := plan.Operations[0].Items
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseAmount, items[0].DisclosurePolicy.UserPrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, items[0].DisclosurePolicy.UserDisclosureMode)
	require.Equal(t, privacytypes.TransferPrivacyPolicyAllPrivate, items[1].DisclosurePolicy.UserPrivacyPolicy)
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseAmountTo, items[2].DisclosurePolicy.UserPrivacyPolicy)
	require.Equal(t, privacytypes.TransferPrivacyPolicyAllPrivate, items[3].DisclosurePolicy.UserPrivacyPolicy)
}

func TestBatchPayrollPlannerRequiresExplicitPreparationBeyondSixteenInputs(t *testing.T) {
	input := batchPlannerInput(1, 17)
	notes := make([]TreasuryNote, 17)
	for i := range notes {
		notes[i] = testTreasuryNote(fmt.Sprintf("small-%02d", i), input.Denom, 1, false, "")
	}
	_, err := (BatchPayrollPlanner{}).Plan(input, notes)
	require.Error(t, err)
	require.ErrorIs(t, err, privacybatchtransfer.ErrPreparationRequired)
}

func TestBatchPayrollPlannerBacktracksAcrossOwnersForRemainingPayments(t *testing.T) {
	input := batchPlannerInput(33, 1)
	input.Items[30].Amount = big.NewInt(30)
	input.Items[31].Amount = big.NewInt(50)
	input.Items[32].Amount = big.NewInt(50)
	ownerA := testTreasuryNote("owner-a-100", input.Denom, 100, false, "")
	ownerA.OwnerKeyID = "owner-a"
	ownerB := testTreasuryNote("owner-b-60", input.Denom, 60, false, "")
	ownerB.OwnerKeyID = "owner-b"
	notes := []TreasuryNote{ownerA, ownerB}

	plan, err := (BatchPayrollPlanner{}).Plan(input, notes)
	require.NoError(t, err)
	require.Len(t, plan.Operations, 2)
	require.Len(t, plan.Operations[0].Items, 31)
	require.Equal(t, "owner-b", plan.Operations[0].InputNotes[0].OwnerKeyID)
	require.Equal(t, int64(60), plan.Operations[0].PaymentTotal.Int64())
	require.Len(t, plan.Operations[1].Items, 2)
	require.Equal(t, "owner-a", plan.Operations[1].InputNotes[0].OwnerKeyID)
	require.Equal(t, int64(100), plan.Operations[1].PaymentTotal.Int64())
}

func TestBatchPayrollPlannerBacktracksWithinOwnerToPreserveFutureNote(t *testing.T) {
	input := batchPlannerInput(33, 1)
	input.Items[30].Amount = big.NewInt(70)
	input.Items[31].Amount = big.NewInt(20)
	input.Items[32].Amount = big.NewInt(20)
	notes := []TreasuryNote{
		testTreasuryNote("owner-70", input.Denom, 70, false, ""),
		testTreasuryNote("owner-40", input.Denom, 40, false, ""),
		testTreasuryNote("owner-30", input.Denom, 30, false, ""),
	}

	plan, err := (BatchPayrollPlanner{}).Plan(input, notes)
	require.NoError(t, err)
	require.Len(t, plan.Operations, 2)
	require.Equal(t, []string{"owner-30", "owner-70"}, []string{plan.Operations[0].InputNotes[0].NoteID, plan.Operations[0].InputNotes[1].NoteID})
	require.Equal(t, int64(100), plan.Operations[0].InputTotal.Int64())
	require.Equal(t, "owner-40", plan.Operations[1].InputNotes[0].NoteID)
}

func batchPlannerInput(paymentCount int, paymentAmount int64) PayrollInput {
	input := PayrollInput{CompanyID: "company-batch", PayrollID: "payroll-batch", BatchID: "run-batch", Denom: "uclair", Attempt: 1}
	input.Items = make([]PayrollItemInput, paymentCount)
	for i := range input.Items {
		input.Items[i] = PayrollItemInput{
			ItemID: fmt.Sprintf("item-%02d", i), EmployeeID: fmt.Sprintf("employee-%02d", i),
			RecipientAddress: testRecipientAddress("1"), Amount: big.NewInt(paymentAmount), Denom: input.Denom,
		}
	}
	return input
}
