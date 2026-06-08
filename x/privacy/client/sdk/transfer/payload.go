package transfer

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

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	PreparedTransferPayloadVersion = "v1"
	PreparedTransferProofVersion   = "v1"
)

type PreparedTransferInput struct {
	Amount               string   `json:"amount"`
	RandomnessHex        string   `json:"randomness_hex"`
	SpendPubKeyHex       string   `json:"spend_pubkey_hex"`
	ViewPubKeyHex        string   `json:"view_pubkey_hex"`
	MerklePath           []string `json:"merkle_path"`
	MerklePathHelper     []uint32 `json:"merkle_path_helper"`
	NoteHashSignatureHex string   `json:"note_hash_signature_hex"`
	NullifierHex         string   `json:"nullifier_hex"`
}

type PreparedTransferOutput struct {
	Amount         string `json:"amount"`
	RandomnessHex  string `json:"randomness_hex"`
	SpendPubKeyHex string `json:"spend_pubkey_hex"`
	ViewPubKeyHex  string `json:"view_pubkey_hex"`
	CommitmentHex  string `json:"commitment_hex"`
}

type PreparedTransferPayload struct {
	Version                        string                   `json:"version"`
	Creator                        string                   `json:"creator"`
	RootHex                        string                   `json:"root_hex"`
	AssetIDHex                     string                   `json:"asset_id_hex"`
	Inputs                         []PreparedTransferInput  `json:"inputs"`
	Outputs                        []PreparedTransferOutput `json:"outputs"`
	CipherTextHexes                []string                 `json:"cipher_text_hexes"`
	UserPrivacyPolicy              uint32                   `json:"user_privacy_policy"`
	UserDisclosureMode             int32                    `json:"user_disclosure_mode"`
	UserDisclosureDigestHex        string                   `json:"user_disclosure_digest_hex,omitempty"`
	UserDisclosureTargetPubKeyHex  string                   `json:"user_disclosure_target_pubkey_hex,omitempty"`
	UserDisclosurePayloadHex       string                   `json:"user_disclosure_payload_hex,omitempty"`
	AuditDisclosureDigestHex       string                   `json:"audit_disclosure_digest_hex"`
	AuditDisclosureTargetPubKeyHex string                   `json:"audit_disclosure_target_pubkey_hex"`
	AuditDisclosurePayloadHex      string                   `json:"audit_disclosure_payload_hex"`
	PayloadHash                    string                   `json:"payload_hash"`
}

type PreparedTransferProof struct {
	Version     string `json:"version"`
	PayloadHash string `json:"payload_hash"`
	ProofHex    string `json:"proof_hex"`
}

