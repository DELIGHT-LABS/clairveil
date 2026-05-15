package provertransport

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildTransferProofResponseRoundTrip(t *testing.T) {
	payload, artifacts, runner := testPreparedTransferPayload(t)

	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)

	response, err := BuildTransferProofResponse(*request, artifacts, runner)
	require.NoError(t, err)
	require.NoError(t, ValidateTransferProofResponse(*request, *response))

	msg, err := request.Payload.ToMsg(response.Proof)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())
}

func TestValidateTransferProofRequestRejectsHashMismatch(t *testing.T) {
	payload, _, _ := testPreparedTransferPayload(t)
	payload.Creator = sdk.AccAddress(bytes.Repeat([]byte{0x9}, 20)).String()

	err := ValidateTransferProofRequest(TransferProofRequest{
		Version: TransferProofRequestVersion,
		Payload: payload,
	})
	require.ErrorContains(t, err, "hash mismatch")
}

func TestBuildWithdrawProofResponseRoundTrip(t *testing.T) {
	now := time.Now()
	payload, artifacts, runner := testPreparedWithdrawProverPayload(t, now)

	request, err := NewWithdrawProofRequest(payload, now)
	require.NoError(t, err)

	response, err := BuildWithdrawProofResponse(*request, artifacts, runner, now)
	require.NoError(t, err)
	require.NoError(t, ValidateWithdrawProofResponse(*request, *response, now))

	finalPayload, err := request.Payload.ToPreparedWithdrawPayload(response.Proof, now)
	require.NoError(t, err)
	require.NoError(t, privacywithdraw.ValidatePreparedWithdrawPayloadMetadata(*finalPayload, now))
}

func TestValidateWithdrawProofRequestRejectsExpiredPayload(t *testing.T) {
	now := time.Now()
	payload, _, _ := testPreparedWithdrawProverPayload(t, now)
	payload.ExpiresAtUnix = now.Add(-time.Minute).Unix()
	payload.PayloadHash = privacywithdraw.ComputePreparedWithdrawProverPayloadHash(payload)

	err := ValidateWithdrawProofRequest(WithdrawProofRequest{
		Version: WithdrawProofRequestVersion,
		Payload: payload,
	}, now)
	require.ErrorContains(t, err, "expired")
}

func TestTransferAndWithdrawProofJSONRoundTrip(t *testing.T) {
	now := time.Now()

	transferPayload, transferArtifacts, transferRunner := testPreparedTransferPayload(t)
	transferRequest, err := NewTransferProofRequest(transferPayload)
	require.NoError(t, err)
	transferResponse, err := BuildTransferProofResponse(*transferRequest, transferArtifacts, transferRunner)
	require.NoError(t, err)

	transferRequestJSON, err := transferRequest.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedTransferRequest, err := DecodeTransferProofRequestJSON(transferRequestJSON)
	require.NoError(t, err)
	require.Equal(t, transferRequest.Payload.PayloadHash, decodedTransferRequest.Payload.PayloadHash)

	transferResponseJSON, err := transferResponse.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedTransferResponse, err := DecodeTransferProofResponseJSON(transferResponseJSON)
	require.NoError(t, err)
	require.Equal(t, transferResponse.Proof.PayloadHash, decodedTransferResponse.Proof.PayloadHash)

	withdrawPayload, withdrawArtifacts, withdrawRunner := testPreparedWithdrawProverPayload(t, now)
	withdrawRequest, err := NewWithdrawProofRequest(withdrawPayload, now)
	require.NoError(t, err)
	withdrawResponse, err := BuildWithdrawProofResponse(*withdrawRequest, withdrawArtifacts, withdrawRunner, now)
	require.NoError(t, err)

	withdrawRequestJSON, err := withdrawRequest.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedWithdrawRequest, err := DecodeWithdrawProofRequestJSON(withdrawRequestJSON)
	require.NoError(t, err)
	require.Equal(t, withdrawRequest.Payload.PayloadHash, decodedWithdrawRequest.Payload.PayloadHash)

	withdrawResponseJSON, err := withdrawResponse.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedWithdrawResponse, err := DecodeWithdrawProofResponseJSON(withdrawResponseJSON)
	require.NoError(t, err)
	require.Equal(t, withdrawResponse.Proof.PayloadHash, decodedWithdrawResponse.Proof.PayloadHash)
}

