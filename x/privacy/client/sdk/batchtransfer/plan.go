package batchtransfer

import (
	"bytes"
	"fmt"
	"math/big"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func PlanBatchTransfer(input PlanBatchTransferInput) (*BatchTransferPlan, error) {
	if input.Mode != OutputModeCompact && input.Mode != OutputModeExact32 {
		return nil, fmt.Errorf("output mode must be compact or exact32")
	}
	if input.PaddingCount < 0 {
		return nil, fmt.Errorf("padding count must not be negative")
	}
	if len(input.Inputs) == 0 {
		return nil, fmt.Errorf("input count must be in 1..16")
	}
	if len(input.Inputs) > int(privacytypes.BatchJoinSplitV1MaxInputs) {
		return nil, ErrPreparationRequired
	}
	if len(input.Payments) == 0 || len(input.Payments) > int(privacytypes.BatchJoinSplitV1MaxOutputs) {
		return nil, fmt.Errorf("payment count must be in 1..32")
	}
	if input.OwnerSpendPubKey == nil || input.OwnerViewPubKey == nil {
		return nil, fmt.Errorf("owner spend/view public keys are required")
	}
	if err := privacycrypto.ValidatePrimeSubgroupPoint(input.OwnerSpendPubKey); err != nil {
		return nil, err
	}
	if err := privacycrypto.ValidatePrimeSubgroupPoint(input.OwnerViewPubKey); err != nil {
		return nil, err
	}

	inputTotal, paymentTotal := new(big.Int), new(big.Int)
	seen := make(map[string]struct{}, len(input.Inputs))
	var asset *big.Int
	ownerSpend, ownerView := input.OwnerSpendPubKey.Bytes(), input.OwnerViewPubKey.Bytes()
	for i := range input.Inputs {
		note := input.Inputs[i].Note
		if err := note.ValidateV1(); err != nil {
			return nil, fmt.Errorf("invalid input NoteV1 %d: %w", i, err)
		}
		spend, view := pointBytesFromNote(note, true), pointBytesFromNote(note, false)
		if !bytes.Equal(spend, ownerSpend[:]) || !bytes.Equal(view, ownerView[:]) {
			return nil, fmt.Errorf("input %d does not belong to the common owner", i)
		}
		if asset == nil {
			asset = new(big.Int).Set(note.AssetID)
		} else if asset.Cmp(note.AssetID) != 0 {
			return nil, fmt.Errorf("input asset mismatch at index %d", i)
		}
		nullifier := note.ComputeNullifier().String()
		if _, ok := seen[nullifier]; ok {
			return nil, fmt.Errorf("duplicate input nullifier at index %d", i)
		}
		seen[nullifier] = struct{}{}
		inputTotal.Add(inputTotal, note.Amount)
	}

	outputs := make([]PlannedOutput, 0, 32)
	for i, payment := range input.Payments {
		if payment.Amount == nil || payment.Amount.Sign() <= 0 {
			return nil, fmt.Errorf("payment %d amount must be positive", i)
		}
		if err := privacytypes.ValidateShieldedAmount("payment amount", payment.Amount); err != nil {
			return nil, err
		}
		if payment.SpendPubKey == nil || payment.ViewPubKey == nil {
			return nil, fmt.Errorf("payment %d recipient keys are required", i)
		}
		if err := privacycrypto.ValidatePrimeSubgroupPoint(payment.SpendPubKey); err != nil {
			return nil, fmt.Errorf("payment %d spend key: %w", i, err)
		}
		if err := privacycrypto.ValidatePrimeSubgroupPoint(payment.ViewPubKey); err != nil {
			return nil, fmt.Errorf("payment %d view key: %w", i, err)
		}
		if payment.PrivacyPolicy > privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
			return nil, fmt.Errorf("payment %d has unsupported disclosure policy", i)
		}
		if payment.PrivacyPolicy == 0 && payment.DisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE {
			return nil, fmt.Errorf("all-private payment %d must use NONE disclosure mode", i)
		}
		if payment.PrivacyPolicy != 0 && payment.DisclosureMode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE {
			return nil, fmt.Errorf("disclosed payment %d must select public or recipient-encrypted mode", i)
		}
		switch payment.DisclosureMode {
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
			if payment.DisclosureTargetPubKey != nil {
				return nil, fmt.Errorf("all-private payment %d must not include a disclosure target", i)
			}
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
			if payment.DisclosureTargetPubKey != nil {
				return nil, fmt.Errorf("public payment %d must not include a disclosure target", i)
			}
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
			if payment.DisclosureTargetPubKey == nil {
				return nil, fmt.Errorf("recipient-encrypted payment %d requires a disclosure target", i)
			}
			if err := privacycrypto.ValidatePrimeSubgroupPoint(payment.DisclosureTargetPubKey); err != nil {
				return nil, fmt.Errorf("payment %d disclosure target: %w", i, err)
			}
		default:
			return nil, fmt.Errorf("payment %d has unsupported disclosure mode", i)
		}
		paymentTotal.Add(paymentTotal, payment.Amount)
		outputs = append(outputs, PlannedOutput{OutputPayment, payment.SpendPubKey, payment.ViewPubKey, new(big.Int).Set(payment.Amount), payment.PrivacyPolicy, payment.DisclosureMode, payment.DisclosureTargetPubKey})
	}
	change := new(big.Int).Sub(inputTotal, paymentTotal)
	if change.Sign() < 0 {
		return nil, fmt.Errorf("payment total exceeds selected input total")
	}
	if change.Sign() > 0 {
		if len(outputs) == 32 {
			return nil, fmt.Errorf("change requires payment count <= 31")
		}
		outputs = append(outputs, PlannedOutput{OutputChange, input.OwnerSpendPubKey, input.OwnerViewPubKey, new(big.Int).Set(change), 0, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, nil})
	}
	padding := input.PaddingCount
	if input.Mode == OutputModeExact32 {
		padding = 32 - len(outputs)
	}
	if padding < 0 || len(outputs)+padding > 32 {
		return nil, fmt.Errorf("payment/change/padding output count must be in 1..32")
	}
	if input.Mode == OutputModeCompact && input.PaddingCount != 0 {
		return nil, fmt.Errorf("compact mode does not permit padding")
	}
	for i := 0; i < padding; i++ {
		outputs = append(outputs, PlannedOutput{OutputPadding, input.OwnerSpendPubKey, input.OwnerViewPubKey, big.NewInt(0), 0, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, nil})
	}
	return &BatchTransferPlan{append([]InputNote(nil), input.Inputs...), outputs, inputTotal, paymentTotal, change}, nil
}

func pointBytesFromNote(note privacytypes.Note, spend bool) []byte {
	var pX, pY *big.Int
	if spend {
		pX, pY = note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY
	} else {
		pX, pY = note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY
	}
	var p [32]byte
	_ = p
	point, err := pointFromCoordinates(pX, pY)
	if err != nil {
		return nil
	}
	b := point.Bytes()
	return append([]byte(nil), b[:]...)
}
