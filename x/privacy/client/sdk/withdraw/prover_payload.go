package withdraw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	PreparedWithdrawProverPayloadVersion = "v1"
	PreparedWithdrawProofVersion         = "v1"
)

type PreparedWithdrawProverPayload struct {
	Version                   string   `json:"version"`
	RootHex                   string   `json:"root_hex"`
	NullifierHex              string   `json:"nullifier_hex"`
	Amount                    string   `json:"amount"`
	AssetDenom                string   `json:"asset_denom"`
	AssetIDHex                string   `json:"asset_id_hex"`
	Recipient                 string   `json:"recipient"`
	RecipientBytesHex         string   `json:"recipient_bytes_hex"`
	ChainID                   string   `json:"chain_id"`
	ExpiresAtUnix             int64    `json:"expires_at_unix"`
	NoteRandomnessHex         string   `json:"note_randomness_hex"`
	SpendPubKeyHex            string   `json:"spend_pubkey_hex"`
	ViewPubKeyHex             string   `json:"view_pubkey_hex"`
	MerklePath                []string `json:"merkle_path"`
	MerklePathHelper          []uint32 `json:"merkle_path_helper"`
	SpendNoteHashSignatureHex string   `json:"spend_note_hash_signature_hex"`
	PayloadHash               string   `json:"payload_hash"`
}

type PreparedWithdrawProof struct {
	Version     string `json:"version"`
	PayloadHash string `json:"payload_hash"`
	ProofHex    string `json:"proof_hex"`
}

type BuildPreparedWithdrawProverPayloadResult struct {
	SelectedNote privacyscan.FoundNote
	Payload      *PreparedWithdrawProverPayload
}

func BuildPreparedWithdrawProverPayload(
	ctx context.Context,
	source ExactMatchNoteSource,
	planner ExactMatchAutoPlanner,
	merklePaths MerklePathProvider,
	signer SpendNoteHashSigner,
	input BuildWithdrawPayloadInput,
) (*BuildPreparedWithdrawProverPayloadResult, error) {
	if source == nil {
		return nil, fmt.Errorf("an exact-match note source is required to build a withdraw prover payload")
	}
	if merklePaths == nil {
		return nil, fmt.Errorf("a merkle path provider is required to build a withdraw prover payload")
	}
	if signer == nil {
		return nil, fmt.Errorf("a spend note hash signer is required to build a withdraw prover payload")
	}
	if err := validateBuildWithdrawPayloadInput(input); err != nil {
		return nil, err
	}

	selectedFoundNote, err := ResolveExactMatchSpendableNote(ctx, source, planner, input.TargetCoin, input.AutoPlan)
	if err != nil {
		return nil, err
	}

	preparedWithdraw, err := PrepareSpendWithdraw(
		ctx,
		merklePaths,
		signer,
		PrepareSpendWithdrawInput{
			Note:           *selectedFoundNote,
			RecipientBytes: input.Recipient.Bytes(),
		},
	)
	if err != nil {
		return nil, err
	}

	rootHex, err := canonicalHexFromBytes(preparedWithdraw.RootBytes, "root")
	if err != nil {
		return nil, err
	}
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(selectedFoundNote.Note.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw asset id: %w", err)
	}
	randomnessHex, err := privacyfield.CanonicalHexFromBigInt(selectedFoundNote.Note.Randomness)
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw note randomness: %w", err)
	}
	spendPubKeyHex, err := withdrawNotePubKeyHex(selectedFoundNote.Note, true)
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw spend key: %w", err)
	}
	viewPubKeyHex, err := withdrawNotePubKeyHex(selectedFoundNote.Note, false)
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw view key: %w", err)
	}

	payload := &PreparedWithdrawProverPayload{
		Version:                   PreparedWithdrawProverPayloadVersion,
		RootHex:                   rootHex,
		NullifierHex:              hex.EncodeToString(preparedWithdraw.NullifierBytes),
		Amount:                    input.TargetCoin.Amount.String(),
		AssetDenom:                input.TargetCoin.Denom,
		AssetIDHex:                assetIDHex,
		Recipient:                 input.Recipient.String(),
		RecipientBytesHex:         hex.EncodeToString(input.Recipient.Bytes()),
		ChainID:                   strings.TrimSpace(input.ChainID),
		ExpiresAtUnix:             input.ExpiresAt.Unix(),
		NoteRandomnessHex:         randomnessHex,
		SpendPubKeyHex:            spendPubKeyHex,
		ViewPubKeyHex:             viewPubKeyHex,
		MerklePath:                append([]string(nil), preparedWithdraw.MerklePath...),
		MerklePathHelper:          append([]uint32(nil), preparedWithdraw.PathHelper...),
		SpendNoteHashSignatureHex: hex.EncodeToString(preparedWithdraw.Signature),
	}
	payload.PayloadHash = ComputePreparedWithdrawProverPayloadHash(*payload)

	return &BuildPreparedWithdrawProverPayloadResult{
		SelectedNote: *selectedFoundNote,
		Payload:      payload,
	}, nil
}