func testPreparedTransferPayload(
	t *testing.T,
) (
	privacytransfer.PreparedTransferPayload,
	*transferArtifactProvider,
	*transferProofRunner,
) {
	t.Helper()

	senderSpendPubKey := testPoint(61)
	senderViewPubKey := testPoint(67)
	recipientSpendPubKey := testPoint(71)
	recipientViewPubKey := testPoint(73)
	disclosurePubKey := testPoint(79)
	auditPubKey := testPoint(83)

	inputs := [2]privacyscan.FoundNote{
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(7),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(701),
				Memo:                 "input-1",
			},
		},
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(5),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(702),
				Memo:                 "input-2",
			},
		},
	}

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)

	merkleProvider := &transferMerklePathProvider{paths: map[string]*privacytransfer.MerklePathResult{}}
	for _, transferInput := range inputs {
		commitmentHex, err := privacyfield.CanonicalHexFromBigInt(transferInput.Note.ComputeCommitment())
		require.NoError(t, err)
		merkleProvider.paths[commitmentHex] = &privacytransfer.MerklePathResult{
			Root:       rootBytes,
			Path:       []string{"01", "02"},
			PathHelper: []uint32{0, 1},
		}
	}

	signature := testSignatureBytes()
	signer := &transferNoteSigner{signature: signature}
	artifacts := &transferArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &transferProofRunner{
		proof: groth16.NewProof(ecc.BN254),
	}

	disclosurePubKeyBytes := disclosurePubKey.Bytes()
	auditPubKeyBytes := auditPubKey.Bytes()

	payload, err := privacytransfer.BuildPreparedTransferPayload(
		context.Background(),
		merkleProvider,
		signer,
		privacytransfer.BuildTransferMessageInput{
			Creator:                       sdk.AccAddress(bytes.Repeat([]byte{0x1}, 20)).String(),
			Inputs:                        inputs,
			RecipientSpendPubKey:          recipientSpendPubKey,
			RecipientViewPubKey:           recipientViewPubKey,
			TransferAmount:                big.NewInt(7),
			TransferDenom:                 "uclair",
			SenderSpendPubKey:             senderSpendPubKey,
			SenderViewPubKey:              senderViewPubKey,
			UserPrivacyPolicy:             privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
			UserDisclosureMode:            privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
			UserDisclosureTargetPubKey:    disclosurePubKey,
			UserDisclosureTargetPubKeyBz:  append([]byte(nil), disclosurePubKeyBytes[:]...),
			AuditDisclosureTargetPubKey:   auditPubKey,
			AuditDisclosureTargetPubKeyBz: append([]byte(nil), auditPubKeyBytes[:]...),
		},
	)
	require.NoError(t, err)

	return *payload, artifacts, runner
}

