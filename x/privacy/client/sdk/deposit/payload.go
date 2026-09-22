package deposit

import (
	"encoding/hex"
	"fmt"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	PreparedDepositProverPayloadVersion = "v1"
	PreparedDepositProofVersion         = "v1"
)

type PreparedDepositProverPayload struct {
	Version                string `json:"version"`
	ReceiverSpendPubKeyHex string `json:"receiver_spend_pubkey_hex"`
	ReceiverViewPubKeyHex  string `json:"receiver_view_pubkey_hex"`
	Amount                 string `json:"amount"`
	AssetIDHex             string `json:"asset_id_hex"`
	RandomnessHex          string `json:"randomness_hex"`
	NoteCommitmentHex      string `json:"note_commitment_hex"`
}

type PreparedDepositProof struct {
	Version           string `json:"version"`
	NoteCommitmentHex string `json:"note_commitment_hex"`
	ProofHex          string `json:"proof_hex"`
}

func BuildPreparedDepositProverPayload(note privacytypes.SecretNoteV1) (*PreparedDepositProverPayload, error) {
	if err := note.ValidateV1(); err != nil {
		return nil, fmt.Errorf("invalid deposit NoteV1: %w", err)
	}

	spendKeyHex, err := depositPublicKeyHex(note.ReceiverSpendPubKeyX, note.ReceiverSpendPubKeyY)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver spend public key: %w", err)
	}
	viewKeyHex, err := depositPublicKeyHex(note.ReceiverViewPubKeyX, note.ReceiverViewPubKeyY)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver view public key: %w", err)
	}
	assetID := note.AssetID.Bytes()
	assetIDHex := hex.EncodeToString(assetID[:])
	randomness := note.Randomness.Bytes()
	randomnessHex := hex.EncodeToString(randomness[:])
	commitment, err := note.CommitmentV1()
	if err != nil {
		return nil, fmt.Errorf("invalid deposit note commitment: %w", err)
	}
	commitmentBytes := commitment.Bytes()
	commitmentHex := hex.EncodeToString(commitmentBytes[:])

	return &PreparedDepositProverPayload{
		Version:                PreparedDepositProverPayloadVersion,
		ReceiverSpendPubKeyHex: spendKeyHex,
		ReceiverViewPubKeyHex:  viewKeyHex,
		Amount:                 note.Amount.String(),
		AssetIDHex:             assetIDHex,
		RandomnessHex:          randomnessHex,
		NoteCommitmentHex:      commitmentHex,
	}, nil
}

func ValidatePreparedDepositProverPayload(payload PreparedDepositProverPayload) error {
	_, err := noteFromPreparedDepositProverPayload(payload)
	return err
}

func BuildPreparedDepositProof(
	payload PreparedDepositProverPayload,
	artifacts DepositArtifactProvider,
	runner DepositProofRunner,
) (*PreparedDepositProof, error) {
	note, err := noteFromPreparedDepositProverPayload(payload)
	if err != nil {
		return nil, err
	}
	proofBytes, err := BuildDepositProof(*note, artifacts, runner)
	if err != nil {
		return nil, err
	}
	if err := privacyzk.ValidateCanonicalProofBN254(proofBytes); err != nil {
		return nil, fmt.Errorf("invalid generated deposit proof: %w", err)
	}
	commitment, err := note.CommitmentV1()
	if err != nil {
		return nil, fmt.Errorf("invalid deposit note commitment: %w", err)
	}
	commitmentBytes := commitment.Bytes()
	commitmentHex := hex.EncodeToString(commitmentBytes[:])
	return &PreparedDepositProof{
		Version:           PreparedDepositProofVersion,
		NoteCommitmentHex: commitmentHex,
		ProofHex:          hex.EncodeToString(proofBytes),
	}, nil
}

