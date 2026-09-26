package deposit

import (
	"bytes"
	"fmt"
	"math/big"

	privacycircuit "github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PrepareAuditV2 creates the deposit v2 final PI/envelope boundary. Creator
// and Principal must be the same authenticated canonical address bytes; fee
// payer/granter never appears in this API.
func PrepareAuditV2(input privacyaudit.PrepareInput) (*privacyaudit.Prepared, error) {
	if input.Kind != auditfield.KindDeposit || input.Recipient != "" || len(input.Inputs) != 0 || len(input.Outputs) != 1 {
		return nil, fmt.Errorf("deposit v2 requires one output and no inputs or recipient")
	}
	return privacyaudit.Prepare(input)
}

// PrepareAuditV2FromNormalNote converts an already-created normal deposit
// output into its audit-field preparation. The public principal is always
// derived from the canonical creator address; it cannot be supplied as a
// funder/fee-payer override. output must be the exact encrypted note effect
// that will be broadcast; no separate initial note is created.
func PrepareAuditV2FromNormalNote(snapshot privacyaudit.Snapshot, creator, amount string, note privacytypes.SecretNoteV1, output *privacyv2.OutputEffect, expiresAtUnix int64) (*privacyaudit.Prepared, error) {
	creatorAddress, err := sdk.AccAddressFromBech32(creator)
	if err != nil || creatorAddress.String() != creator || output == nil {
		return nil, fmt.Errorf("canonical deposit creator and output are required")
	}
	if err := note.ValidateV1(); err != nil {
		return nil, err
	}
	coin, err := sdk.ParseCoinNormalized(amount)
	if err != nil || coin.IsNegative() || coin.String() != amount || coin.Amount.BigInt().BitLen() > 64 {
		return nil, fmt.Errorf("deposit amount must be a canonical uint64 coin")
	}
	expectedAsset := privacytypes.ComputeAssetIDV1(coin.Denom).FillBytes(make([]byte, 32))
	noteAsset := note.AssetID.Bytes()
	if !bytes.Equal(expectedAsset, noteAsset[:]) {
		return nil, fmt.Errorf("deposit amount denom does not match the normal note asset")
	}
	commitment, err := note.CommitmentV1()
	if err != nil {
		return nil, err
	}
	commitmentBytes := commitment.Bytes()
	if !bytes.Equal(output.Commitment, commitmentBytes[:]) {
		return nil, fmt.Errorf("deposit output does not match the normal note commitment")
	}
	asset, err := auditFieldFromDepositValue(note.AssetID)
	if err != nil {
		return nil, err
	}
	spendX, err := auditFieldFromDepositValue(note.ReceiverSpendPubKeyX)
	if err != nil {
		return nil, err
	}
	spendY, err := auditFieldFromDepositValue(note.ReceiverSpendPubKeyY)
	if err != nil {
		return nil, err
	}
	viewX, err := auditFieldFromDepositValue(note.ReceiverViewPubKeyX)
	if err != nil {
		return nil, err
	}
	viewY, err := auditFieldFromDepositValue(note.ReceiverViewPubKeyY)
	if err != nil {
		return nil, err
	}
	assetRaw := note.AssetID.Bytes()
	return PrepareAuditV2(privacyaudit.PrepareInput{
		Snapshot: snapshot, Kind: auditfield.KindDeposit, Creator: creator, Amount: amount, ExpiresAtUnix: expiresAtUnix,
		Outputs: []*privacyv2.OutputEffect{output}, PublicAsset: assetRaw[:], Principal: bytes.Clone(creatorAddress),
		Plaintext: []auditfield.Field32{asset, auditfield.Field32FromUint64(note.Amount), spendX, spendY, viewX, viewY},
	})
}

func auditFieldFromDepositValue(value privacycrypto.FieldValue) (auditfield.Field32, error) {
	raw := value.Bytes()
	return auditfield.ParseField32(raw[:])
}

func BuildAuditV2Message(prepared *privacyaudit.Prepared, proof []byte) (*privacyv2.MsgDeposit, error) {
	message, err := prepared.BuildMessage(proof)
	if err != nil {
		return nil, err
	}
	deposit, ok := message.(*privacyv2.MsgDeposit)
	if !ok {
		return nil, fmt.Errorf("prepared message is not a v2 deposit")
	}
	return deposit, nil
}

// BuildAuditV2Witness reuses the normal deposit note output and binds it to
// an already-finalized audit envelope. It creates no genesis/initial note and
// does not mutate the legacy deposit workflow.
func BuildAuditV2Witness(prepared *privacyaudit.Prepared, note privacytypes.SecretNoteV1) (witness.Witness, error) {
	if prepared == nil || prepared.Kind() != auditfield.KindDeposit {
		return nil, fmt.Errorf("deposit audit preparation is required")
	}
	if err := note.ValidateV1(); err != nil {
		return nil, err
	}
	public, random, err := prepared.CircuitBinding()
	if err != nil {
		return nil, err
	}
	commitment, err := note.CommitmentV1()
	if err != nil {
		return nil, err
	}
	commitmentBytes := commitment.Bytes()
	inputs := prepared.PublicInputs()
	commitmentRoot, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorCommitmentV1, 1, []*big.Int{new(big.Int).SetBytes(commitmentBytes[:])})
	if err != nil || !bytes.Equal(inputs[12][:], commitmentRoot.FillBytes(make([]byte, 32))) {
		return nil, fmt.Errorf("deposit note commitment does not match final audit PI")
	}
	w := note.ToProverWitnessV1()
	assignment := &privacycircuit.DepositAuditFieldV1{Public: public, Random: random, Commitment: new(big.Int).SetBytes(commitmentBytes[:]), Randomness: w.Randomness}
	assignment.ReceiverSpendPubKey.X, assignment.ReceiverSpendPubKey.Y = w.ReceiverSpendPubKeyX, w.ReceiverSpendPubKeyY
	assignment.ReceiverViewPubKey.X, assignment.ReceiverViewPubKey.Y = w.ReceiverViewPubKeyX, w.ReceiverViewPubKeyY
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField())
}
