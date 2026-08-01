package deposit

import (
	"encoding/hex"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
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

func BuildPreparedDepositProverPayload(note privacytypes.Note) (*PreparedDepositProverPayload, error) {
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
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(note.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid deposit asset id: %w", err)
	}
	randomnessHex, err := privacyfield.CanonicalHexFromBigInt(note.Randomness)
	if err != nil {
		return nil, fmt.Errorf("invalid deposit randomness: %w", err)
	}
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	if err != nil {
		return nil, fmt.Errorf("invalid deposit note commitment: %w", err)
	}

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
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	if err != nil {
		return nil, fmt.Errorf("invalid deposit note commitment: %w", err)
	}
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
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	if err != nil {
		return fmt.Errorf("invalid deposit note commitment: %w", err)
	}
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

func noteFromPreparedDepositProverPayload(payload PreparedDepositProverPayload) (*privacytypes.Note, error) {
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
	amount, err := privacytypes.ParseCanonicalShieldedAmount("deposit prover payload amount", payload.Amount)
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
	if commitment.Sign() == 0 {
		return nil, fmt.Errorf("deposit note commitment must be non-zero")
	}

	note := &privacytypes.Note{
		ReceiverSpendPubKeyX: spendKey.X.BigInt(new(big.Int)),
		ReceiverSpendPubKeyY: spendKey.Y.BigInt(new(big.Int)),
		ReceiverViewPubKeyX:  viewKey.X.BigInt(new(big.Int)),
		ReceiverViewPubKeyY:  viewKey.Y.BigInt(new(big.Int)),
		Amount:               amount,
		AssetID:              assetID,
		Randomness:           randomness,
		Memo:                 "",
	}
	if err := note.ValidateV1(); err != nil {
		return nil, fmt.Errorf("invalid deposit prover payload NoteV1: %w", err)
	}
	if note.ComputeCommitment().Cmp(commitment) != 0 {
		return nil, fmt.Errorf("deposit prover payload note commitment mismatch")
	}
	return note, nil
}

func depositPublicKeyHex(x, y *big.Int) (string, error) {
	if x == nil || y == nil {
		return "", fmt.Errorf("public key coordinates are required")
	}
	var point crypto_tedwards.PointAffine
	point.X.SetBigInt(x)
	point.Y.SetBigInt(y)
	if err := privacycrypto.ValidatePrimeSubgroupPoint(&point); err != nil {
		return "", err
	}
	encoded := point.Bytes()
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

func decodeDepositFieldHex(value, fieldName string) (*big.Int, error) {
	if err := validateExactLowerHex(value, privacyfield.ByteSize*2, fieldName); err != nil {
		return nil, err
	}
	encoded, err := privacyfield.DecodeCanonicalHex(value, fieldName)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(encoded), nil
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