func ComputePreparedWithdrawProverPayloadHash(payload PreparedWithdrawProverPayload) string {
	var b strings.Builder

	write := func(value string) {
		b.WriteString(value)
		b.WriteByte('\n')
	}
	writeSlice := func(values []string) {
		write(strconv.Itoa(len(values)))
		for _, value := range values {
			write(value)
		}
	}
	writeUint32Slice := func(values []uint32) {
		write(strconv.Itoa(len(values)))
		for _, value := range values {
			write(strconv.FormatUint(uint64(value), 10))
		}
	}

	write(payload.Version)
	write(payload.RootHex)
	write(payload.NullifierHex)
	write(payload.Amount)
	write(payload.AssetDenom)
	write(payload.AssetIDHex)
	write(payload.Recipient)
	write(payload.RecipientBytesHex)
	write(payload.ChainID)
	write(strconv.FormatInt(payload.ExpiresAtUnix, 10))
	write(payload.NoteRandomnessHex)
	write(payload.SpendPubKeyHex)
	write(payload.ViewPubKeyHex)
	writeSlice(payload.MerklePath)
	writeUint32Slice(payload.MerklePathHelper)
	write(payload.SpendNoteHashSignatureHex)

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func ValidatePreparedWithdrawProverPayloadMetadata(payload PreparedWithdrawProverPayload, now time.Time) error {
	if payload.Version != PreparedWithdrawProverPayloadVersion {
		return fmt.Errorf("unsupported withdraw prover payload version %q (expected %q)", payload.Version, PreparedWithdrawProverPayloadVersion)
	}
	if payload.PayloadHash == "" || payload.PayloadHash != ComputePreparedWithdrawProverPayloadHash(payload) {
		return fmt.Errorf("withdraw prover payload hash mismatch; the file may have been modified after preparation")
	}
	if strings.TrimSpace(payload.ChainID) == "" {
		return fmt.Errorf("withdraw prover payload chain_id is required")
	}
	if payload.ExpiresAtUnix <= 0 {
		return fmt.Errorf("withdraw prover payload expires_at_unix must be positive")
	}
	if now.Unix() > payload.ExpiresAtUnix {
		return fmt.Errorf("withdraw prover payload expired; regenerate it before requesting a proof")
	}

	if _, err := privacyfield.DecodeCanonicalHex(payload.RootHex, "root"); err != nil {
		return err
	}
	if _, err := privacyfield.DecodeCanonicalHex(payload.NullifierHex, "nullifier"); err != nil {
		return err
	}
	if _, err := parseWithdrawAmount(payload.Amount); err != nil {
		return err
	}
	if strings.TrimSpace(payload.AssetDenom) == "" {
		return fmt.Errorf("withdraw prover payload asset_denom is required")
	}
	assetID, err := decodeCanonicalHexBigInt(payload.AssetIDHex, "asset id")
	if err != nil {
		return err
	}
	if assetID.Cmp(privacycrypto.HashString(payload.AssetDenom)) != 0 {
		return fmt.Errorf("withdraw prover payload asset_denom %q does not match asset_id_hex %s", payload.AssetDenom, payload.AssetIDHex)
	}

	recipient, err := sdk.AccAddressFromBech32(payload.Recipient)
	if err != nil {
		return fmt.Errorf("invalid withdraw prover payload recipient: %w", err)
	}
	recipientBytes, err := decodeOpaqueHex(payload.RecipientBytesHex, "recipient bytes")
	if err != nil {
		return err
	}
	if !bytesEqual(recipient.Bytes(), recipientBytes) {
		return fmt.Errorf("withdraw prover payload recipient_bytes_hex does not match recipient")
	}

	if _, err := privacyfield.DecodeCanonicalHex(payload.NoteRandomnessHex, "note randomness"); err != nil {
		return err
	}
	if _, err := decodePublicKeyHex(payload.SpendPubKeyHex, "spend pubkey"); err != nil {
		return err
	}
	if _, err := decodePublicKeyHex(payload.ViewPubKeyHex, "view pubkey"); err != nil {
		return err
	}
	if _, err := decodeSignatureHex(payload.SpendNoteHashSignatureHex); err != nil {
		return fmt.Errorf("invalid spend note hash signature: %w", err)
	}

	return nil
}

func DecodePreparedWithdrawProverPayloadJSON(payloadBytes []byte) (*PreparedWithdrawProverPayload, error) {
	var payload PreparedWithdrawProverPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("invalid withdraw prover payload JSON: %w", err)
	}
	return &payload, nil
}

