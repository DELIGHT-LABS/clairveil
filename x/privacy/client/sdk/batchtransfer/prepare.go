package batchtransfer

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func PrepareBatchTransfer(ctx context.Context, provider MerklePathProvider, plan *BatchTransferPlan) (*PreparedBatchTransfer, error) {
	if provider == nil {
		return nil, fmt.Errorf("a merkle path provider is required")
	}
	if plan == nil {
		return nil, fmt.Errorf("a batch transfer plan is required")
	}
	if err := validateBatchTransferPlanForPreparation(plan); err != nil {
		return nil, err
	}
	prepared := &PreparedBatchTransfer{Inputs: make([]PreparedBatchTransferInput, len(plan.Inputs)), Outputs: make([]PreparedBatchTransferOutput, len(plan.Outputs))}
	for i, input := range plan.Inputs {
		commitment, err := privacyfield.CanonicalBytesFromBigInt(input.Note.ComputeCommitment())
		if err != nil {
			return nil, err
		}
		path, err := provider.LookupMerklePath(ctx, hex.EncodeToString(commitment))
		if err != nil {
			return nil, fmt.Errorf("input %d merkle path lookup failed: %w", i, err)
		}
		if path == nil || len(path.Path) != 32 || len(path.Path) != len(path.PathHelper) {
			return nil, fmt.Errorf("input %d merkle path is malformed", i)
		}
		if err := privacyfield.ValidateCanonicalBytes32(path.Root); err != nil {
			return nil, fmt.Errorf("input %d root: %w", i, err)
		}
		if prepared.Root == nil {
			prepared.Root = append([]byte(nil), path.Root...)
		} else if !bytes.Equal(prepared.Root, path.Root) {
			return nil, ErrWalletSyncRequired
		}
		for _, bit := range path.PathHelper {
			if bit > 1 {
				return nil, fmt.Errorf("input %d merkle helper must contain only bits", i)
			}
		}
		nullifier, err := privacyfield.CanonicalBytesFromBigInt(input.Note.ComputeNullifier())
		if err != nil {
			return nil, err
		}
		prepared.Inputs[i] = PreparedBatchTransferInput{input.Note, append([]string(nil), path.Path...), append([]uint32(nil), path.PathHelper...), nullifier}
		if prepared.AssetID == nil {
			prepared.AssetID = new(big.Int).Set(input.Note.AssetID)
		}
	}
	nullifiers := make([][]byte, len(prepared.Inputs))
	for i := range prepared.Inputs {
		nullifiers[i] = prepared.Inputs[i].Nullifier
	}
	if err := privacytypes.ValidateDistinctCanonicalFieldElements("input nullifier", nullifiers); err != nil {
		return nil, err
	}

	commitments := make([][]byte, len(plan.Outputs))
	usedSecrets := make(map[string]struct{}, len(plan.Outputs)*3+len(plan.Inputs))
	for _, input := range plan.Inputs {
		usedSecrets[input.Note.Randomness.String()] = struct{}{}
	}
	for i, output := range plan.Outputs {
		xs, ys := pointCoordinates(output.SpendPubKey)
		xv, yv := pointCoordinates(output.ViewPubKey)
		r, err := freshSecret(usedSecrets)
		if err != nil {
			return nil, err
		}
		note := privacytypes.Note{ReceiverSpendPubKeyX: xs, ReceiverSpendPubKeyY: ys, ReceiverViewPubKeyX: xv, ReceiverViewPubKeyY: yv, Amount: new(big.Int).Set(output.Amount), AssetID: new(big.Int).Set(prepared.AssetID), Randomness: r, Memo: string(output.Kind)}
		if err := note.ValidateV1(); err != nil {
			return nil, fmt.Errorf("output %d NoteV1: %w", i, err)
		}
		commitments[i], err = privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
		if err != nil {
			return nil, err
		}
		full, err := freshSecret(usedSecrets)
		if err != nil {
			return nil, err
		}
		user := big.NewInt(0)
		if output.PrivacyPolicy != 0 {
			user, err = freshSecret(usedSecrets)
			if err != nil {
				return nil, err
			}
		}
		target := []byte(nil)
		if output.DisclosureTargetPubKey != nil {
			b := output.DisclosureTargetPubKey.Bytes()
			target = append(target, b[:]...)
		}
		prepared.Outputs[i] = PreparedBatchTransferOutput{output.Kind, note, output.PrivacyPolicy, output.DisclosureMode, target, user, full}
	}
	if err := privacytypes.ValidateDistinctCanonicalFieldElements("output commitment", commitments); err != nil {
		return nil, err
	}
	return prepared, nil
}

