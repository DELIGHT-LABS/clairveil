package disclosure

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	PayloadVersion = privacytypes.FixedPayloadVersionV1
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
	BlindingHex         string `json:"disclosure_blinding_hex"`
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
	fixed, err := privacytypes.UnmarshalDisclosurePlaintextV1(payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode DisclosurePlaintextV1: %w", err)
	}
	return payloadFromFixedV1(fixed, 0)
}

func DecryptPayloadHex(cipherTextHex string, disclosureScalar *big.Int) (*Payload, error) {
	cipherText, err := hex.DecodeString(strings.TrimSpace(cipherTextHex))
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext hex: %w", err)
	}

	return DecryptPayload(cipherText, disclosureScalar)
}

func DecryptPayload(cipherText []byte, disclosureScalar *big.Int) (*Payload, error) {
	kind, rawCipherText, err := privacytypes.DecodeEncryptedEnvelopeV1(cipherText)
	if err != nil {
		return nil, fmt.Errorf("failed to decode disclosure envelope: %w", err)
	}
	if kind != privacytypes.EnvelopeUserDisclosureV1 &&
		kind != privacytypes.EnvelopeAuditDisclosureV1 &&
		kind != privacytypes.EnvelopeSelfViewDisclosureV1 {
		return nil, fmt.Errorf("encrypted envelope kind %d is not a disclosure payload", kind)
	}
	plainText, err := privacycrypto.AsymDecrypt(rawCipherText, disclosureScalar)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt disclosure payload: %w", err)
	}
	fixed, err := privacytypes.UnmarshalDisclosurePlaintextV1(plainText)
	if err != nil {
		return nil, fmt.Errorf("failed to decode DisclosurePlaintextV1: %w", err)
	}
	return payloadFromFixedV1(fixed, kind)
}

func VerifyPayload(payload *Payload, onChainDigestHex string) (*VerificationReport, error) {
	expectedDigestHex, verification, err := ComputeExpectedDisclosureDigest(payload)
	if err != nil {
		return nil, err
	}

	verification.LocalDisclosureDigestMatch = strings.TrimSpace(payload.DisclosureDigestHex) == "" || strings.EqualFold(strings.TrimSpace(payload.DisclosureDigestHex), expectedDigestHex)
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
	if payload == nil {
		return "", nil, fmt.Errorf("disclosure payload is required")
	}
	if payload.Version != PayloadVersion {
		return "", nil, fmt.Errorf("unsupported disclosure payload version %q (expected %q)", payload.Version, PayloadVersion)
	}
	if err := validatePayloadSemantics(payload); err != nil {
		return "", nil, err
	}

	commitmentBytes, err := privacyfield.DecodeCanonicalHex(payload.CommitmentHex, "commitment")
	if err != nil {
		return "", nil, err
	}
	blindingBytes, err := privacyfield.DecodeCanonicalHex(payload.BlindingHex, "disclosure blinding")
	if err != nil {
		return "", nil, err
	}
	blinding := new(big.Int).SetBytes(blindingBytes)

	amount, assetID, err := DisclosureAmountAndAsset(payload)
	if err != nil {
		return "", nil, err
	}
	if amount != nil && strings.TrimSpace(payload.AssetDenom) != "" {
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
			blinding,
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
			blinding,
		)
		return expectedDigestHex, verification, err
	case PlaneUser:
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
			blinding,
		)
		return expectedDigestHex, verification, err
	default:
		return "", nil, fmt.Errorf("unsupported disclosure payload plane %q", payload.Plane)
	}
}

func DisclosureAmountAndAsset(payload *Payload) (*big.Int, *big.Int, error) {
	if strings.TrimSpace(payload.AssetIDHex) == "" {
		return nil, nil, fmt.Errorf("disclosure payload must include asset_id_hex")
	}

	var amount *big.Int
	if strings.TrimSpace(payload.Amount) != "" {
		var err error
		amount, err = privacytypes.ParseCanonicalShieldedAmount("disclosure amount", payload.Amount)
		if err != nil {
			return nil, nil, err
		}
	}

	assetIDBytes, err := privacyfield.DecodeCanonicalHex(payload.AssetIDHex, "asset id")
	if err != nil {
		return nil, nil, err
	}
	assetID := new(big.Int).SetBytes(assetIDBytes)
	if strings.TrimSpace(payload.AssetDenom) != "" {
		expectedAssetID := privacytypes.ComputeAssetIDV1(payload.AssetDenom)
		if assetID.Cmp(expectedAssetID) != 0 {
			return nil, nil, fmt.Errorf("asset denom %q does not match asset_id_hex %s", payload.AssetDenom, payload.AssetIDHex)
		}
	}

	return amount, assetID, nil
}