func testPreparedWithdrawProverPayload(
	t *testing.T,
	now time.Time,
) (
	privacywithdraw.PreparedWithdrawProverPayload,
	*withdrawArtifactProvider,
	*withdrawProofRunner,
) {
	t.Helper()

	selectedNote := testWithdrawFoundNote(10, "uclair", 701)
	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(selectedNote.Note.ComputeCommitment())
	require.NoError(t, err)
	recipient, err := sdk.AccAddressFromBech32(testBech32AddressWithByte(0x2))
	require.NoError(t, err)

	source := &withdrawNoteSource{
		responses: [][]privacyscan.FoundNote{{selectedNote}},
	}
	planner := &withdrawAutoPlanner{}
	merklePaths := &withdrawMerklePathProvider{
		paths: map[string]*privacywithdraw.MerklePathResult{
			commitmentHex: {
				Root:       rootBytes,
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}
	signer := &withdrawSpendSigner{signature: testSignatureBytes()}
	artifacts := &withdrawArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &withdrawProofRunner{
		proof: groth16.NewProof(ecc.BN254),
	}

	result, err := privacywithdraw.BuildPreparedWithdrawProverPayload(
		context.Background(),
		source,
		planner,
		merklePaths,
		signer,
		privacywithdraw.BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin("uclair", 10),
			Recipient:  recipient,
			ChainID:    "clairveil-local-1",
			ExpiresAt:  now.Add(time.Hour),
			AutoPlan:   false,
		},
	)
	require.NoError(t, err)

	return *result.Payload, artifacts, runner
}

type transferMerklePathProvider struct {
	paths map[string]*privacytransfer.MerklePathResult
}

func (s *transferMerklePathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*privacytransfer.MerklePathResult, error) {
	return s.paths[commitmentHex], nil
}

type transferNoteSigner struct {
	signature []byte
}

func (s *transferNoteSigner) SignNoteHash(_ *big.Int) ([]byte, error) {
	return append([]byte(nil), s.signature...), nil
}

type transferArtifactProvider struct {
	r1cs       constraint.ConstraintSystem
	provingKey groth16.ProvingKey
}

func (s *transferArtifactProvider) JoinSplitR1CS() (constraint.ConstraintSystem, error) {
	return s.r1cs, nil
}

func (s *transferArtifactProvider) JoinSplitProvingKey() (groth16.ProvingKey, error) {
	return s.provingKey, nil
}

type transferProofRunner struct {
	proof   groth16.Proof
	witness witness.Witness
}

func (s *transferProofRunner) ProveJoinSplit(_ constraint.ConstraintSystem, _ groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error) {
	s.witness = witness
	return s.proof, nil
}

type withdrawNoteSource struct {
	responses [][]privacyscan.FoundNote
}

func (s *withdrawNoteSource) LoadFoundNotes(_ context.Context) ([]privacyscan.FoundNote, error) {
	if len(s.responses) == 0 {
		return nil, nil
	}
	return append([]privacyscan.FoundNote(nil), s.responses[0]...), nil
}

type withdrawAutoPlanner struct{}

func (*withdrawAutoPlanner) AutoPlanExactMatchNote(_ context.Context, _ sdk.Coin) error {
	return nil
}

type withdrawMerklePathProvider struct {
	paths map[string]*privacywithdraw.MerklePathResult
}

func (s *withdrawMerklePathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*privacywithdraw.MerklePathResult, error) {
	return s.paths[commitmentHex], nil
}

type withdrawSpendSigner struct {
	signature []byte
}

func (s *withdrawSpendSigner) SignSpendNoteHash(_ *big.Int) ([]byte, error) {
	return append([]byte(nil), s.signature...), nil
}

type withdrawArtifactProvider struct {
	r1cs       constraint.ConstraintSystem
	provingKey groth16.ProvingKey
}

func (s *withdrawArtifactProvider) SpendR1CS() (constraint.ConstraintSystem, error) {
	return s.r1cs, nil
}

func (s *withdrawArtifactProvider) SpendProvingKey() (groth16.ProvingKey, error) {
	return s.provingKey, nil
}

type withdrawProofRunner struct {
	proof   groth16.Proof
	witness witness.Witness
}

func (s *withdrawProofRunner) ProveSpend(_ constraint.ConstraintSystem, _ groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error) {
	s.witness = witness
	return s.proof, nil
}

func testWithdrawFoundNote(amount int64, denom string, randomness int64) privacyscan.FoundNote {
	spendPubKey := testPoint(31)
	viewPubKey := testPoint(37)
	return privacyscan.FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(spendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(spendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(viewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(viewPubKey, false),
			Amount:               big.NewInt(amount),
			AssetID:              privacycrypto.HashString(denom),
			Randomness:           big.NewInt(randomness),
		},
	}
}

func testPoint(value int64) *crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&curve.Base, big.NewInt(value))
	return &pubKey
}

func pointCoordinate(point *crypto_tedwards.PointAffine, x bool) *big.Int {
	value := new(big.Int)
	if x {
		point.X.BigInt(value)
		return value
	}
	point.Y.BigInt(value)
	return value
}

func testSignatureBytes() []byte {
	signaturePubKey := testPoint(17)
	pointBytes := signaturePubKey.Bytes()
	signatureBytes := make([]byte, 64)
	copy(signatureBytes[:32], pointBytes[:])

	sValue := big.NewInt(19).Bytes()
	copy(signatureBytes[64-len(sValue):], sValue)
	return signatureBytes
}

func testBech32AddressWithByte(b byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String()
}
