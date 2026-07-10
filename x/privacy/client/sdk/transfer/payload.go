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
	PreparedTransferPayloadVersion = "v5"
	PreparedTransferProofVersion   = "v2"

	legacyPreparedTransferPayloadVersionV1 = "v1"
	legacyPreparedTransferPayloadVersionV2 = "v2"
	legacyPreparedTransferPayloadVersionV3 = "v3"
	legacyPreparedTransferPayloadVersionV4 = "v4"
)

type PreparedTransferInput struct {
	Amount           string   `json:"amount"`
	RandomnessHex    string   `json:"randomness_hex"`
	SpendPubKeyHex   string   `json:"spend_pubkey_hex"`
	ViewPubKeyHex    string   `json:"view_pubkey_hex"`
	MerklePath       []string `json:"merkle_path"`
	MerklePathHelper []uint32 `json:"merkle_path_helper"`
	NullifierHex     string   `json:"nullifier_hex"`
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
	ChainID                        string                   `json:"chain_id"`
	ExpiresAtUnix                  int64                    `json:"expires_at_unix"`
	RootHex                        string                   `json:"root_hex"`
	AssetIDHex                     string                   `json:"asset_id_hex"`
	Inputs                         []PreparedTransferInput  `json:"inputs"`
	Outputs                        []PreparedTransferOutput `json:"outputs"`
	CipherTextHexes                []string                 `json:"cipher_text_hexes"`
	ViewTagHexes                   []string                 `json:"view_tag_hexes"`
	UserPrivacyPolicy              uint32                   `json:"user_privacy_policy"`
	UserDisclosureMode             int32                    `json:"user_disclosure_mode"`
	UserDisclosureDigestHex        string                   `json:"user_disclosure_digest_hex,omitempty"`
	UserDisclosureTargetPubKeyHex  string                   `json:"user_disclosure_target_pubkey_hex,omitempty"`
	UserDisclosurePayloadHex       string                   `json:"user_disclosure_payload_hex,omitempty"`
	AuditDisclosureDigestHex       string                   `json:"audit_disclosure_digest_hex"`
	AuditDisclosureTargetPubKeyHex string                   `json:"audit_disclosure_target_pubkey_hex"`
	AuditDisclosurePayloadHex      string                   `json:"audit_disclosure_payload_hex"`
	SelfViewDisclosureDigestHex    string                   `json:"self_view_disclosure_digest_hex,omitempty"`
	SelfViewDisclosurePayloadHex   string                   `json:"self_view_disclosure_payload_hex,omitempty"`
	UserDisclosureBlindingHex      string                   `json:"user_disclosure_blinding_hex,omitempty"`
	FullDisclosureBlindingHex      string                   `json:"full_disclosure_blinding_hex"`
	OwnerSignatureHex              string                   `json:"owner_signature_hex"`
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
	signer OwnerIntentSigner,
	input BuildTransferMessageInput,
) (*PreparedTransferPayload, error) {
	if signer == nil {
		return nil, fmt.Errorf("an owner intent signer is required to prepare a transfer")
	}
	if input.ChainID == "" {
		return nil, fmt.Errorf("chain id is required to prepare a transfer")
	}
	if input.ExpiresAtUnix <= 0 {
		return nil, fmt.Errorf("expires_at_unix must be positive to prepare a transfer")
	}
	prepared, err := PrepareJoinSplitTransfer(
		ctx,
		merklePaths,
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

	fullDisclosureBlinding, err := privacycrypto.GenerateNonZeroRandomness()
	if err != nil {
		return nil, fmt.Errorf("failed to generate full disclosure blinding: %w", err)
	}
	userDisclosureBlinding := big.NewInt(0)
	if input.UserPrivacyPolicy != privacytypes.TransferPrivacyPolicyAllPrivate {
		userDisclosureBlinding, err = privacycrypto.GenerateNonZeroRandomness()
		if err != nil {
			return nil, fmt.Errorf("failed to generate user disclosure blinding: %w", err)
		}
	}
	disclosureInput := DisclosureBuildInput{
		OutputCommitment:       prepared.OutputCommitments[0],
		TransferDenom:          input.TransferDenom,
		FromNote:               prepared.FromNote,
		RecipientNote:          prepared.RecipientNote,
		UserDisclosureBlinding: userDisclosureBlinding,
		FullDisclosureBlinding: fullDisclosureBlinding,
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
	var selfViewDisclosureData *DisclosureData
	if !input.DisableSelfViewDisclosure {
		selfViewDisclosureData, err = BuildSelfViewDisclosureData(disclosureInput, input.SelfViewDisclosureTargetPubKey)
		if err != nil {
			return nil, err
		}
	}

	cipherTexts, viewTags, err := EncryptOutputNotesWithViewTags(prepared.RecipientNote, prepared.ChangeNote, prepared.OutputCommitments)
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
	fullDisclosureBlindingHex, err := privacyfield.CanonicalHexFromBigInt(fullDisclosureBlinding)
	if err != nil {
		return nil, fmt.Errorf("invalid full disclosure blinding: %w", err)
	}
	userDisclosureBlindingHex := ""
	if userDisclosureBlinding.Sign() != 0 {
		userDisclosureBlindingHex, err = privacyfield.CanonicalHexFromBigInt(userDisclosureBlinding)
		if err != nil {
			return nil, fmt.Errorf("invalid user disclosure blinding: %w", err)
		}
	}

	payload := &PreparedTransferPayload{
		Version:                        PreparedTransferPayloadVersion,
		Creator:                        input.Creator,
		ChainID:                        input.ChainID,
		ExpiresAtUnix:                  input.ExpiresAtUnix,
		RootHex:                        rootHex,
		AssetIDHex:                     assetIDHex,
		Inputs:                         make([]PreparedTransferInput, 0, len(input.Inputs)),
		Outputs:                        make([]PreparedTransferOutput, 0, 2),
		CipherTextHexes:                make([]string, 0, len(cipherTexts)),
		ViewTagHexes:                   make([]string, 0, len(viewTags)),
		UserPrivacyPolicy:              input.UserPrivacyPolicy,
		UserDisclosureMode:             int32(input.UserDisclosureMode),
		AuditDisclosureDigestHex:       hex.EncodeToString(auditDisclosureData.Digest),
		AuditDisclosureTargetPubKeyHex: hex.EncodeToString(encodedDisclosureTargetBytes(input.AuditDisclosureTargetPubKey, input.AuditDisclosureTargetPubKeyBz)),
		AuditDisclosurePayloadHex:      hex.EncodeToString(auditDisclosureData.CipherText),
		FullDisclosureBlindingHex:      fullDisclosureBlindingHex,
	}

	if userDisclosureData != nil {
		payload.UserDisclosureDigestHex = hex.EncodeToString(userDisclosureData.Digest)
		payload.UserDisclosureTargetPubKeyHex = hex.EncodeToString(encodedDisclosureTargetBytes(input.UserDisclosureTargetPubKey, input.UserDisclosureTargetPubKeyBz))
		payload.UserDisclosurePayloadHex = hex.EncodeToString(userDisclosureData.CipherText)
		payload.UserDisclosureBlindingHex = userDisclosureBlindingHex
	}
	if selfViewDisclosureData != nil {
		payload.SelfViewDisclosureDigestHex = hex.EncodeToString(selfViewDisclosureData.Digest)
		payload.SelfViewDisclosurePayloadHex = hex.EncodeToString(selfViewDisclosureData.CipherText)
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
			Amount:           foundNote.Note.Amount.String(),
			RandomnessHex:    randomnessHex,
			SpendPubKeyHex:   spendPubKeyHex,
			ViewPubKeyHex:    viewPubKeyHex,
			MerklePath:       append([]string(nil), prepared.InputMerklePaths[i]...),
			MerklePathHelper: append([]uint32(nil), prepared.InputPathHelpers[i]...),
			NullifierHex:     hex.EncodeToString(prepared.InputNullifiers[i]),
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
	for _, viewTag := range viewTags {
		payload.ViewTagHexes = append(payload.ViewTagHexes, hex.EncodeToString(viewTag))
	}

	effectMessage := privacytypes.NewMsgTransferWithDisclosure(
		input.Creator,
		nil,
		prepared.CommonRoot,
		prepared.InputNullifiers,
		prepared.OutputCommitments,
		cipherTexts,
		viewTags,
		input.UserPrivacyPolicy,
		digestBytes(userDisclosureData),
		input.UserDisclosureMode,
		encodedDisclosureTargetBytes(input.UserDisclosureTargetPubKey, input.UserDisclosureTargetPubKeyBz),
		cipherTextBytes(userDisclosureData),
		auditDisclosureData.Digest,
		encodedDisclosureTargetBytes(input.AuditDisclosureTargetPubKey, input.AuditDisclosureTargetPubKeyBz),
		auditDisclosureData.CipherText,
		digestBytes(selfViewDisclosureData),
		cipherTextBytes(selfViewDisclosureData),
		input.ExpiresAtUnix,
	)
	payloadDigest, err := privacytypes.ComputeTransferPayloadDigestV1(effectMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to compute canonical transfer payload digest: %w", err)
	}
	chainDomain, err := privacytypes.ComputeChainDomainV1(input.ChainID, privacytypes.ActiveCircuitSetID)
	if err != nil {
		return nil, fmt.Errorf("failed to compute transfer chain domain: %w", err)
	}
	ownerIntent, err := privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi:        chainDomain.Hi,
		ChainDomainLo:        chainDomain.Lo,
		MerkleRoot:           new(big.Int).SetBytes(prepared.CommonRoot),
		AssetID:              prepared.FromNote.AssetID,
		Nullifiers:           [2]*big.Int{new(big.Int).SetBytes(prepared.InputNullifiers[0]), new(big.Int).SetBytes(prepared.InputNullifiers[1])},
		Commitments:          [2]*big.Int{new(big.Int).SetBytes(prepared.OutputCommitments[0]), new(big.Int).SetBytes(prepared.OutputCommitments[1])},
		UserDisclosureDigest: new(big.Int).SetBytes(digestBytes(userDisclosureData)),
		FullDisclosureDigest: new(big.Int).SetBytes(auditDisclosureData.Digest),
		PayloadDigestHi:      payloadDigest.Hi,
		PayloadDigestLo:      payloadDigest.Lo,
		ExpiresAtUnix:        input.ExpiresAtUnix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute transfer owner intent: %w", err)
	}
	ownerSignature, err := signer.SignOwnerIntent(ownerIntent)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transfer owner intent: %w", err)
	}
	if _, err := privacycrypto.DecodeCanonicalEdDSASignature(ownerSignature); err != nil {
		return nil, fmt.Errorf("invalid owner intent signature: %w", err)
	}
	payload.OwnerSignatureHex = hex.EncodeToString(ownerSignature)

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
	write(payload.ChainID)
	write(strconv.FormatInt(payload.ExpiresAtUnix, 10))
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
	write(payload.SelfViewDisclosureDigestHex)
	write(payload.SelfViewDisclosurePayloadHex)
	write(payload.UserDisclosureBlindingHex)
	write(payload.FullDisclosureBlindingHex)
	write(payload.OwnerSignatureHex)
	write(strconv.Itoa(len(payload.Inputs)))
	for _, input := range payload.Inputs {
		write(input.Amount)
		write(input.RandomnessHex)
		write(input.SpendPubKeyHex)
		write(input.ViewPubKeyHex)
		writeSlice(input.MerklePath)
		writeUint32Slice(input.MerklePathHelper)
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
	writeSlice(payload.ViewTagHexes)

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func ValidatePreparedTransferPayloadMetadata(payload PreparedTransferPayload) error {
	if err := validatePreparedTransferPayloadVersion(payload); err != nil {
		return err
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
	if len(payload.ViewTagHexes) != circuit.NumOutputs {
		return fmt.Errorf("transfer payload requires exactly %d view tags; got %d", circuit.NumOutputs, len(payload.ViewTagHexes))
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
	viewTags, err := decodeViewTagHexes(payload.ViewTagHexes)
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
	selfViewDigest, err := decodeOptionalPayloadField(payload.SelfViewDisclosureDigestHex, "self-view disclosure digest")
	if err != nil {
		return err
	}
	selfViewPayload, err := decodeOptionalOpaqueHex(payload.SelfViewDisclosurePayloadHex, "self-view disclosure payload")
	if err != nil {
		return err
	}
	if payload.UserPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
		if strings.TrimSpace(payload.UserDisclosureBlindingHex) != "" {
			return fmt.Errorf("all-private transfer payload must not include user disclosure blinding")
		}
	} else if _, err := decodeNonZeroCanonicalBlindingHex(payload.UserDisclosureBlindingHex, "user disclosure blinding"); err != nil {
		return err
	}
	if _, err := decodeNonZeroCanonicalBlindingHex(payload.FullDisclosureBlindingHex, "full disclosure blinding"); err != nil {
		return err
	}
	ownerSignature, err := decodeSignatureHex(payload.OwnerSignatureHex)
	if err != nil {
		return fmt.Errorf("invalid owner intent signature: %w", err)
	}
	if _, err := privacycrypto.DecodeCanonicalEdDSASignature(ownerSignature); err != nil {
		return fmt.Errorf("invalid owner intent signature: %w", err)
	}

	effectMessage := privacytypes.NewMsgTransferWithDisclosure(
		payload.Creator,
		nil,
		rootBytes,
		nullifiers,
		commitments,
		cipherTexts,
		viewTags,
		payload.UserPrivacyPolicy,
		userDigest,
		privacytypes.UserDisclosureMode(payload.UserDisclosureMode),
		userTarget,
		userPayload,
		auditDigest,
		auditTarget,
		auditPayload,
		selfViewDigest,
		selfViewPayload,
		payload.ExpiresAtUnix,
	)
	if err := effectMessage.ValidateBasic(); err != nil {
		return err
	}
	if _, err := privacytypes.ComputeChainDomainV1(payload.ChainID, privacytypes.ActiveCircuitSetID); err != nil {
		return err
	}
	if _, err := privacytypes.ComputeTransferPayloadDigestV1(effectMessage); err != nil {
		return err
	}
	return nil
}

func validatePreparedTransferPayloadVersion(payload PreparedTransferPayload) error {
	switch payload.Version {
	case PreparedTransferPayloadVersion:
		return nil
	case legacyPreparedTransferPayloadVersionV4:
		return fmt.Errorf("legacy transfer payload version %q does not bind the final owner intent; regenerate it with transfer payload version %q", payload.Version, PreparedTransferPayloadVersion)
	case legacyPreparedTransferPayloadVersionV3:
		return fmt.Errorf("legacy transfer payload version %q does not include required disclosure blinding; regenerate it with transfer payload version %q", payload.Version, PreparedTransferPayloadVersion)
	case legacyPreparedTransferPayloadVersionV2:
		return fmt.Errorf("legacy transfer payload version %q does not include required view tags; regenerate it with transfer payload version %q", payload.Version, PreparedTransferPayloadVersion)
	case legacyPreparedTransferPayloadVersionV1:
		return fmt.Errorf("legacy transfer payload version %q does not include required self-view disclosure and view tags; regenerate it with transfer payload version %q", payload.Version, PreparedTransferPayloadVersion)
	default:
		return fmt.Errorf("unsupported transfer payload version %q (expected %q)", payload.Version, PreparedTransferPayloadVersion)
	}
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
	viewTags, err := decodeViewTagHexes(p.ViewTagHexes)
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
	selfViewDigest, err := decodeOptionalPayloadField(p.SelfViewDisclosureDigestHex, "self-view disclosure digest")
	if err != nil {
		return nil, err
	}
	selfViewPayload, err := decodeOptionalOpaqueHex(p.SelfViewDisclosurePayloadHex, "self-view disclosure payload")
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
		viewTags,
		p.UserPrivacyPolicy,
		userDigest,
		privacytypes.UserDisclosureMode(p.UserDisclosureMode),
		userTarget,
		userPayload,
		auditDigest,
		auditTarget,
		auditPayload,
		selfViewDigest,
		selfViewPayload,
		p.ExpiresAtUnix,
	)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	return msg, nil
}

func (p PreparedTransferPayload) transferEffectMessage(proofBytes []byte) (*privacytypes.MsgTransfer, error) {
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
	viewTags, err := decodeViewTagHexes(p.ViewTagHexes)
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
	selfViewDigest, err := decodeOptionalPayloadField(p.SelfViewDisclosureDigestHex, "self-view disclosure digest")
	if err != nil {
		return nil, err
	}
	selfViewPayload, err := decodeOptionalOpaqueHex(p.SelfViewDisclosurePayloadHex, "self-view disclosure payload")
	if err != nil {
		return nil, err
	}
	return privacytypes.NewMsgTransferWithDisclosure(
		p.Creator,
		proofBytes,
		rootBytes,
		nullifiers,
		commitments,
		cipherTexts,
		viewTags,
		p.UserPrivacyPolicy,
		userDigest,
		privacytypes.UserDisclosureMode(p.UserDisclosureMode),
		userTarget,
		userPayload,
		auditDigest,
		auditTarget,
		auditPayload,
		selfViewDigest,
		selfViewPayload,
		p.ExpiresAtUnix,
	), nil
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
	userDisclosureBlinding := big.NewInt(0)
	if payload.UserPrivacyPolicy != privacytypes.TransferPrivacyPolicyAllPrivate {
		userDisclosureBlinding, err = decodeNonZeroCanonicalBlindingHex(payload.UserDisclosureBlindingHex, "user disclosure blinding")
		if err != nil {
			return nil, err
		}
	}
	fullDisclosureBlinding, err := decodeNonZeroCanonicalBlindingHex(payload.FullDisclosureBlindingHex, "full disclosure blinding")
	if err != nil {
		return nil, err
	}

	assignment := &circuit.JoinSplitCircuit{
		MerkleRoot:             new(big.Int).SetBytes(rootBytes),
		AssetID:                new(big.Int).SetBytes(assetIDBytes),
		ExpiresAtUnix:          big.NewInt(payload.ExpiresAtUnix),
		UserPrivacyPolicy:      big.NewInt(int64(payload.UserPrivacyPolicy)),
		UserDisclosureDigest:   userDigest,
		FullDisclosureDigest:   auditDigest,
		UserDisclosureBlinding: userDisclosureBlinding,
		FullDisclosureBlinding: fullDisclosureBlinding,
	}
	chainDomain, err := privacytypes.ComputeChainDomainV1(payload.ChainID, privacytypes.ActiveCircuitSetID)
	if err != nil {
		return nil, err
	}
	assignment.ChainDomainHi = chainDomain.Hi
	assignment.ChainDomainLo = chainDomain.Lo
	effectMessage, err := payload.transferEffectMessage(nil)
	if err != nil {
		return nil, err
	}
	payloadDigest, err := privacytypes.ComputeTransferPayloadDigestV1(effectMessage)
	if err != nil {
		return nil, err
	}
	assignment.PayloadDigestHi = payloadDigest.Hi
	assignment.PayloadDigestLo = payloadDigest.Lo
	ownerSignatureBytes, err := decodeSignatureHex(payload.OwnerSignatureHex)
	if err != nil {
		return nil, fmt.Errorf("invalid owner intent signature: %w", err)
	}
	if err := assignSignature(&assignment.OwnerSignature, ownerSignatureBytes); err != nil {
		return nil, fmt.Errorf("invalid owner intent signature: %w", err)
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
		nullifier, err := decodeCanonicalHexBigInt(input.NullifierHex, "input nullifier")
		if err != nil {
			return nil, err
		}

		assignment.InputAmounts[i] = amount
		assignment.InputRandomness[i] = randomness
		assignment.Nullifiers[i] = nullifier
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

func digestBytes(data *DisclosureData) []byte {
	if data == nil {
		return nil
	}
	return data.Digest
}

func cipherTextBytes(data *DisclosureData) []byte {
	if data == nil {
		return nil
	}
	return data.CipherText
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
	return privacytypes.ParseCanonicalShieldedAmount(fieldName, value)
}

func decodeCanonicalHexBigInt(value, fieldName string) (*big.Int, error) {
	bz, err := privacyfield.DecodeCanonicalHex(value, fieldName)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(bz), nil
}

func decodeNonZeroCanonicalBlindingHex(value, fieldName string) (*big.Int, error) {
	blinding, err := decodeCanonicalHexBigInt(value, fieldName)
	if err != nil {
		return nil, err
	}
	if blinding.Sign() == 0 {
		return nil, fmt.Errorf("%s must be non-zero", fieldName)
	}
	return blinding, nil
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

func decodeViewTagHexes(values []string) ([][]byte, error) {
	viewTags, err := decodeOpaqueHexList(values, "view tag")
	if err != nil {
		return nil, err
	}
	for i, viewTag := range viewTags {
		if len(viewTag) != privacytypes.ViewTagLength {
			return nil, fmt.Errorf("view tag %d must be exactly %d bytes", i, privacytypes.ViewTagLength)
		}
	}
	return viewTags, nil
}