func ValidatePreparedDepositProof(payload PreparedDepositProverPayload, proof PreparedDepositProof) error {
	note, err := noteFromPreparedDepositProverPayload(payload)
	if err != nil {
		return err
	}
	if proof.Version != PreparedDepositProofVersion {
		return fmt.Errorf("unsupported prepared deposit proof version %q (expected %q)", proof.Version, PreparedDepositProofVersion)
	}
	commitment, err := note.CommitmentV1()
	if err != nil {
		return fmt.Errorf("invalid deposit note commitment: %w", err)
	}
	commitmentBytes := commitment.Bytes()
	commitmentHex := hex.EncodeToString(commitmentBytes[:])
	if proof.NoteCommitmentHex != commitmentHex || proof.NoteCommitmentHex != payload.NoteCommitmentHex {
		return fmt.Errorf("deposit proof response note commitment mismatch")
	}
	if err := validateExactLowerHex(proof.ProofHex, privacyzk.CanonicalBN254Groth16ProofSize*2, "deposit proof"); err != nil {
		return err
	}
	proofBytes, err := hex.DecodeString(proof.ProofHex)
	if err != nil {
		return fmt.Errorf("invalid deposit proof hex: %w", err)
	}
	if err := privacyzk.ValidateCanonicalProofBN254(proofBytes); err != nil {
		return fmt.Errorf("invalid canonical deposit proof: %w", err)
	}
	return nil
}

func noteFromPreparedDepositProverPayload(payload PreparedDepositProverPayload) (*privacytypes.SecretNoteV1, error) {
	if payload.Version != PreparedDepositProverPayloadVersion {
		return nil, fmt.Errorf("unsupported deposit prover payload version %q (expected %q)", payload.Version, PreparedDepositProverPayloadVersion)
	}
	spendKey, err := decodeDepositPublicKeyHex(payload.ReceiverSpendPubKeyHex, "receiver spend public key")
	if err != nil {
		return nil, err
	}
	viewKey, err := decodeDepositPublicKeyHex(payload.ReceiverViewPubKeyHex, "receiver view public key")
	if err != nil {
		return nil, err
	}
	amount, err := parseDepositAmount(payload.Amount)
	if err != nil {
		return nil, err
	}
	assetID, err := decodeDepositFieldHex(payload.AssetIDHex, "asset id")
	if err != nil {
		return nil, err
	}
	randomness, err := decodeDepositFieldHex(payload.RandomnessHex, "randomness")
	if err != nil {
		return nil, err
	}
	commitment, err := decodeDepositFieldHex(payload.NoteCommitmentHex, "note commitment")
	if err != nil {
		return nil, err
	}
	if commitment.IsZero() {
		return nil, fmt.Errorf("deposit note commitment must be non-zero")
	}

	spendX, spendY, err := privacycrypto.PublicPointFieldValues(*spendKey)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver spend public key: %w", err)
	}
	viewX, viewY, err := privacycrypto.PublicPointFieldValues(*viewKey)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver view public key: %w", err)
	}
	note, err := privacytypes.NewSecretNoteV1(spendX, spendY, viewX, viewY, amount, assetID, randomness, "")
	if err != nil {
		return nil, fmt.Errorf("invalid deposit prover payload NoteV1: %w", err)
	}
	computedCommitment, err := note.CommitmentV1()
	if err != nil {
		return nil, fmt.Errorf("compute deposit note commitment: %w", err)
	}
	if !computedCommitment.Equal(commitment) {
		return nil, fmt.Errorf("deposit prover payload note commitment mismatch")
	}
	return note, nil
}

func depositPublicKeyHex(x, y privacycrypto.FieldValue) (string, error) {
	encoded, err := privacycrypto.LegacyCompressedPointFromFieldValues(x, y)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(encoded[:]), nil
}

func decodeDepositPublicKeyHex(value, fieldName string) (*crypto_tedwards.PointAffine, error) {
	if err := validateExactLowerHex(value, privacycrypto.CanonicalPointSize*2, fieldName); err != nil {
		return nil, err
	}
	encoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}
	point, err := privacycrypto.DecodeCanonicalPoint(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	return point, nil
}

func decodeDepositFieldHex(value, fieldName string) (privacycrypto.FieldValue, error) {
	if err := validateExactLowerHex(value, 64, fieldName); err != nil {
		return privacycrypto.FieldValue{}, err
	}
	encoded, err := hex.DecodeString(value)
	if err != nil {
		return privacycrypto.FieldValue{}, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}
	field, err := privacycrypto.ParseFieldValueBE32(encoded)
	if err != nil {
		return privacycrypto.FieldValue{}, fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	return field, nil
}

func parseDepositAmount(value string) (privacyamount.Amount128, error) {
	return privacyamount.Parse(value)
}

func validateExactLowerHex(value string, length int, fieldName string) error {
	if len(value) != length {
		return fmt.Errorf("%s must be exactly %d lowercase hex characters", fieldName, length)
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return fmt.Errorf("%s must be exactly %d lowercase hex characters", fieldName, length)
		}
	}
	return nil
}