func BuildPreparedTransferPayload(
	ctx context.Context,
	merklePaths MerklePathProvider,
	signer NoteHashSigner,
	input BuildTransferMessageInput,
) (*PreparedTransferPayload, error) {
	prepared, err := PrepareJoinSplitTransfer(
		ctx,
		merklePaths,
		signer,
		PrepareJoinSplitInput{
			Inputs:               input.Inputs,
			RecipientSpendPubKey: input.RecipientSpendPubKey,
			RecipientViewPubKey:  input.RecipientViewPubKey,
			TransferAmount:       input.TransferAmount,
			SenderSpendPubKey:    input.SenderSpendPubKey,
			SenderViewPubKey:     input.SenderViewPubKey,
		},
	)
	if err != nil {
		return nil, err
	}

	disclosureInput := DisclosureBuildInput{
		OutputCommitment: prepared.OutputCommitments[0],
		TransferDenom:    input.TransferDenom,
		FromNote:         prepared.FromNote,
		RecipientNote:    prepared.RecipientNote,
	}

	userDisclosureData, err := BuildUserDisclosureData(
		disclosureInput,
		input.UserPrivacyPolicy,
		input.UserDisclosureMode,
		input.UserDisclosureTargetPubKey,
	)
	if err != nil {
		return nil, err
	}
	auditDisclosureData, err := BuildAuditDisclosureData(disclosureInput, input.AuditDisclosureTargetPubKey)
	if err != nil {
		return nil, err
	}

	cipherTexts, err := EncryptOutputNotes(prepared.RecipientNote, prepared.ChangeNote)
	if err != nil {
		return nil, err
	}

	rootHex, err := hexFromCanonicalBytes(prepared.CommonRoot, "root")
	if err != nil {
		return nil, err
	}
	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(prepared.FromNote.AssetID)
	if err != nil {
		return nil, fmt.Errorf("invalid asset id: %w", err)
	}

	payload := &PreparedTransferPayload{
		Version:                        PreparedTransferPayloadVersion,
		Creator:                        input.Creator,
		RootHex:                        rootHex,
		AssetIDHex:                     assetIDHex,
		Inputs:                         make([]PreparedTransferInput, 0, len(input.Inputs)),
		Outputs:                        make([]PreparedTransferOutput, 0, 2),
		CipherTextHexes:                make([]string, 0, len(cipherTexts)),
		UserPrivacyPolicy:              input.UserPrivacyPolicy,
		UserDisclosureMode:             int32(input.UserDisclosureMode),
		AuditDisclosureDigestHex:       hex.EncodeToString(auditDisclosureData.Digest),
		AuditDisclosureTargetPubKeyHex: hex.EncodeToString(encodedDisclosureTargetBytes(input.AuditDisclosureTargetPubKey, input.AuditDisclosureTargetPubKeyBz)),
		AuditDisclosurePayloadHex:      hex.EncodeToString(auditDisclosureData.CipherText),
	}

	if userDisclosureData != nil {
		payload.UserDisclosureDigestHex = hex.EncodeToString(userDisclosureData.Digest)
		payload.UserDisclosureTargetPubKeyHex = hex.EncodeToString(encodedDisclosureTargetBytes(input.UserDisclosureTargetPubKey, input.UserDisclosureTargetPubKeyBz))
		payload.UserDisclosurePayloadHex = hex.EncodeToString(userDisclosureData.CipherText)
	}

	for i, foundNote := range input.Inputs {
		randomnessHex, err := privacyfield.CanonicalHexFromBigInt(foundNote.Note.Randomness)
		if err != nil {
			return nil, fmt.Errorf("invalid input randomness %d: %w", i, err)
		}
		spendPubKeyHex, err := notePubKeyHex(foundNote.Note, true)
		if err != nil {
			return nil, fmt.Errorf("invalid input spend key %d: %w", i, err)
		}
		viewPubKeyHex, err := notePubKeyHex(foundNote.Note, false)
		if err != nil {
			return nil, fmt.Errorf("invalid input view key %d: %w", i, err)
		}

		payload.Inputs = append(payload.Inputs, PreparedTransferInput{
			Amount:               foundNote.Note.Amount.String(),
			RandomnessHex:        randomnessHex,
			SpendPubKeyHex:       spendPubKeyHex,
			ViewPubKeyHex:        viewPubKeyHex,
			MerklePath:           append([]string(nil), prepared.InputMerklePaths[i]...),
			MerklePathHelper:     append([]uint32(nil), prepared.InputPathHelpers[i]...),
			NoteHashSignatureHex: hex.EncodeToString(prepared.InputSignatures[i]),
			NullifierHex:         hex.EncodeToString(prepared.InputNullifiers[i]),
		})
	}

	for i, outputNote := range []privacytypes.Note{prepared.RecipientNote, prepared.ChangeNote} {
		randomnessHex, err := privacyfield.CanonicalHexFromBigInt(outputNote.Randomness)
		if err != nil {
			return nil, fmt.Errorf("invalid output randomness %d: %w", i, err)
		}
		spendPubKeyHex, err := notePubKeyHex(outputNote, true)
		if err != nil {
			return nil, fmt.Errorf("invalid output spend key %d: %w", i, err)
		}
		viewPubKeyHex, err := notePubKeyHex(outputNote, false)
		if err != nil {
			return nil, fmt.Errorf("invalid output view key %d: %w", i, err)
		}

		payload.Outputs = append(payload.Outputs, PreparedTransferOutput{
			Amount:         outputNote.Amount.String(),
			RandomnessHex:  randomnessHex,
			SpendPubKeyHex: spendPubKeyHex,
			ViewPubKeyHex:  viewPubKeyHex,
			CommitmentHex:  hex.EncodeToString(prepared.OutputCommitments[i]),
		})
	}

	for _, cipherText := range cipherTexts {
		payload.CipherTextHexes = append(payload.CipherTextHexes, hex.EncodeToString(cipherText))
	}

	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)
	return payload, nil
}