func validatePayloadSemantics(payload *Payload) error {
	if payload.OutputIndex != privacytypes.TransferDisclosureRecipientOutputIndex {
		return fmt.Errorf("unsupported disclosure output_index %d (expected %d)", payload.OutputIndex, privacytypes.TransferDisclosureRecipientOutputIndex)
	}

	amountPresent := payload.Amount != ""
	assetPresent := payload.AssetIDHex != ""
	fromPresent := payload.FromShieldedAddress != ""
	toPresent := payload.ToShieldedAddress != ""

	switch payload.Plane {
	case PlaneUser:
		if payload.Policy == privacytypes.TransferPrivacyPolicyAllPrivate {
			return fmt.Errorf("user disclosure payload cannot use the all-private policy")
		}
		if err := requireDisclosureField("amount", amountPresent, payload.Policy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0); err != nil {
			return err
		}
		if !assetPresent {
			return fmt.Errorf("user disclosure payload requires asset_id_hex")
		}
		if err := requireDisclosureField("from_shielded_address", fromPresent, payload.Policy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0); err != nil {
			return err
		}
		if err := requireDisclosureField("to_shielded_address", toPresent, payload.Policy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0); err != nil {
			return err
		}
	case PlaneAudit, PlaneSelfView:
		if payload.Policy != privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
			return fmt.Errorf("%s disclosure payload must use the full disclosure policy", payload.Plane)
		}
		if !amountPresent || !fromPresent || !toPresent {
			return fmt.Errorf("%s disclosure payload must include amount, sender, and recipient fields", payload.Plane)
		}
	default:
		return fmt.Errorf("unsupported disclosure payload plane %q", payload.Plane)
	}

	return nil
}

func payloadFromFixedV1(fixed *privacytypes.DisclosurePlaintextV1, envelopeKind privacytypes.EncryptedEnvelopeKindV1) (*Payload, error) {
	if fixed == nil {
		return nil, fmt.Errorf("DisclosurePlaintextV1 is required")
	}
	payload := &Payload{
		Version:       PayloadVersion,
		OutputIndex:   fixed.OutputIndex,
		CommitmentHex: fixedFieldHex(fixed.Commitment),
		BlindingHex:   fixedFieldHex(fixed.DisclosureBlinding),
	}
	switch fixed.Plane {
	case privacytypes.DisclosurePlaneUserV1:
		if envelopeKind != 0 && envelopeKind != privacytypes.EnvelopeUserDisclosureV1 {
			return nil, fmt.Errorf("user disclosure plane does not match envelope kind %d", envelopeKind)
		}
		payload.Plane = PlaneUser
		payload.Policy = fixed.Policy
	case privacytypes.DisclosurePlaneFullV1:
		switch envelopeKind {
		case privacytypes.EnvelopeAuditDisclosureV1:
			payload.Plane = PlaneAudit
		case privacytypes.EnvelopeSelfViewDisclosureV1:
			payload.Plane = PlaneSelfView
		default:
			return nil, fmt.Errorf("full disclosure plaintext requires audit or self-view envelope kind")
		}
		payload.Policy = privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom
	default:
		return nil, fmt.Errorf("unsupported disclosure plane %d", fixed.Plane)
	}

	payload.AssetIDHex = fixedFieldHex(fixed.AssetID)
	if fixed.Plane == privacytypes.DisclosurePlaneFullV1 || fixed.Policy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0 {
		payload.Amount = fixed.Amount.String()
	}
	var err error
	if fixed.Plane == privacytypes.DisclosurePlaneFullV1 || fixed.Policy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0 {
		payload.FromShieldedAddress, err = fixedShieldedAddress(
			fixed.SenderSpendKeyX, fixed.SenderSpendKeyY, fixed.SenderViewKeyX, fixed.SenderViewKeyY,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid sender key in DisclosurePlaintextV1: %w", err)
		}
	}
	if fixed.Plane == privacytypes.DisclosurePlaneFullV1 || fixed.Policy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0 {
		payload.ToShieldedAddress, err = fixedShieldedAddress(
			fixed.RecipientSpendKeyX, fixed.RecipientSpendKeyY, fixed.RecipientViewKeyX, fixed.RecipientViewKeyY,
		)
		if err != nil {
			return nil, fmt.Errorf("invalid recipient key in DisclosurePlaintextV1: %w", err)
		}
	}
	digestHex, _, err := ComputeExpectedDisclosureDigest(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to recompute fixed disclosure digest: %w", err)
	}
	payload.DisclosureDigestHex = digestHex
	return payload, nil
}

func fixedFieldHex(value *big.Int) string {
	if value == nil {
		return ""
	}
	return hex.EncodeToString(value.FillBytes(make([]byte, 32)))
}

func fixedShieldedAddress(spendX, spendY, viewX, viewY *big.Int) (string, error) {
	var spend, view crypto_tedwards.PointAffine
	spend.X.SetBigInt(spendX)
	spend.Y.SetBigInt(spendY)
	view.X.SetBigInt(viewX)
	view.Y.SetBigInt(viewY)
	return privacytypes.EncodeShieldedAddressWithView(&spend, &view)
}

func requireDisclosureField(name string, present, required bool) error {
	if required && !present {
		return fmt.Errorf("user disclosure policy requires %s", name)
	}
	if !required && present {
		return fmt.Errorf("user disclosure policy does not authenticate %s", name)
	}
	return nil
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