func ReadPreparedWithdrawProverPayloadFile(path string) (*PreparedWithdrawProverPayload, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedWithdrawProverPayloadJSON(payloadBytes)
}

func (p PreparedWithdrawProverPayload) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func (p PreparedWithdrawProverPayload) WriteJSONFile(path string) error {
	payloadBytes, err := p.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0o600)
}

func BuildPreparedWithdrawProof(
	payload PreparedWithdrawProverPayload,
	artifacts SpendArtifactProvider,
	runner SpendProofRunner,
) (*PreparedWithdrawProof, error) {
	proofBytes, err := ProvePreparedWithdrawPayload(payload, artifacts, runner)
	if err != nil {
		return nil, err
	}

	return &PreparedWithdrawProof{
		Version:     PreparedWithdrawProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    hex.EncodeToString(proofBytes),
	}, nil
}

func ValidatePreparedWithdrawProof(payload PreparedWithdrawProverPayload, proof PreparedWithdrawProof, now time.Time) error {
	if proof.Version != PreparedWithdrawProofVersion {
		return fmt.Errorf("unsupported withdraw proof version %q (expected %q)", proof.Version, PreparedWithdrawProofVersion)
	}
	if err := ValidatePreparedWithdrawProverPayloadMetadata(payload, now); err != nil {
		return err
	}
	if proof.PayloadHash == "" || proof.PayloadHash != payload.PayloadHash {
		return fmt.Errorf("withdraw proof payload hash mismatch")
	}
	if _, err := hex.DecodeString(proof.ProofHex); err != nil {
		return fmt.Errorf("invalid withdraw proof hex: %w", err)
	}
	return nil
}

func DecodePreparedWithdrawProofJSON(payloadBytes []byte) (*PreparedWithdrawProof, error) {
	var proof PreparedWithdrawProof
	if err := json.Unmarshal(payloadBytes, &proof); err != nil {
		return nil, fmt.Errorf("invalid withdraw proof JSON: %w", err)
	}
	return &proof, nil
}