func ComputePreparedTransferPayloadHash(payload PreparedTransferPayload) string {
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
	write(payload.Creator)
	write(payload.RootHex)
	write(payload.AssetIDHex)
	write(strconv.FormatUint(uint64(payload.UserPrivacyPolicy), 10))
	write(strconv.FormatInt(int64(payload.UserDisclosureMode), 10))
	write(payload.UserDisclosureDigestHex)
	write(payload.UserDisclosureTargetPubKeyHex)
	write(payload.UserDisclosurePayloadHex)
	write(payload.AuditDisclosureDigestHex)
	write(payload.AuditDisclosureTargetPubKeyHex)
	write(payload.AuditDisclosurePayloadHex)
	write(strconv.Itoa(len(payload.Inputs)))
	for _, input := range payload.Inputs {
		write(input.Amount)
		write(input.RandomnessHex)
		write(input.SpendPubKeyHex)
		write(input.ViewPubKeyHex)
		writeSlice(input.MerklePath)
		writeUint32Slice(input.MerklePathHelper)
		write(input.NoteHashSignatureHex)
		write(input.NullifierHex)
	}
	write(strconv.Itoa(len(payload.Outputs)))
	for _, output := range payload.Outputs {
		write(output.Amount)
		write(output.RandomnessHex)
		write(output.SpendPubKeyHex)
		write(output.ViewPubKeyHex)
		write(output.CommitmentHex)
	}
	writeSlice(payload.CipherTextHexes)

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func ValidatePreparedTransferPayloadMetadata(payload PreparedTransferPayload) error {
	if payload.Version != PreparedTransferPayloadVersion {
		return fmt.Errorf("unsupported transfer payload version %q (expected %q)", payload.Version, PreparedTransferPayloadVersion)
	}
	if payload.PayloadHash == "" || payload.PayloadHash != ComputePreparedTransferPayloadHash(payload) {
		return fmt.Errorf("transfer payload hash mismatch; the file may have been modified after preparation")
	}
	if len(payload.Inputs) != circuit.NumInputs {
		return fmt.Errorf("transfer payload requires exactly %d inputs; got %d", circuit.NumInputs, len(payload.Inputs))
	}
	if len(payload.Outputs) != circuit.NumOutputs {
		return fmt.Errorf("transfer payload requires exactly %d outputs; got %d", circuit.NumOutputs, len(payload.Outputs))
	}
	if len(payload.CipherTextHexes) != circuit.NumOutputs {
		return fmt.Errorf("transfer payload requires exactly %d ciphertexts; got %d", circuit.NumOutputs, len(payload.CipherTextHexes))
	}
	for i, input := range payload.Inputs {
		if err := validateMerklePathHelperBits(input.MerklePathHelper); err != nil {
			return fmt.Errorf("invalid merkle path helper for input %d: %w", i, err)
		}
	}

	rootBytes, err := decodePayloadField(payload.RootHex, "root")
	if err != nil {
		return err
	}
	nullifiers, err := decodePayloadFieldList(payload.inputNullifierHexes(), "nullifier")
	if err != nil {
		return err
	}
	commitments, err := decodePayloadFieldList(payload.outputCommitmentHexes(), "commitment")
	if err != nil {
		return err
	}
	cipherTexts, err := decodeOpaqueHexList(payload.CipherTextHexes, "cipher text")
	if err != nil {
		return err
	}
	userDigest, err := decodeOptionalPayloadField(payload.UserDisclosureDigestHex, "user disclosure digest")
	if err != nil {
		return err
	}
	userTarget, err := decodeOptionalOpaqueHex(payload.UserDisclosureTargetPubKeyHex, "user disclosure target pubkey")
	if err != nil {
		return err
	}
	userPayload, err := decodeOptionalOpaqueHex(payload.UserDisclosurePayloadHex, "user disclosure payload")
	if err != nil {
		return err
	}
	auditDigest, err := decodePayloadField(payload.AuditDisclosureDigestHex, "audit disclosure digest")
	if err != nil {
		return err
	}
	auditTarget, err := decodeOpaqueHex(payload.AuditDisclosureTargetPubKeyHex, "audit disclosure target pubkey")
	if err != nil {
		return err
	}
	auditPayload, err := decodeOpaqueHex(payload.AuditDisclosurePayloadHex, "audit disclosure payload")
	if err != nil {
		return err
	}

	if err := privacytypes.NewMsgTransferWithDisclosure(
		payload.Creator,
		make([]byte, 32),
		rootBytes,
		nullifiers,
		commitments,
		cipherTexts,
		payload.UserPrivacyPolicy,
		userDigest,
		privacytypes.UserDisclosureMode(payload.UserDisclosureMode),
		userTarget,
		userPayload,
		auditDigest,
		auditTarget,
		auditPayload,
	).ValidateBasic(); err != nil {
		return err
	}
	return nil
}

func DecodePreparedTransferPayloadJSON(payloadBytes []byte) (*PreparedTransferPayload, error) {
	var payload PreparedTransferPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("invalid transfer payload JSON: %w", err)
	}
	return &payload, nil
}

