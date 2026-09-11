package withdraw

import (
	"bytes"
	"fmt"
	"math/big"

	privacycircuit "github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PrepareAuditV2 creates the withdraw v2 final PI/envelope boundary.
func PrepareAuditV2(input privacyaudit.PrepareInput) (*privacyaudit.Prepared, error) {
	if input.Kind != auditfield.KindWithdraw || len(input.Inputs) != 1 || len(input.Outputs) != 0 || input.Recipient == "" {
		return nil, fmt.Errorf("withdraw v2 requires one input, recipient and no outputs")
	}
	return privacyaudit.Prepare(input)
}

// PrepareAuditV2FromSpend promotes the existing normal selected note/path to
// a final v2 audit preparation. Creator and recipient are parsed as canonical
// addresses; the recipient bytes bound in PI are derived from recipient and
// checked against the existing normal spend assignment.
func PrepareAuditV2FromSpend(snapshot privacyaudit.Snapshot, legacy *PreparedSpendWithdraw, creator, recipient, amount string) (*privacyaudit.Prepared, error) {
	creatorAddress, creatorErr := sdk.AccAddressFromBech32(creator)
	recipientAddress, recipientErr := sdk.AccAddressFromBech32(recipient)
	if legacy == nil || creatorErr != nil || recipientErr != nil || creatorAddress.String() != creator || recipientAddress.String() != recipient {
		return nil, fmt.Errorf("withdraw audit snapshot, selected note, creator and recipient are required")
	}
	if err := legacy.Note.ValidateV1(); err != nil {
		return nil, fmt.Errorf("invalid selected withdraw note: %w", err)
	}
	coin, err := sdk.ParseCoinNormalized(amount)
	if err != nil || !coin.IsPositive() || coin.String() != amount || coin.Amount.BigInt().Cmp(new(big.Int).SetUint64(legacy.Note.Amount)) != 0 {
		return nil, fmt.Errorf("withdraw amount does not match the selected note")
	}
	expectedAsset := privacytypes.ComputeAssetIDV1(coin.Denom).FillBytes(make([]byte, 32))
	noteAsset := legacy.Note.AssetID.Bytes()
	if !bytes.Equal(expectedAsset, noteAsset[:]) {
		return nil, fmt.Errorf("withdraw amount denom does not match the selected note asset")
	}
	recipientDigest, err := privacytypes.ComputeWithdrawRecipientDigestV1(recipientAddress)
	if err != nil {
		return nil, err
	}
	oldRecipientHi, hiOK := legacy.Assignment.RecipientDigestHi.(*big.Int)
	oldRecipientLo, loOK := legacy.Assignment.RecipientDigestLo.(*big.Int)
	if !hiOK || !loOK || oldRecipientHi.Cmp(recipientDigest.Hi) != 0 || oldRecipientLo.Cmp(recipientDigest.Lo) != 0 {
		return nil, fmt.Errorf("withdraw recipient does not match the normal spend assignment")
	}
	commitment, err := legacy.Note.CommitmentV1()
	if err != nil {
		return nil, err
	}
	commitmentRaw := commitment.Bytes()
	assetRaw := legacy.Note.AssetID.Bytes()
	asset, err := auditfield.ParseField32(assetRaw[:])
	if err != nil {
		return nil, err
	}
	cin, err := auditfield.ParseField32(commitmentRaw[:])
	if err != nil {
		return nil, err
	}
	return PrepareAuditV2(privacyaudit.PrepareInput{
		Snapshot: snapshot, Kind: auditfield.KindWithdraw, Creator: creator, Recipient: recipient, Amount: amount, ExpiresAtUnix: legacy.ExpiresAtUnix,
		Root: legacy.RootBytes, Inputs: [][]byte{legacy.NullifierBytes}, PublicAsset: assetRaw[:], Principal: bytes.Clone(recipientAddress), Plaintext: []auditfield.Field32{asset, cin},
	})
}

func BuildAuditV2Message(prepared *privacyaudit.Prepared, proof []byte) (*privacyv2.MsgWithdraw, error) {
	message, err := prepared.BuildMessage(proof)
	if err != nil {
		return nil, err
	}
	withdraw, ok := message.(*privacyv2.MsgWithdraw)
	if !ok {
		return nil, fmt.Errorf("prepared message is not a v2 withdraw")
	}
	return withdraw, nil
}

// BuildAuditV2Witness upgrades a normally selected withdraw note/path to the
// audit-field circuit and replaces the legacy spend intent with the final
// audit PI/root intent. sign receives that exact canonical field value.
func BuildAuditV2Witness(prepared *privacyaudit.Prepared, legacy *PreparedSpendWithdraw, sign func(*big.Int) ([]byte, error)) (witness.Witness, error) {
	if prepared == nil || prepared.Kind() != auditfield.KindWithdraw || legacy == nil || sign == nil {
		return nil, fmt.Errorf("withdraw audit preparation, selected note and signer are required")
	}
	publicInputs := prepared.PublicInputs()
	nullifierRoot, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorNullifierV1, 1, []*big.Int{new(big.Int).SetBytes(legacy.NullifierBytes)})
	if err != nil || len(publicInputs) != 23 || !bytes.Equal(publicInputs[8][:], legacy.RootBytes) || !bytes.Equal(publicInputs[11][:], nullifierRoot.FillBytes(make([]byte, 32))) {
		return nil, fmt.Errorf("selected withdraw note does not match final audit PI")
	}
	public, random, err := prepared.CircuitBinding()
	if err != nil {
		return nil, err
	}
	old := legacy.Assignment
	assignment := &privacycircuit.SpendAuditFieldV1{
		Public: public, Random: random, Nullifier: old.Nullifier, ReceiverSpendPubKey: old.ReceiverSpendPubKey,
		ReceiverViewPubKey: old.ReceiverViewPubKey, Randomness: old.Randomness, Path: old.Path, PathHelper: old.PathHelper,
	}
	intent, err := prepared.OwnerIntent()
	if err != nil {
		return nil, err
	}
	signature, err := sign(new(big.Int).SetBytes(intent[:]))
	if err != nil {
		return nil, err
	}
	if err := assignSignature(&assignment.Signature, signature); err != nil {
		return nil, err
	}
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField())
}
