package cli

import (
	"encoding/hex"
	"fmt"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
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

func decodeDisclosurePrivateKeyHex(value string) (privacycrypto.SecretScalar, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return privacycrypto.SecretScalar{}, fmt.Errorf("invalid disclosure private key hex: %w", err)
	}
	defer clear(raw)
	return privacycrypto.ImportNonzeroScalarBE32(raw)
}