func ReadPreparedTransferPayloadFile(path string) (*PreparedTransferPayload, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedTransferPayloadJSON(payloadBytes)
}

func (p PreparedTransferPayload) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func (p PreparedTransferPayload) WriteJSONFile(path string) error {
	payloadBytes, err := p.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0o600)
}

func BuildPreparedTransferProof(
	payload PreparedTransferPayload,
	artifacts JoinSplitArtifactProvider,
	runner JoinSplitProofRunner,
) (*PreparedTransferProof, error) {
	proofBytes, err := ProvePreparedTransferPayload(payload, artifacts, runner)
	if err != nil {
		return nil, err
	}

	return &PreparedTransferProof{
		Version:     PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    hex.EncodeToString(proofBytes),
	}, nil
}

func ValidatePreparedTransferProof(payload PreparedTransferPayload, proof PreparedTransferProof) error {
	if proof.Version != PreparedTransferProofVersion {
		return fmt.Errorf("unsupported transfer proof version %q (expected %q)", proof.Version, PreparedTransferProofVersion)
	}
	if err := ValidatePreparedTransferPayloadMetadata(payload); err != nil {
		return err
	}
	if proof.PayloadHash == "" || proof.PayloadHash != payload.PayloadHash {
		return fmt.Errorf("transfer proof payload hash mismatch")
	}
	if _, err := hex.DecodeString(proof.ProofHex); err != nil {
		return fmt.Errorf("invalid transfer proof hex: %w", err)
	}
	return nil
}

func DecodePreparedTransferProofJSON(payloadBytes []byte) (*PreparedTransferProof, error) {
	var proof PreparedTransferProof
	if err := json.Unmarshal(payloadBytes, &proof); err != nil {
		return nil, fmt.Errorf("invalid transfer proof JSON: %w", err)
	}
	return &proof, nil
}

func ReadPreparedTransferProofFile(path string) (*PreparedTransferProof, error) {
	proofBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedTransferProofJSON(proofBytes)
}

func (p PreparedTransferProof) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func (p PreparedTransferProof) WriteJSONFile(path string) error {
	proofBytes, err := p.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, proofBytes, 0o600)
}

func (p PreparedTransferPayload) ToMsg(proof PreparedTransferProof) (*privacytypes.MsgTransfer, error) {
	if err := ValidatePreparedTransferProof(p, proof); err != nil {
		return nil, err
	}

	proofBytes, err := hex.DecodeString(proof.ProofHex)
	if err != nil {
		return nil, fmt.Errorf("invalid transfer proof hex: %w", err)
	}

	rootBytes, err := decodePayloadField(p.RootHex, "root")
	if err != nil {
		return nil, err
	}
	nullifiers, err := decodePayloadFieldList(p.inputNullifierHexes(), "nullifier")
	if err != nil {
		return nil, err
	}
	commitments, err := decodePayloadFieldList(p.outputCommitmentHexes(), "commitment")
	if err != nil {
		return nil, err
	}
	cipherTexts, err := decodeOpaqueHexList(p.CipherTextHexes, "cipher text")
	if err != nil {
		return nil, err
	}
	userDigest, err := decodeOptionalPayloadField(p.UserDisclosureDigestHex, "user disclosure digest")
	if err != nil {
		return nil, err
	}
	userTarget, err := decodeOptionalOpaqueHex(p.UserDisclosureTargetPubKeyHex, "user disclosure target pubkey")
	if err != nil {
		return nil, err
	}
	userPayload, err := decodeOptionalOpaqueHex(p.UserDisclosurePayloadHex, "user disclosure payload")
	if err != nil {
		return nil, err
	}
	auditDigest, err := decodePayloadField(p.AuditDisclosureDigestHex, "audit disclosure digest")
	if err != nil {
		return nil, err
	}
	auditTarget, err := decodeOpaqueHex(p.AuditDisclosureTargetPubKeyHex, "audit disclosure target pubkey")
	if err != nil {
		return nil, err
	}
	auditPayload, err := decodeOpaqueHex(p.AuditDisclosurePayloadHex, "audit disclosure payload")
	if err != nil {
		return nil, err
	}

	msg := privacytypes.NewMsgTransferWithDisclosure(
		p.Creator,
		proofBytes,
		rootBytes,
		nullifiers,
		commitments,
		cipherTexts,
		p.UserPrivacyPolicy,
		userDigest,
		privacytypes.UserDisclosureMode(p.UserDisclosureMode),
		userTarget,
		userPayload,
		auditDigest,
		auditTarget,
		auditPayload,
	)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	return msg, nil
}