func validateBatchTransferPlanForPreparation(plan *BatchTransferPlan) error {
	if err := privacytypes.ValidateBatchJoinSplitCountsV1(uint32(len(plan.Inputs)), uint32(len(plan.Outputs))); err != nil {
		return err
	}
	if plan.InputTotal == nil || plan.PaymentTotal == nil || plan.Change == nil {
		return fmt.Errorf("batch transfer plan totals are required")
	}

	inputTotal := new(big.Int)
	seenNullifiers := make(map[string]struct{}, len(plan.Inputs))
	for i := range plan.Inputs {
		note := plan.Inputs[i].Note
		if err := note.ValidateV1(); err != nil {
			return fmt.Errorf("invalid input NoteV1 %d: %w", i, err)
		}
		if i > 0 && !sameOwner(plan.Inputs[0].Note, note) {
			return fmt.Errorf("input %d does not belong to the common owner", i)
		}
		if i > 0 && plan.Inputs[0].Note.AssetID.Cmp(note.AssetID) != 0 {
			return fmt.Errorf("input asset mismatch at index %d", i)
		}
		nullifier := note.ComputeNullifier().String()
		if _, duplicate := seenNullifiers[nullifier]; duplicate {
			return fmt.Errorf("duplicate input nullifier at index %d", i)
		}
		seenNullifiers[nullifier] = struct{}{}
		inputTotal.Add(inputTotal, note.Amount)
	}

	ownerSpend := pointBytesFromNote(plan.Inputs[0].Note, true)
	ownerView := pointBytesFromNote(plan.Inputs[0].Note, false)
	paymentTotal := new(big.Int)
	outputTotal := new(big.Int)
	changeTotal := new(big.Int)
	seenPayment, seenChange, seenPadding := false, false, false
	for i := range plan.Outputs {
		output := plan.Outputs[i]
		if output.SpendPubKey == nil || output.ViewPubKey == nil {
			return fmt.Errorf("output %d recipient keys are required", i)
		}
		if err := privacycrypto.ValidatePrimeSubgroupPoint(output.SpendPubKey); err != nil {
			return fmt.Errorf("output %d spend key: %w", i, err)
		}
		if err := privacycrypto.ValidatePrimeSubgroupPoint(output.ViewPubKey); err != nil {
			return fmt.Errorf("output %d view key: %w", i, err)
		}
		if output.Amount == nil {
			return fmt.Errorf("output %d amount is required", i)
		}
		if err := privacytypes.ValidateShieldedAmount(fmt.Sprintf("output %d amount", i), output.Amount); err != nil {
			return err
		}
		if output.PrivacyPolicy > privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
			return fmt.Errorf("output %d has unsupported disclosure policy", i)
		}
		switch output.DisclosureMode {
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
			if output.PrivacyPolicy != 0 || output.DisclosureTargetPubKey != nil {
				return fmt.Errorf("output %d all-private disclosure is not canonical", i)
			}
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
			if output.PrivacyPolicy == 0 || output.DisclosureTargetPubKey != nil {
				return fmt.Errorf("output %d public disclosure is not canonical", i)
			}
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
			if output.PrivacyPolicy == 0 || output.DisclosureTargetPubKey == nil {
				return fmt.Errorf("output %d recipient-encrypted disclosure is not canonical", i)
			}
			if err := privacycrypto.ValidatePrimeSubgroupPoint(output.DisclosureTargetPubKey); err != nil {
				return fmt.Errorf("output %d disclosure target: %w", i, err)
			}
		default:
			return fmt.Errorf("output %d has unsupported disclosure mode", i)
		}

		spendBytes := output.SpendPubKey.Bytes()
		viewBytes := output.ViewPubKey.Bytes()
		matchesOwner := bytes.Equal(spendBytes[:], ownerSpend) && bytes.Equal(viewBytes[:], ownerView)
		switch output.Kind {
		case OutputPayment:
			if seenChange || seenPadding || output.Amount.Sign() <= 0 {
				return fmt.Errorf("payment outputs must be a positive canonical prefix")
			}
			seenPayment = true
			paymentTotal.Add(paymentTotal, output.Amount)
		case OutputChange:
			if seenChange || seenPadding || output.Amount.Sign() <= 0 || output.PrivacyPolicy != 0 || !matchesOwner {
				return fmt.Errorf("change output %d is not canonical", i)
			}
			seenChange = true
			changeTotal.Set(output.Amount)
		case OutputPadding:
			if output.Amount.Sign() != 0 || output.PrivacyPolicy != 0 || !matchesOwner {
				return fmt.Errorf("padding output %d is not canonical", i)
			}
			seenPadding = true
		default:
			return fmt.Errorf("unsupported planned output kind %q", output.Kind)
		}
		outputTotal.Add(outputTotal, output.Amount)
	}
	if !seenPayment {
		return fmt.Errorf("batch transfer plan requires at least one payment output")
	}
	expectedChange := new(big.Int).Sub(inputTotal, paymentTotal)
	if expectedChange.Sign() < 0 || outputTotal.Cmp(inputTotal) != 0 {
		return fmt.Errorf("batch transfer plan input/output conservation mismatch")
	}
	if plan.InputTotal.Cmp(inputTotal) != 0 || plan.PaymentTotal.Cmp(paymentTotal) != 0 || plan.Change.Cmp(expectedChange) != 0 || changeTotal.Cmp(expectedChange) != 0 || seenChange != (expectedChange.Sign() > 0) {
		return fmt.Errorf("batch transfer plan totals are inconsistent")
	}
	return nil
}

func freshSecret(used map[string]struct{}) (*big.Int, error) {
	for {
		value, err := privacycrypto.GenerateNonZeroRandomness()
		if err != nil {
			return nil, err
		}
		key := value.String()
		if _, exists := used[key]; exists {
			continue
		}
		used[key] = struct{}{}
		return value, nil
	}
}

func pointCoordinates(p *crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x, y := new(big.Int), new(big.Int)
	p.X.BigInt(x)
	p.Y.BigInt(y)
	return x, y
}

func pointFromCoordinates(x, y *big.Int) (*crypto_tedwards.PointAffine, error) {
	if x == nil || y == nil {
		return nil, fmt.Errorf("point coordinates are required")
	}
	var p crypto_tedwards.PointAffine
	p.X.SetBigInt(x)
	p.Y.SetBigInt(y)
	if err := privacycrypto.ValidatePrimeSubgroupPoint(&p); err != nil {
		return nil, err
	}
	return &p, nil
}
