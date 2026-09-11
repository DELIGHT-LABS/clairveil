package batchtransfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

// PreparedAuditV2BatchTransferPayload is the resumable part of a v2 batch
// transfer. It intentionally contains no retired AuditConfig fields, audit
// recipient key, or legacy audit ciphertext. The ephemeral audit witness is
// constructed and cleared only by the prove stage.
const PreparedAuditV2BatchTransferPayloadVersion = "batch-transfer-audit-v2-payload-v1"
const PreparedAuditV2BatchTransferProofVersion = "batch-transfer-audit-v2-proof-v1"

type PreparedAuditV2BatchTransferPayload struct {
	Version       string                        `json:"version"`
	Creator       string                        `json:"creator,omitempty"`
	ChainID       string                        `json:"chain_id"`
	ExpiresAtUnix int64                         `json:"expires_at_unix"`
	Root          []byte                        `json:"root"`
	AssetID       *big.Int                      `json:"asset_id"`
	Inputs        []PreparedBatchTransferInput  `json:"inputs"`
	Outputs       []PreparedBatchTransferOutput `json:"outputs"`
	Effects       []*privacyv2.OutputEffect     `json:"effects"`
	PayloadHash   string                        `json:"payload_hash"`
}

// PreparedAuditV2BatchTransferProof stores only the verified final message
// and public binding necessary for relay/broadcast. In particular it never
// serializes r or the full proving witness.
type PreparedAuditV2BatchTransferProof struct {
	Version            string                      `json:"version"`
	RequestPayloadHash string                      `json:"request_payload_hash"`
	Message            *privacyv2.MsgBatchTransfer `json:"message"`
	PublicInputs       [][]byte                    `json:"public_inputs"`
	ArtifactHash       []byte                      `json:"artifact_hash"`
}

type BuildPreparedAuditV2BatchTransferPayloadInput struct {
	Creator                        string
	ChainID                        string
	ExpiresAtUnix                  int64
	SelfViewDisclosureTargetPubKey *crypto_tedwards.PointAffine
	DisableSelfViewDisclosure      bool
}

// BuildPreparedAuditV2BatchTransferPayload reuses normal note recovery and
// user/self disclosure construction while deliberately omitting the retired
// audit disclosure plane.
func BuildPreparedAuditV2BatchTransferPayload(prepared *PreparedBatchTransfer, input BuildPreparedAuditV2BatchTransferPayloadInput) (*PreparedAuditV2BatchTransferPayload, error) {
	if err := validatePreparedBatchTransferForPayloadBuild(prepared); err != nil {
		return nil, err
	}
	if input.ChainID == "" || input.ExpiresAtUnix <= time.Now().Unix() {
		return nil, fmt.Errorf("chain id and future expiry are required")
	}
	p := &PreparedAuditV2BatchTransferPayload{Version: PreparedAuditV2BatchTransferPayloadVersion, Creator: input.Creator, ChainID: input.ChainID, ExpiresAtUnix: input.ExpiresAtUnix, Root: bytes.Clone(prepared.Root), AssetID: new(big.Int).Set(prepared.AssetID), Inputs: prepared.Inputs, Outputs: prepared.Outputs, Effects: make([]*privacyv2.OutputEffect, len(prepared.Outputs))}
	owner := p.Inputs[0].Note
	for i, output := range p.Outputs {
		commitment, err := output.Note.CommitmentV1()
		if err != nil {
			return nil, err
		}
		commitmentBytes := fixedBytes(commitment)
		notePlaintext, err := privacytypes.MarshalSecretNotePlaintextV1(&output.Note)
		if err != nil {
			return nil, err
		}
		viewKey, err := fixedPoint(output.Note.ReceiverViewPubKeyX, output.Note.ReceiverViewPubKeyY)
		if err != nil {
			clear(notePlaintext)
			return nil, err
		}
		raw, tag, err := privacycrypto.AsymEncryptWithViewTag(notePlaintext, *viewKey, commitmentBytes, uint32(i))
		clear(notePlaintext)
		if err != nil {
			return nil, err
		}
		ciphertext, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, raw)
		if err != nil {
			return nil, err
		}
		userPlain, userDigest, err := disclosurePlaintext(uint32(i), false, owner, output)
		if err != nil {
			return nil, err
		}
		fullPlain, fullDigest, err := disclosurePlaintext(uint32(i), true, owner, output)
		if err != nil {
			clear(userPlain)
			return nil, err
		}
		effect := &privacyv2.OutputEffect{Commitment: commitmentBytes, Ciphertext: ciphertext, ViewTag: tag, UserPrivacyPolicy: output.PrivacyPolicy, UserDisclosureMode: uint32(output.DisclosureMode), SelfFullDisclosureDigest: fieldBytes(fullDigest)}
		if !input.DisableSelfViewDisclosure {
			if input.SelfViewDisclosureTargetPubKey == nil {
				clear(userPlain)
				clear(fullPlain)
				return nil, fmt.Errorf("self-view target is required unless self-view is disabled")
			}
			selfRaw, err := privacycrypto.AsymEncrypt(fullPlain, *input.SelfViewDisclosureTargetPubKey)
			if err != nil {
				clear(userPlain)
				clear(fullPlain)
				return nil, err
			}
			effect.SelfViewDisclosurePayload, err = privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeSelfViewDisclosureV1, selfRaw)
			if err != nil {
				clear(userPlain)
				clear(fullPlain)
				return nil, err
			}
		}
		clear(fullPlain)
		if output.PrivacyPolicy != 0 {
			effect.UserDisclosureDigest = fieldBytes(userDigest)
			switch output.DisclosureMode {
			case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
				effect.UserDisclosurePayload = userPlain
			case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
				target, err := privacycrypto.DecodeCanonicalPoint(output.DisclosureTargetPubKey)
				if err != nil {
					clear(userPlain)
					return nil, err
				}
				userRaw, err := privacycrypto.AsymEncrypt(userPlain, *target)
				clear(userPlain)
				if err != nil {
					return nil, err
				}
				effect.UserDisclosurePayload, err = privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeUserDisclosureV1, userRaw)
				if err != nil {
					return nil, err
				}
				effect.UserDisclosureTargetPubkey = bytes.Clone(output.DisclosureTargetPubKey)
			default:
				clear(userPlain)
				return nil, fmt.Errorf("output %d disclosure mode is invalid", i)
			}
		} else {
			clear(userPlain)
		}
		p.Effects[i] = effect
	}
	var err error
	p.PayloadHash, err = computeAuditV2BatchPayloadHash(p)
	if err != nil {
		return nil, err
	}
	return p, ValidatePreparedAuditV2BatchTransferPayloadAt(p, time.Now())
}