func ProvePreparedTransferPayload(
	payload PreparedTransferPayload,
	artifacts JoinSplitArtifactProvider,
	runner JoinSplitProofRunner,
) ([]byte, error) {
	assignment, err := buildJoinSplitAssignmentFromPreparedTransferPayload(payload)
	if err != nil {
		return nil, err
	}
	return ProveJoinSplitAssignment(assignment, artifacts, runner)
}

func buildJoinSplitAssignmentFromPreparedTransferPayload(payload PreparedTransferPayload) (*circuit.JoinSplitCircuit, error) {
	if err := ValidatePreparedTransferPayloadMetadata(payload); err != nil {
		return nil, err
	}
	if len(payload.Inputs) != circuit.NumInputs {
		return nil, fmt.Errorf("transfer payload requires exactly %d inputs; got %d", circuit.NumInputs, len(payload.Inputs))
	}
	if len(payload.Outputs) != circuit.NumOutputs {
		return nil, fmt.Errorf("transfer payload requires exactly %d outputs; got %d", circuit.NumOutputs, len(payload.Outputs))
	}

	rootBytes, err := privacyfield.DecodeCanonicalHex(payload.RootHex, "root")
	if err != nil {
		return nil, err
	}
	assetIDBytes, err := privacyfield.DecodeCanonicalHex(payload.AssetIDHex, "asset id")
	if err != nil {
		return nil, err
	}
	userDigest := big.NewInt(0)
	if strings.TrimSpace(payload.UserDisclosureDigestHex) != "" {
		userDigestBytes, err := decodePayloadField(payload.UserDisclosureDigestHex, "user disclosure digest")
		if err != nil {
			return nil, err
		}
		userDigest = new(big.Int).SetBytes(userDigestBytes)
	}
	auditDigestBytes, err := decodePayloadField(payload.AuditDisclosureDigestHex, "audit disclosure digest")
	if err != nil {
		return nil, err
	}
	auditDigest := new(big.Int).SetBytes(auditDigestBytes)

	assignment := &circuit.JoinSplitCircuit{
		MerkleRoot:            new(big.Int).SetBytes(rootBytes),
		AssetID:               new(big.Int).SetBytes(assetIDBytes),
		UserPrivacyPolicy:     big.NewInt(int64(payload.UserPrivacyPolicy)),
		UserDisclosureDigest:  userDigest,
		AuditDisclosureDigest: auditDigest,
	}

	for i, input := range payload.Inputs {
		amount, err := parseDecimalField(input.Amount, "input amount")
		if err != nil {
			return nil, err
		}
		randomness, err := decodeCanonicalHexBigInt(input.RandomnessHex, "input randomness")
		if err != nil {
			return nil, err
		}
		spendPubKey, err := decodePublicKeyHex(input.SpendPubKeyHex, "input spend pubkey")
		if err != nil {
			return nil, err
		}
		viewPubKey, err := decodePublicKeyHex(input.ViewPubKeyHex, "input view pubkey")
		if err != nil {
			return nil, err
		}
		signatureBytes, err := decodeSignatureHex(input.NoteHashSignatureHex)
		if err != nil {
			return nil, fmt.Errorf("invalid input note hash signature %d: %w", i, err)
		}
		nullifier, err := decodeCanonicalHexBigInt(input.NullifierHex, "input nullifier")
		if err != nil {
			return nil, err
		}

		assignment.InputAmounts[i] = amount
		assignment.InputRandomness[i] = randomness
		assignment.Nullifiers[i] = nullifier
		assignSignature(&assignment.InputSignatures[i], signatureBytes)
		assignPubKey(&assignment.InputSpendPubKeys[i], *spendPubKey)
		assignPubKey(&assignment.InputViewPubKeys[i], *viewPubKey)

		pathNodes, pathHelpers := decodeMerkleProof(input.MerklePath, input.MerklePathHelper)
		for depth := 0; depth < circuit.MerkleDepth; depth++ {
			assignment.InputPaths[i][depth] = pathNodes[depth]
			assignment.InputPathHelpers[i][depth] = pathHelpers[depth]
		}

		expectedNullifier := privacycrypto.MimcHash(
			randomness,
			pointAffineCoordinate(spendPubKey, true),
			pointAffineCoordinate(spendPubKey, false),
		)
		if expectedNullifier.Cmp(nullifier) != 0 {
			return nil, fmt.Errorf("input nullifier %d does not match payload witness", i)
		}
	}

	for i, output := range payload.Outputs {
		amount, err := parseDecimalField(output.Amount, "output amount")
		if err != nil {
			return nil, err
		}
		randomness, err := decodeCanonicalHexBigInt(output.RandomnessHex, "output randomness")
		if err != nil {
			return nil, err
		}
		spendPubKey, err := decodePublicKeyHex(output.SpendPubKeyHex, "output spend pubkey")
		if err != nil {
			return nil, err
		}
		viewPubKey, err := decodePublicKeyHex(output.ViewPubKeyHex, "output view pubkey")
		if err != nil {
			return nil, err
		}
		commitment, err := decodeCanonicalHexBigInt(output.CommitmentHex, "output commitment")
		if err != nil {
			return nil, err
		}

		assignment.OutputAmounts[i] = amount
		assignment.OutputRandomness[i] = randomness
		assignment.Commitments[i] = commitment
		assignPubKey(&assignment.OutputSpendPubKeys[i], *spendPubKey)
		assignPubKey(&assignment.OutputViewPubKeys[i], *viewPubKey)

		expectedCommitment := privacycrypto.MimcHash(
			pointAffineCoordinate(spendPubKey, true),
			pointAffineCoordinate(spendPubKey, false),
			pointAffineCoordinate(viewPubKey, true),
			pointAffineCoordinate(viewPubKey, false),
			amount,
			new(big.Int).SetBytes(assetIDBytes),
			randomness,
		)
		if expectedCommitment.Cmp(commitment) != 0 {
			return nil, fmt.Errorf("output commitment %d does not match payload witness", i)
		}
	}

	return assignment, nil
}

