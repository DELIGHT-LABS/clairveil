package transfer

import (
	"context"
	"fmt"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	PrivacyPolicyAllPrivate   = "all-private"
	PrivacyPolicyAmount       = "amount"
	PrivacyPolicyTo           = "to"
	PrivacyPolicyAmountTo     = "amount-to"
	PrivacyPolicyFrom         = "from"
	PrivacyPolicyAmountFrom   = "amount-from"
	PrivacyPolicyFromTo       = "from-to"
	PrivacyPolicyAmountFromTo = "amount-from-to"

	DisclosureModeNone               = "none"
	DisclosureModePublic             = "public"
	DisclosureModeRecipientEncrypted = "recipient-encrypted"
)

type RuntimeConfig struct {
	UserPrivacyPolicy             uint32
	UserDisclosureMode            privacytypes.UserDisclosureMode
	UserDisclosureTargetPubKey    *crypto_tedwards.PointAffine
	UserDisclosureTargetPubKeyBz  []byte
	AuditDisclosureTargetPubKey   *crypto_tedwards.PointAffine
	AuditDisclosureTargetPubKeyBz []byte
}

type ResolveRuntimeConfigInput struct {
	RawPolicy           string
	RawDisclosureMode   string
	DisclosurePubKeyHex string
}

func ParsePrivacyPolicy(raw string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", PrivacyPolicyAllPrivate:
		return privacytypes.TransferPrivacyPolicyAllPrivate, nil
	case PrivacyPolicyAmount:
		return privacytypes.TransferPrivacyPolicyDiscloseAmount, nil
	case PrivacyPolicyTo:
		return privacytypes.TransferPrivacyPolicyDiscloseTo, nil
	case PrivacyPolicyAmountTo, "amount+to":
		return privacytypes.TransferPrivacyPolicyDiscloseAmountTo, nil
	case PrivacyPolicyFrom:
		return privacytypes.TransferPrivacyPolicyDiscloseFrom, nil
	case PrivacyPolicyAmountFrom, "amount+from":
		return privacytypes.TransferPrivacyPolicyDiscloseAmountFrom, nil
	case PrivacyPolicyFromTo, "to-from", "from+to":
		return privacytypes.TransferPrivacyPolicyDiscloseToFrom, nil
	case PrivacyPolicyAmountFromTo, "amount+from+to", "amount+to+from", "amount-to-from":
		return privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom, nil
	default:
		return 0, fmt.Errorf("unsupported privacy policy %q (supported: all-private, amount, to, amount-to, from, amount-from, from-to, amount-from-to)", raw)
	}
}

func PrivacyPolicyLabel(policy uint32) string {
	switch policy {
	case privacytypes.TransferPrivacyPolicyAllPrivate:
		return PrivacyPolicyAllPrivate
	case privacytypes.TransferPrivacyPolicyDiscloseAmount:
		return PrivacyPolicyAmount
	case privacytypes.TransferPrivacyPolicyDiscloseTo:
		return PrivacyPolicyTo
	case privacytypes.TransferPrivacyPolicyDiscloseAmountTo:
		return PrivacyPolicyAmountTo
	case privacytypes.TransferPrivacyPolicyDiscloseFrom:
		return PrivacyPolicyFrom
	case privacytypes.TransferPrivacyPolicyDiscloseAmountFrom:
		return PrivacyPolicyAmountFrom
	case privacytypes.TransferPrivacyPolicyDiscloseToFrom:
		return PrivacyPolicyFromTo
	case privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom:
		return PrivacyPolicyAmountFromTo
	default:
		return fmt.Sprintf("unknown-%d", policy)
	}
}

func ParseDisclosureMode(raw string) (privacytypes.UserDisclosureMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DisclosureModeNone:
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, nil
	case DisclosureModePublic:
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC, nil
	case DisclosureModeRecipientEncrypted:
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, nil
	default:
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, fmt.Errorf("unsupported disclosure mode %q (supported: none, public, recipient-encrypted)", raw)
	}
}

func UserDisclosureModeLabel(mode privacytypes.UserDisclosureMode) string {
	switch mode {
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
		return DisclosureModeNone
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
		return DisclosureModePublic
	case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
		return DisclosureModeRecipientEncrypted
	default:
		return fmt.Sprintf("unknown-%d", mode)
	}
}

func ResolveRuntimeConfig(
	ctx context.Context,
	auditProvider AuditDisclosureTargetProvider,
	input ResolveRuntimeConfigInput,
) (*RuntimeConfig, error) {
	userPrivacyPolicy, err := ParsePrivacyPolicy(input.RawPolicy)
	if err != nil {
		return nil, err
	}

	userDisclosureMode, err := ParseDisclosureMode(input.RawDisclosureMode)
	if err != nil {
		return nil, err
	}

	var userDisclosureTargetPubKey *crypto_tedwards.PointAffine
	var userDisclosureTargetPubKeyBz []byte

	if userPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
		if userDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE {
			return nil, fmt.Errorf("all-private transfers must use disclosure mode %q", DisclosureModeNone)
		}
		if strings.TrimSpace(input.DisclosurePubKeyHex) != "" {
			return nil, fmt.Errorf("all-private transfers must not set a disclosure pubkey")
		}
	} else {
		switch userDisclosureMode {
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
			if strings.TrimSpace(input.DisclosurePubKeyHex) != "" {
				return nil, fmt.Errorf("public disclosure must not set a disclosure pubkey")
			}
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
			pubKey, pubKeyBz, err := DecodeDisclosurePubKeyHex(input.DisclosurePubKeyHex)
			if err != nil {
				return nil, err
			}
			userDisclosureTargetPubKey = pubKey
			userDisclosureTargetPubKeyBz = pubKeyBz
		default:
			return nil, fmt.Errorf(
				"user disclosure mode %q is not valid with privacy policy %q",
				input.RawDisclosureMode,
				input.RawPolicy,
			)
		}
	}

	auditPubKey, auditPubKeyBz, err := ResolveAuditDisclosureTarget(ctx, auditProvider)
	if err != nil {
		return nil, err
	}

	return &RuntimeConfig{
		UserPrivacyPolicy:             userPrivacyPolicy,
		UserDisclosureMode:            userDisclosureMode,
		UserDisclosureTargetPubKey:    userDisclosureTargetPubKey,
		UserDisclosureTargetPubKeyBz:  userDisclosureTargetPubKeyBz,
		AuditDisclosureTargetPubKey:   auditPubKey,
		AuditDisclosureTargetPubKeyBz: auditPubKeyBz,
	}, nil
}
