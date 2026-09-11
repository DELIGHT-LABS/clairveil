package transfer

import (
	"fmt"
	"io"
	"math"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// SecretJoinSplitRequest is the wallet-side request used before a circuit
// witness exists. It never stores a Note or private field as big.Int.
type SecretJoinSplitRequest struct {
	Inputs [2]privacyscan.SecretFoundNote

	RecipientSpendX privacycrypto.FieldValue
	RecipientSpendY privacycrypto.FieldValue
	RecipientViewX  privacycrypto.FieldValue
	RecipientViewY  privacycrypto.FieldValue

	SenderSpendX privacycrypto.FieldValue
	SenderSpendY privacycrypto.FieldValue
	SenderViewX  privacycrypto.FieldValue
	SenderViewY  privacycrypto.FieldValue

	TransferAmount uint64
}

// SecretJoinSplitOutputs holds freshly sampled output notes and their fixed
// MiMC commitments. The only conversion to a legacy Note belongs at the
// prover boundary via SecretNoteV1.ToProverWitnessV1.
type SecretJoinSplitOutputs struct {
	RecipientNote       privacytypes.SecretNoteV1
	ChangeNote          privacytypes.SecretNoteV1
	RecipientCommitment privacycrypto.FieldValue
	ChangeCommitment    privacycrypto.FieldValue
}

// PrepareSecretJoinSplitOutputsForAsset is the output-only form used by the
// existing circuit builder. Its caller has already selected inputs and is at
// the explicit conversion boundary; it never accepts a legacy Note.
func PrepareSecretJoinSplitOutputsForAsset(reader io.Reader, asset privacycrypto.FieldValue, recipientAmount, changeAmount uint64, recipientSpendX, recipientSpendY, recipientViewX, recipientViewY, senderSpendX, senderSpendY, senderViewX, senderViewY privacycrypto.FieldValue) (*SecretJoinSplitOutputs, error) {
	if reader == nil {
		return nil, fmt.Errorf("secret note randomness reader is required")
	}
	recipient, err := privacytypes.NewRandomSecretNoteV1(reader, recipientSpendX, recipientSpendY, recipientViewX, recipientViewY, recipientAmount, asset, "Transfer")
	if err != nil {
		return nil, err
	}
	change, err := privacytypes.NewRandomSecretNoteV1(reader, senderSpendX, senderSpendY, senderViewX, senderViewY, changeAmount, asset, "Change")
	if err != nil {
		return nil, err
	}
	recipientCommitment, err := privacytypes.SecretNoteCommitmentV1(*recipient)
	if err != nil {
		return nil, err
	}
	changeCommitment, err := privacytypes.SecretNoteCommitmentV1(*change)
	if err != nil {
		return nil, err
	}
	return &SecretJoinSplitOutputs{RecipientNote: *recipient, ChangeNote: *change, RecipientCommitment: recipientCommitment, ChangeCommitment: changeCommitment}, nil
}

// PrepareSecretJoinSplitOutputs creates the two output notes without routing
// their private fields through big.Int. Selection, Merkle witness construction
// and circuit assignment remain separate prover-boundary operations.
func PrepareSecretJoinSplitOutputs(reader io.Reader, request SecretJoinSplitRequest) (*SecretJoinSplitOutputs, error) {
	if reader == nil {
		return nil, fmt.Errorf("secret note randomness reader is required")
	}
	left, right := request.Inputs[0].Note, request.Inputs[1].Note
	if request.TransferAmount > math.MaxUint64-left.Amount {
		return nil, fmt.Errorf("input amount overflow")
	}
	total := left.Amount + right.Amount
	if request.TransferAmount > total {
		return nil, fmt.Errorf("transfer amount exceeds selected input total")
	}
	change := total - request.TransferAmount
	return PrepareSecretJoinSplitOutputsForAsset(reader, left.AssetID, request.TransferAmount, change,
		request.RecipientSpendX, request.RecipientSpendY, request.RecipientViewX, request.RecipientViewY,
		request.SenderSpendX, request.SenderSpendY, request.SenderViewX, request.SenderViewY)
}
