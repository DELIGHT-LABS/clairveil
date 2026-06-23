package disclosure

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	PayloadVersion = "v4"
	PlaneUser      = "user"
	PlaneAudit     = "audit"
	PlaneSelfView  = "self-view"
)

type Payload struct {
	Version             string `json:"version"`
	Plane               string `json:"plane"`
	Policy              uint32 `json:"policy"`
	OutputIndex         uint32 `json:"output_index"`
	CommitmentHex       string `json:"commitment_hex"`
	DisclosureDigestHex string `json:"disclosure_digest_hex,omitempty"`
	Amount              string `json:"amount,omitempty"`
	AssetIDHex          string `json:"asset_id_hex,omitempty"`
	AssetDenom          string `json:"asset_denom,omitempty"`
	FromShieldedAddress string `json:"from_shielded_address,omitempty"`
	ToShieldedAddress   string `json:"to_shielded_address,omitempty"`
}

type VerificationReport struct {
	Verified                     bool `json:"verified"`
	LocalDisclosureDigestMatch   bool `json:"local_disclosure_digest_match"`
	AssetDenomVerified           bool `json:"asset_denom_verified,omitempty"`
	OnChainDisclosureDigestUsed  bool `json:"on_chain_disclosure_digest_used"`
	OnChainDisclosureDigestMatch bool `json:"on_chain_disclosure_digest_match,omitempty"`
}

func DecodePublicPayloadHex(payloadHex string) (*Payload, error) {
	payloadBytes, err := hex.DecodeString(strings.TrimSpace(payloadHex))
	if err != nil {
		return nil, fmt.Errorf("invalid disclosure payload hex: %w", err)
	}

	return DecodePublicPayloadJSON(payloadBytes)
}

func DecodePublicPayloadJSON(payloadBytes []byte) (*Payload, error) {
	var payload Payload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode disclosure payload JSON: %w", err)
	}

	return &payload, nil
}

func DecryptPayloadHex(cipherTextHex string, disclosureScalar *big.Int) (*Payload, error) {
	cipherText, err := hex.DecodeString(strings.TrimSpace(cipherTextHex))
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext hex: %w", err)
	}

	return DecryptPayload(cipherText, disclosureScalar)
}

func DecryptPayload(cipherText []byte, disclosureScalar *big.Int) (*Payload, error) {
	plainText, err := privacycrypto.AsymDecrypt(cipherText, disclosureScalar)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt disclosure payload: %w", err)
	}

	return DecodePublicPayloadJSON(plainText)
}

func VerifyPayload(payload *Payload, onChainDigestHex string) (*VerificationReport, error) {
	expectedDigestHex, verification, err := ComputeExpectedDisclosureDigest(payload)
	if err != nil {
		return nil, err
	}

	verification.LocalDisclosureDigestMatch = strings.EqualFold(strings.TrimSpace(payload.DisclosureDigestHex), expectedDigestHex)
	if !verification.LocalDisclosureDigestMatch {
		return nil, fmt.Errorf("disclosure digest mismatch: payload has %s, expected %s", payload.DisclosureDigestHex, expectedDigestHex)
	}

	if strings.TrimSpace(onChainDigestHex) != "" {
		verification.OnChainDisclosureDigestUsed = true
		verification.OnChainDisclosureDigestMatch = strings.EqualFold(strings.TrimSpace(onChainDigestHex), expectedDigestHex)
		if !verification.OnChainDisclosureDigestMatch {
			return nil, fmt.Errorf("on-chain disclosure digest mismatch: event has %s, decoded payload resolves to %s", onChainDigestHex, expectedDigestHex)
		}
	}

	verification.Verified = verification.LocalDisclosureDigestMatch && (!verification.OnChainDisclosureDigestUsed || verification.OnChainDisclosureDigestMatch)
	return verification, nil
}

