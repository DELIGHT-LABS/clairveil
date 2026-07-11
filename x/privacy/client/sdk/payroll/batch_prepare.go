package payroll

import (
	"context"
	"encoding/hex"
	"fmt"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type BatchInputNoteSource interface {
	LoadBatchInputNote(ctx context.Context, noteID string) (privacytypes.Note, error)
}

func BuildPayrollBatchTransferPlan(ctx context.Context, operation BatchPayrollOperationPlan, source BatchInputNoteSource, ownerSpend, ownerView *crypto_tedwards.PointAffine, mode privacybatchtransfer.OutputMode) (*privacybatchtransfer.BatchTransferPlan, error) {
	if source == nil || ownerSpend == nil || ownerView == nil {
		return nil, fmt.Errorf("batch note source and owner keys are required")
	}
	inputs := make([]privacybatchtransfer.InputNote, len(operation.InputNotes))
	for i, input := range operation.InputNotes {
		note, err := source.LoadBatchInputNote(ctx, input.NoteID)
		if err != nil {
			return nil, fmt.Errorf("load batch input note %s: %w", input.NoteID, err)
		}
		inputs[i] = privacybatchtransfer.InputNote{Note: note}
	}
	payments := make([]privacybatchtransfer.Payment, len(operation.Items))
	for i, item := range operation.Items {
		bundle, err := privacytypes.DecodeShieldedAddressBundle(item.RecipientAddress)
		if err != nil {
			return nil, fmt.Errorf("decode payroll recipient %s: %w", item.ItemID, err)
		}
		var target *crypto_tedwards.PointAffine
		if item.DisclosurePolicy.UserDisclosureTargetPubKeyHex != "" {
			targetBytes, err := hex.DecodeString(item.DisclosurePolicy.UserDisclosureTargetPubKeyHex)
			if err != nil {
				return nil, fmt.Errorf("decode item %s disclosure target: %w", item.ItemID, err)
			}
			target, err = privacycrypto.DecodeCanonicalPoint(targetBytes)
			if err != nil {
				return nil, fmt.Errorf("decode item %s disclosure target: %w", item.ItemID, err)
			}
		}
		payments[i] = privacybatchtransfer.Payment{
			SpendPubKey: bundle.SpendPubKey, ViewPubKey: bundle.ViewPubKey, Amount: cloneBigInt(item.Amount),
			PrivacyPolicy: item.DisclosurePolicy.UserPrivacyPolicy, DisclosureMode: item.DisclosurePolicy.UserDisclosureMode,
			DisclosureTargetPubKey: target,
		}
	}
	plan, err := privacybatchtransfer.PlanBatchTransfer(privacybatchtransfer.PlanBatchTransferInput{
		Inputs: inputs, Payments: payments, OwnerSpendPubKey: ownerSpend, OwnerViewPubKey: ownerView, Mode: mode,
	})
	if err != nil {
		return nil, err
	}
	expectedOutputCount := operation.OutputCount
	if mode == privacybatchtransfer.OutputModeExact32 {
		expectedOutputCount = int(privacytypes.BatchJoinSplitV1MaxOutputs)
	}
	if len(plan.Outputs) != expectedOutputCount || plan.Change.Cmp(operation.Change) != 0 {
		return nil, fmt.Errorf("batch SDK plan differs from durable payroll plan")
	}
	return plan, nil
}

func PreparePayrollBatchTransfer(ctx context.Context, operation BatchPayrollOperationPlan, source BatchInputNoteSource, paths privacybatchtransfer.MerklePathProvider, ownerSpend, ownerView *crypto_tedwards.PointAffine, mode privacybatchtransfer.OutputMode) (*privacybatchtransfer.PreparedBatchTransfer, error) {
	plan, err := BuildPayrollBatchTransferPlan(ctx, operation, source, ownerSpend, ownerView, mode)
	if err != nil {
		return nil, err
	}
	return privacybatchtransfer.PrepareBatchTransfer(ctx, paths, plan)
}