func computeAuditV2BatchPayloadHash(p *PreparedAuditV2BatchTransferPayload) (string, error) {
	clone := *p
	clone.PayloadHash = ""
	clone.Creator = ""
	bz, err := json.Marshal(&clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("clairveil.prepared-batch-transfer.audit-v2.v1"), bz...))
	return hex.EncodeToString(sum[:]), nil
}

func ValidatePreparedAuditV2BatchTransferPayloadAt(p *PreparedAuditV2BatchTransferPayload, now time.Time) error {
	if p == nil || p.Version != PreparedAuditV2BatchTransferPayloadVersion {
		return fmt.Errorf("unsupported prepared v2 batch payload")
	}
	if p.ChainID == "" || (!now.IsZero() && p.ExpiresAtUnix <= now.Unix()) {
		return fmt.Errorf("prepared v2 batch payload has expired or no chain id")
	}
	if err := privacytypes.ValidateBatchJoinSplitCountsV1(uint32(len(p.Inputs)), uint32(len(p.Outputs))); err != nil {
		return err
	}
	if len(p.Effects) != len(p.Outputs) || privacyfield.ValidateCanonicalBytes32(p.Root) != nil || p.AssetID == nil || p.AssetID.Sign() <= 0 {
		return fmt.Errorf("invalid prepared v2 batch payload shape")
	}
	for i, in := range p.Inputs {
		if err := in.Note.ValidateV1(); err != nil {
			return fmt.Errorf("input %d: %w", i, err)
		}
		if len(in.MerklePath) != 32 || len(in.MerklePathHelper) != 32 || len(in.Nullifier) != 32 {
			return fmt.Errorf("input %d path or nullifier malformed", i)
		}
	}
	for i, out := range p.Outputs {
		if err := out.Note.ValidateV1(); err != nil {
			return fmt.Errorf("output %d: %w", i, err)
		}
		if p.Effects[i] == nil {
			return fmt.Errorf("output %d effect is required", i)
		}
		c, err := out.Note.CommitmentV1()
		if err != nil || !bytes.Equal(fixedBytes(c), p.Effects[i].Commitment) {
			return fmt.Errorf("output %d effect commitment mismatch", i)
		}
	}
	if _, _, err := privacytypes.AuditOutputsToAux(4, p.Effects); err != nil {
		return err
	}
	want, err := computeAuditV2BatchPayloadHash(p)
	if err != nil {
		return err
	}
	if want != p.PayloadHash {
		return fmt.Errorf("prepared v2 batch payload hash mismatch")
	}
	return nil
}

func WritePreparedAuditV2BatchTransferPayload(path string, payload *PreparedAuditV2BatchTransferPayload) error {
	bz, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBatchFile(path, bz)
}
func ReadPreparedAuditV2BatchTransferPayload(path string) (*PreparedAuditV2BatchTransferPayload, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var header struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(bz, &header); err != nil {
		return nil, fmt.Errorf("invalid prepared v2 batch payload JSON: %w", err)
	}
	if header.Version != PreparedAuditV2BatchTransferPayloadVersion {
		return nil, fmt.Errorf("unsupported prepared batch payload version %q; regenerate with prepare-batch-transfer", header.Version)
	}
	var p PreparedAuditV2BatchTransferPayload
	if err := decodeStrictBatchJSON(bz, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
func WritePreparedAuditV2BatchTransferProof(path string, proof *PreparedAuditV2BatchTransferProof) error {
	bz, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBatchFile(path, bz)
}
func ReadPreparedAuditV2BatchTransferProof(path string) (*PreparedAuditV2BatchTransferProof, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var header struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(bz, &header); err != nil {
		return nil, fmt.Errorf("invalid prepared v2 batch proof JSON: %w", err)
	}
	if header.Version != PreparedAuditV2BatchTransferProofVersion {
		return nil, fmt.Errorf("unsupported prepared batch proof version %q; prove again", header.Version)
	}
	var p PreparedAuditV2BatchTransferProof
	if err := decodeStrictBatchJSON(bz, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