func ComputeExpectedDisclosureDigest(payload *Payload) (string, *VerificationReport, error) {
	verification := &VerificationReport{}

	commitmentBytes, err := privacyfield.DecodeCanonicalHex(payload.CommitmentHex, "commitment")
	if err != nil {
		return "", nil, err
	}

	amount, assetID, err := DisclosureAmountAndAsset(payload)
	if err != nil {
		return "", nil, err
	}
	if amount != nil {
		verification.AssetDenomVerified = true
	}

	fromBundle, err := disclosureShieldedAddressBundle(payload.FromShieldedAddress, "from")
	if err != nil {
		return "", nil, err
	}
	toBundle, err := disclosureShieldedAddressBundle(payload.ToShieldedAddress, "to")
	if err != nil {
		return "", nil, err
	}

	switch payload.Plane {
	case PlaneAudit:
		expectedDigestHex, err := privacytypes.ComputeAuditTransferDisclosureDigestHex(
			payload.OutputIndex,
			commitmentBytes,
			amount,
			assetID,
			bundleX(fromBundle, true),
			bundleY(fromBundle, true),
			bundleX(fromBundle, false),
			bundleY(fromBundle, false),
			bundleX(toBundle, true),
			bundleY(toBundle, true),
			bundleX(toBundle, false),
			bundleY(toBundle, false),
		)
		return expectedDigestHex, verification, err
	case PlaneSelfView:
		expectedDigestHex, err := privacytypes.ComputeSelfViewTransferDisclosureDigestHex(
			payload.OutputIndex,
			commitmentBytes,
			amount,
			assetID,
			bundleX(fromBundle, true),
			bundleY(fromBundle, true),
			bundleX(fromBundle, false),
			bundleY(fromBundle, false),
			bundleX(toBundle, true),
			bundleY(toBundle, true),
			bundleX(toBundle, false),
			bundleY(toBundle, false),
		)
		return expectedDigestHex, verification, err
	case "", PlaneUser:
		expectedDigestHex, err := privacytypes.ComputeTransferDisclosureDigestHex(
			payload.Policy,
			payload.OutputIndex,
			commitmentBytes,
			amount,
			assetID,
			bundleX(fromBundle, true),
			bundleY(fromBundle, true),
			bundleX(fromBundle, false),
			bundleY(fromBundle, false),
			bundleX(toBundle, true),
			bundleY(toBundle, true),
			bundleX(toBundle, false),
			bundleY(toBundle, false),
		)
		return expectedDigestHex, verification, err
	default:
		return "", nil, fmt.Errorf("unsupported disclosure payload plane %q", payload.Plane)
	}
}

func DisclosureAmountAndAsset(payload *Payload) (*big.Int, *big.Int, error) {
	if strings.TrimSpace(payload.Amount) == "" && strings.TrimSpace(payload.AssetIDHex) == "" && strings.TrimSpace(payload.AssetDenom) == "" {
		return nil, nil, nil
	}
	if strings.TrimSpace(payload.Amount) == "" || strings.TrimSpace(payload.AssetIDHex) == "" || strings.TrimSpace(payload.AssetDenom) == "" {
		return nil, nil, fmt.Errorf("amount disclosure payload must include amount, asset_id_hex, and asset_denom together")
	}

	amount, ok := new(big.Int).SetString(strings.TrimSpace(payload.Amount), 10)
	if !ok {
		return nil, nil, fmt.Errorf("invalid disclosure amount %q", payload.Amount)
	}

	assetIDBytes, err := privacyfield.DecodeCanonicalHex(payload.AssetIDHex, "asset id")
	if err != nil {
		return nil, nil, err
	}
	assetID := new(big.Int).SetBytes(assetIDBytes)
	expectedAssetID := privacycrypto.HashString(payload.AssetDenom)
	if assetID.Cmp(expectedAssetID) != 0 {
		return nil, nil, fmt.Errorf("asset denom %q does not match asset_id_hex %s", payload.AssetDenom, payload.AssetIDHex)
	}

	return amount, assetID, nil
}

func DisclosedFields(payload *Payload) []string {
	fields := make([]string, 0, 3)
	if payload.Amount != "" {
		fields = append(fields, "amount")
	}
	if payload.FromShieldedAddress != "" {
		fields = append(fields, "from_shielded_address")
	}
	if payload.ToShieldedAddress != "" {
		fields = append(fields, "to_shielded_address")
	}
	return fields
}

func disclosureShieldedAddressBundle(address string, label string) (*privacytypes.ShieldedAddressBundle, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, nil
	}

	bundle, err := privacytypes.DecodeShieldedAddressBundle(address)
	if err != nil {
		return nil, fmt.Errorf("invalid %s shielded address: %w", label, err)
	}
	return bundle, nil
}

func bundleX(bundle *privacytypes.ShieldedAddressBundle, spend bool) *big.Int {
	if bundle == nil {
		return nil
	}
	value := new(big.Int)
	if spend {
		bundle.SpendPubKey.X.BigInt(value)
	} else {
		bundle.ViewPubKey.X.BigInt(value)
	}
	return value
}

func bundleY(bundle *privacytypes.ShieldedAddressBundle, spend bool) *big.Int {
	if bundle == nil {
		return nil
	}
	value := new(big.Int)
	if spend {
		bundle.SpendPubKey.Y.BigInt(value)
	} else {
		bundle.ViewPubKey.Y.BigInt(value)
	}
	return value
}