func ReadPreparedWithdrawProofFile(path string) (*PreparedWithdrawProof, error) {
	proofBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedWithdrawProofJSON(proofBytes)
}

func (p PreparedWithdrawProof) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func (p PreparedWithdrawProof) WriteJSONFile(path string) error {
	proofBytes, err := p.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, proofBytes, 0o600)
}

func (p PreparedWithdrawProverPayload) ToPreparedWithdrawPayload(proof PreparedWithdrawProof, now time.Time) (*PreparedWithdrawPayload, error) {
	if err := ValidatePreparedWithdrawProof(p, proof, now); err != nil {
		return nil, err
	}

	proofBytes, err := hex.DecodeString(proof.ProofHex)
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw proof hex: %w", err)
	}
	rootBytes, err := privacyfield.DecodeCanonicalHex(p.RootHex, "root")
	if err != nil {
		return nil, err
	}
	nullifierBytes, err := privacyfield.DecodeCanonicalHex(p.NullifierHex, "nullifier")
	if err != nil {
		return nil, err
	}
	coin, err := sdk.ParseCoinNormalized(strings.TrimSpace(p.Amount) + strings.TrimSpace(p.AssetDenom))
	if err != nil {
		return nil, fmt.Errorf("invalid withdraw amount/denom pair: %w", err)
	}

	return BuildPreparedWithdrawPayload(BuildPreparedWithdrawPayloadInput{
		ProofBytes:     proofBytes,
		RootBytes:      rootBytes,
		NullifierBytes: nullifierBytes,
		Amount:         coin.String(),
		Recipient:      p.Recipient,
		ChainID:        p.ChainID,
		ExpiresAtUnix:  p.ExpiresAtUnix,
	})
}

func ProvePreparedWithdrawPayload(
	payload PreparedWithdrawProverPayload,
	artifacts SpendArtifactProvider,
	runner SpendProofRunner,
) ([]byte, error) {
	assignment, err := buildSpendAssignmentFromPreparedWithdrawPayload(payload, time.Now())
	if err != nil {
		return nil, err
	}
	return ProveSpendWithdrawAssignment(assignment, artifacts, runner)
}

func buildSpendAssignmentFromPreparedWithdrawPayload(payload PreparedWithdrawProverPayload, now time.Time) (*circuit.SpendCircuit, error) {
	if err := ValidatePreparedWithdrawProverPayloadMetadata(payload, now); err != nil {
		return nil, err
	}

	rootBytes, err := privacyfield.DecodeCanonicalHex(payload.RootHex, "root")
	if err != nil {
		return nil, err
	}
	nullifier, err := decodeCanonicalHexBigInt(payload.NullifierHex, "nullifier")
	if err != nil {
		return nil, err
	}
	amount, err := parseWithdrawAmount(payload.Amount)
	if err != nil {
		return nil, err
	}
	assetID, err := decodeCanonicalHexBigInt(payload.AssetIDHex, "asset id")
	if err != nil {
		return nil, err
	}
	recipientBytes, err := decodeOpaqueHex(payload.RecipientBytesHex, "recipient bytes")
	if err != nil {
		return nil, err
	}
	randomness, err := decodeCanonicalHexBigInt(payload.NoteRandomnessHex, "note randomness")
	if err != nil {
		return nil, err
	}
	spendPubKey, err := decodePublicKeyHex(payload.SpendPubKeyHex, "spend pubkey")
	if err != nil {
		return nil, err
	}
	viewPubKey, err := decodePublicKeyHex(payload.ViewPubKeyHex, "view pubkey")
	if err != nil {
		return nil, err
	}
	signatureBytes, err := decodeSignatureHex(payload.SpendNoteHashSignatureHex)
	if err != nil {
		return nil, fmt.Errorf("invalid spend note hash signature: %w", err)
	}

	assignment := &circuit.SpendCircuit{
		MerkleRoot: new(big.Int).SetBytes(rootBytes),
		Amount:     amount,
		Recipient:  new(big.Int).SetBytes(recipientBytes),
		AssetID:    assetID,
		Nullifier:  nullifier,
		Randomness: randomness,
	}
	assignPubKey(&assignment.ReceiverSpendPubKey, *spendPubKey)
	assignPubKey(&assignment.ReceiverViewPubKey, *viewPubKey)
	assignSignature(&assignment.Signature, signatureBytes)

	pathNodes, pathHelpers := decodeMerkleProof(payload.MerklePath, payload.MerklePathHelper)
	for depth := 0; depth < circuit.MerkleDepth; depth++ {
		assignment.Path[depth] = pathNodes[depth]
		assignment.PathHelper[depth] = pathHelpers[depth]
	}

	expectedNullifier := privacycrypto.MimcHash(
		randomness,
		pointAffineCoordinate(spendPubKey, true),
		pointAffineCoordinate(spendPubKey, false),
	)
	if expectedNullifier.Cmp(nullifier) != 0 {
		return nil, fmt.Errorf("withdraw nullifier does not match payload witness")
	}

	return assignment, nil
}