func notePubKeyHex(note privacytypes.Note, spend bool) (string, error) {
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

func hexFromCanonicalBytes(bz []byte, fieldName string) (string, error) {
	if err := privacyfield.ValidateCanonicalBytes32(bz); err != nil {
		return "", fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	return hex.EncodeToString(bz), nil
}

func parseDecimalField(value string, fieldName string) (*big.Int, error) {
	parsed, ok := new(big.Int).SetString(strings.TrimSpace(value), 10)
	if !ok {
		return nil, fmt.Errorf("invalid %s %q", fieldName, value)
	}
	if err := privacytypes.ValidateShieldedAmount(fieldName, parsed); err != nil {
		return nil, err
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

func decodePublicKeyHex(value string, fieldName string) (*crypto_tedwards.PointAffine, error) {
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

func (p PreparedTransferPayload) inputNullifierHexes() []string {
	out := make([]string, 0, len(p.Inputs))
	for _, input := range p.Inputs {
		out = append(out, input.NullifierHex)
	}
	return out
}

func (p PreparedTransferPayload) outputCommitmentHexes() []string {
	out := make([]string, 0, len(p.Outputs))
	for _, output := range p.Outputs {
		out = append(out, output.CommitmentHex)
	}
	return out
}

func decodePayloadField(value, fieldName string) ([]byte, error) {
	return privacyfield.DecodeCanonicalHex(value, fieldName)
}

func decodeOptionalPayloadField(value, fieldName string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return decodePayloadField(value, fieldName)
}

func decodePayloadFieldList(values []string, fieldName string) ([][]byte, error) {
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		bz, err := decodePayloadField(value, fieldName)
		if err != nil {
			return nil, err
		}
		out = append(out, bz)
	}
	return out, nil
}

func decodeOpaqueHex(value, fieldName string) ([]byte, error) {
	bz, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid %s hex: %w", fieldName, err)
	}
	return bz, nil
}

func decodeOptionalOpaqueHex(value, fieldName string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return decodeOpaqueHex(value, fieldName)
}

func decodeOpaqueHexList(values []string, fieldName string) ([][]byte, error) {
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		bz, err := decodeOpaqueHex(value, fieldName)
		if err != nil {
			return nil, err
		}
		out = append(out, bz)
	}
	return out, nil
}
