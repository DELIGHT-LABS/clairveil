package cli

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	flagTransferPrivacyPolicy    = "privacy-policy"
	flagTransferDisclosurePubKey = "disclosure-pubkey"

	transferPrivacyPolicyAllPrivate   = "all-private"
	transferPrivacyPolicyAmount       = "amount"
	transferPrivacyPolicyTo           = "to"
	transferPrivacyPolicyAmountTo     = "amount-to"
	transferPrivacyPolicyFrom         = "from"
	transferPrivacyPolicyAmountFrom   = "amount-from"
	transferPrivacyPolicyFromTo       = "from-to"
	transferPrivacyPolicyAmountFromTo = "amount-from-to"

	transferDisclosurePayloadVersion       = privacydisclosure.PayloadVersion
	transferDisclosurePayloadPlaneUser     = privacydisclosure.PlaneUser
	transferDisclosurePayloadPlaneAudit    = privacydisclosure.PlaneAudit
	transferDisclosurePayloadPlaneSelfView = privacydisclosure.PlaneSelfView
)

type transferDisclosurePayload = privacydisclosure.Payload

type transferDisclosureData = privacytransfer.DisclosureData

func parseTransferPrivacyPolicy(raw string) (uint32, error) {
	return privacytransfer.ParsePrivacyPolicy(raw)
}

func policyLabel(policy uint32) string {
	return privacytransfer.PrivacyPolicyLabel(policy)
}

func userDisclosureModeLabel(mode types.UserDisclosureMode) string {
	return privacytransfer.UserDisclosureModeLabel(mode)
}

func decodeDisclosurePubKeyHex(value string) (*crypto_tedwards.PointAffine, []byte, error) {
	return privacytransfer.DecodeDisclosurePubKeyHex(value)
}

func decodeDisclosurePrivateKeyHex(value string) (*big.Int, error) {
	scalarHex := strings.TrimSpace(value)
	if scalarHex == "" {
		return nil, fmt.Errorf("disclosure private key hex is required")
	}

	scalarBytes, err := hex.DecodeString(scalarHex)
	if err != nil {
		return nil, fmt.Errorf("invalid disclosure private key hex: %w", err)
	}
	if len(scalarBytes) == 0 {
		return nil, fmt.Errorf("disclosure private key hex is empty")
	}

	scalar := new(big.Int).SetBytes(scalarBytes)
	if scalar.Sign() <= 0 {
		return nil, fmt.Errorf("disclosure private key must be greater than zero")
	}

	curve := crypto_tedwards.GetEdwardsCurve()
	if scalar.Cmp(&curve.Order) >= 0 {
		return nil, fmt.Errorf("disclosure private key must be smaller than the BN254 Edwards curve order")
	}

	return scalar, nil
}