func validateBuildWithdrawPayloadInput(input BuildWithdrawPayloadInput) error {
	if !input.TargetCoin.IsValid() || !input.TargetCoin.Amount.IsPositive() {
		return fmt.Errorf("target coin must be a valid positive coin")
	}
	if input.Recipient.Empty() {
		return fmt.Errorf("recipient address is required to build a withdraw payload")
	}
	if strings.TrimSpace(input.ChainID) == "" {
		return fmt.Errorf("chain id is required to build a withdraw payload")
	}
	if input.ExpiresAt.IsZero() {
		return fmt.Errorf("expires_at is required to build a withdraw payload")
	}
	return nil
}

func canonicalHexFromBytes(bz []byte, fieldName string) (string, error) {
	if err := privacyfield.ValidateCanonicalBytes32(bz); err != nil {
		return "", fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	return hex.EncodeToString(bz), nil
}

func parseWithdrawAmount(value string) (*big.Int, error) {
	parsed, ok := new(big.Int).SetString(strings.TrimSpace(value), 10)
	if !ok || parsed.Sign() <= 0 {
		return nil, fmt.Errorf("withdraw prover payload amount must be a positive decimal string")
	}
	return parsed, nil
}

func decodeCanonicalHexBigInt(value, fieldName string) (*big.Int, error) {
	bz, err := privacyfield.DecodeCanonicalHex(value, fieldName)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(bz), nil
}

func decodePublicKeyHex(value, fieldName string) (*crypto_tedwards.PointAffine, error) {
	bz, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}
	var point crypto_tedwards.PointAffine
	if _, err := point.SetBytes(bz); err != nil {
		return nil, fmt.Errorf("invalid %s bytes: %w", fieldName, err)
	}
	return &point, nil
}

func decodeSignatureHex(value string) ([]byte, error) {
	signatureBytes, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(signatureBytes) != 64 {
		return nil, fmt.Errorf("signature must be 64 bytes")
	}
	return signatureBytes, nil
}

func decodeOpaqueHex(value, fieldName string) ([]byte, error) {
	bz, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}
	return bz, nil
}

func withdrawNotePubKeyHex(note privacytypes.Note, spend bool) (string, error) {
	var point *crypto_tedwards.PointAffine
	var err error
	if spend {
		point, err = spendPubKeyFromNote(note)
	} else {
		point, err = viewPubKeyFromNote(note)
	}
	if err != nil {
		return "", err
	}

	pointBytes := point.Bytes()
	return hex.EncodeToString(pointBytes[:]), nil
}

func pointAffineCoordinate(point *crypto_tedwards.PointAffine, x bool) *big.Int {
	coordinate := new(big.Int)
	if x {
		point.X.BigInt(coordinate)
		return coordinate
	}
	point.Y.BigInt(coordinate)
	return coordinate
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
