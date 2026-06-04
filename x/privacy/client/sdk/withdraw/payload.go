package withdraw

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const PreparedWithdrawPayloadVersion = "v1"

type PreparedWithdrawPayload struct {
	ProofHex      string `json:"proof_hex"`
	RootHex       string `json:"root_hex"`
	NullifierHex  string `json:"nullifier_hex"`
	Amount        string `json:"amount"`
	Recipient     string `json:"recipient"`
	ChainID       string `json:"chain_id"`
	Version       string `json:"version"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
	PayloadHash   string `json:"payload_hash"`
}

type BuildPreparedWithdrawPayloadInput struct {
	ProofBytes     []byte
	RootBytes      []byte
	NullifierBytes []byte
	Amount         string
	Recipient      string
	ChainID        string
	ExpiresAtUnix  int64
}

func BuildPreparedWithdrawPayload(input BuildPreparedWithdrawPayloadInput) (*PreparedWithdrawPayload, error) {
	if err := privacyfield.ValidateCanonicalBytes32(input.RootBytes); err != nil {
		return nil, fmt.Errorf("invalid root: %w", err)
	}
	if err := privacyfield.ValidateCanonicalBytes32(input.NullifierBytes); err != nil {
		return nil, fmt.Errorf("invalid nullifier: %w", err)
	}

	payload := &PreparedWithdrawPayload{
		ProofHex:      hex.EncodeToString(input.ProofBytes),
		RootHex:       hex.EncodeToString(input.RootBytes),
		NullifierHex:  hex.EncodeToString(input.NullifierBytes),
		Amount:        input.Amount,
		Recipient:     input.Recipient,
		ChainID:       input.ChainID,
		Version:       PreparedWithdrawPayloadVersion,
		ExpiresAtUnix: input.ExpiresAtUnix,
	}
	payload.PayloadHash = ComputePreparedWithdrawPayloadHash(
		payload.ProofHex,
		payload.RootHex,
		payload.NullifierHex,
		payload.Amount,
		payload.Recipient,
		payload.ChainID,
		payload.Version,
		payload.ExpiresAtUnix,
	)

	return payload, nil
}

func ComputePreparedWithdrawPayloadHash(
	proofHex string,
	rootHex string,
	nullifierHex string,
	amount string,
	recipient string,
	chainID string,
	version string,
	expiresAtUnix int64,
) string {
	source := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n%s\n%d", version, proofHex, rootHex, nullifierHex, amount, recipient, chainID, expiresAtUnix)
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func ValidatePreparedWithdrawPayloadMetadata(payload PreparedWithdrawPayload, now time.Time) error {
	if payload.Version != PreparedWithdrawPayloadVersion {
		return fmt.Errorf("unsupported withdraw payload version %q (expected %q); regenerate it with prepare-withdraw", payload.Version, PreparedWithdrawPayloadVersion)
	}

	if payload.ChainID == "" {
		return fmt.Errorf("withdraw payload chain_id is required")
	}

	if payload.ExpiresAtUnix <= 0 {
		return fmt.Errorf("withdraw payload expires_at_unix must be positive")
	}

	expectedHash := ComputePreparedWithdrawPayloadHash(
		payload.ProofHex,
		payload.RootHex,
		payload.NullifierHex,
		payload.Amount,
		payload.Recipient,
		payload.ChainID,
		payload.Version,
		payload.ExpiresAtUnix,
	)
	if payload.PayloadHash == "" || payload.PayloadHash != expectedHash {
		return fmt.Errorf("withdraw payload hash mismatch; the file may have been modified after prepare-withdraw")
	}

	if now.Unix() > payload.ExpiresAtUnix {
		return fmt.Errorf("withdraw payload expired; generate a fresh payload with prepare-withdraw")
	}

	return nil
}

func DecodePreparedWithdrawPayloadJSON(payloadBytes []byte) (*PreparedWithdrawPayload, error) {
	var payload PreparedWithdrawPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("invalid withdraw payload JSON: %w", err)
	}
	return &payload, nil
}

func ReadPreparedWithdrawPayloadFile(path string) (*PreparedWithdrawPayload, error) {
	payloadBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePreparedWithdrawPayloadJSON(payloadBytes)
}

func (p PreparedWithdrawPayload) MarshalIndentedJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

func (p PreparedWithdrawPayload) WriteJSONFile(path string) error {
	payloadBytes, err := p.MarshalIndentedJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, payloadBytes, 0600)
}

func (p PreparedWithdrawPayload) ToMsg(creator string) (*privacytypes.MsgWithdraw, error) {
	if err := ValidatePreparedWithdrawPayloadMetadata(p, time.Now()); err != nil {
		return nil, err
	}

	proofBz, err := hex.DecodeString(p.ProofHex)
	if err != nil {
		return nil, fmt.Errorf("invalid proof hex: %w", err)
	}

	rootBz, err := privacyfield.DecodeCanonicalHex(p.RootHex, "root")
	if err != nil {
		return nil, err
	}

	nullifierBz, err := privacyfield.DecodeCanonicalHex(p.NullifierHex, "nullifier")
	if err != nil {
		return nil, err
	}

	if _, err := sdk.AccAddressFromBech32(p.Recipient); err != nil {
		return nil, fmt.Errorf("invalid recipient: %w", err)
	}

	return privacytypes.NewMsgWithdraw(
		creator,
		proofBz,
		rootBz,
		nullifierBz,
		p.Amount,
		p.Recipient,
		p.ChainID,
		p.ExpiresAtUnix,
	), nil
}

func BuildRelayWithdrawMsgFromJSON(payloadBytes []byte, creator string) (*privacytypes.MsgWithdraw, error) {
	payload, err := DecodePreparedWithdrawPayloadJSON(payloadBytes)
	if err != nil {
		return nil, err
	}
	return payload.ToMsg(creator)
}

func BuildRelayWithdrawMsgFromFile(path string, creator string) (*privacytypes.MsgWithdraw, error) {
	payload, err := ReadPreparedWithdrawPayloadFile(path)
	if err != nil {
		return nil, err
	}
	return payload.ToMsg(creator)
}
