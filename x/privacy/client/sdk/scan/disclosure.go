package scan

import (
	"bytes"
	"fmt"
	"math/big"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type DisclosureVerificationStatus string

const (
	DisclosureNotPresent     DisclosureVerificationStatus = "NotPresent"
	DisclosureNotAttempted   DisclosureVerificationStatus = "NotAttempted"
	DisclosureVerified       DisclosureVerificationStatus = "Verified"
	DisclosureDecryptFailed  DisclosureVerificationStatus = "DecryptFailed"
	DisclosureDigestMismatch DisclosureVerificationStatus = "DigestMismatch"
	DisclosureInvalid        DisclosureVerificationStatus = "Invalid"
)

type DisclosureKeySet struct {
	UserRecipient *big.Int
	Audit         *big.Int
	SelfView      *big.Int
}

type DisclosurePlaneEvidence struct {
	Status    DisclosureVerificationStatus        `json:"status"`
	Reason    string                              `json:"reason,omitempty"`
	Plaintext *privacytypes.DisclosurePlaintextV1 `json:"-"`
}

type PrivacyScanDisclosureEvidence struct {
	User                   DisclosurePlaneEvidence `json:"user"`
	Audit                  DisclosurePlaneEvidence `json:"audit"`
	SelfView               DisclosurePlaneEvidence `json:"self_view"`
	AuditDeliveryFailed    bool                    `json:"audit_delivery_failed"`
	SelfViewDeliveryFailed bool                    `json:"self_view_delivery_failed"`
	ManualReview           bool                    `json:"manual_review"`
	ManualReviewReason     string                  `json:"manual_review_reason,omitempty"`
}

// VerifyPrivacyScanDisclosures validates the output-bound plaintext metadata
// and recomputes each per-output digest using the disclosed blinding.
func VerifyPrivacyScanDisclosures(output *privacytypes.PrivacyScanOutputV2, keys DisclosureKeySet) PrivacyScanDisclosureEvidence {
	result := PrivacyScanDisclosureEvidence{
		User:     DisclosurePlaneEvidence{Status: DisclosureNotPresent},
		Audit:    DisclosurePlaneEvidence{Status: DisclosureNotPresent},
		SelfView: DisclosurePlaneEvidence{Status: DisclosureNotPresent},
	}
	if output == nil {
		result.ManualReview = true
		result.ManualReviewReason = "privacy scan output is absent"
		return result
	}

	if output.UserPrivacyPolicy != privacytypes.TransferPrivacyPolicyAllPrivate {
		switch output.UserDisclosureMode {
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String():
			result.User = verifyDisclosurePlaintext(output, output.UserDisclosurePayload, privacytypes.DisclosurePlaneUserV1, output.UserDisclosureDigest)
		case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED.String():
			result.User = decryptAndVerifyDisclosure(output, output.UserDisclosurePayload, privacytypes.EnvelopeUserDisclosureV1, keys.UserRecipient, privacytypes.DisclosurePlaneUserV1, output.UserDisclosureDigest)
		default:
			result.User = DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: "invalid user disclosure mode"}
		}
	} else if output.UserDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE.String() || len(output.UserDisclosureDigest) != 0 || len(output.UserDisclosureTargetPubkey) != 0 || len(output.UserDisclosurePayload) != 0 {
		result.User = DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: "all-private user disclosure framing mismatch"}
	}
	result.Audit = decryptAndVerifyDisclosure(output, output.AuditDisclosurePayload, privacytypes.EnvelopeAuditDisclosureV1, keys.Audit, privacytypes.DisclosurePlaneFullV1, output.FullDisclosureDigest)
	if len(output.SelfViewDisclosurePayload) != 0 {
		result.SelfView = decryptAndVerifyDisclosure(output, output.SelfViewDisclosurePayload, privacytypes.EnvelopeSelfViewDisclosureV1, keys.SelfView, privacytypes.DisclosurePlaneFullV1, output.FullDisclosureDigest)
	}
	if result.Audit.Status == DisclosureNotPresent {
		result.ManualReview = true
		result.ManualReviewReason = "mandatory audit disclosure is absent"
	} else if result.Audit.Status == DisclosureDecryptFailed {
		result.AuditDeliveryFailed = true
		result.ManualReview = true
		result.ManualReviewReason = "AuditDeliveryFailed"
	} else if result.Audit.Status == DisclosureDigestMismatch || result.Audit.Status == DisclosureInvalid {
		result.ManualReview = true
		result.ManualReviewReason = string(result.Audit.Status)
	}
	if result.User.Status == DisclosureDecryptFailed || result.User.Status == DisclosureDigestMismatch || result.User.Status == DisclosureInvalid {
		result.ManualReview = true
		if result.ManualReviewReason == "" {
			result.ManualReviewReason = "user disclosure " + string(result.User.Status)
		}
	}
	if result.SelfView.Status == DisclosureDecryptFailed {
		result.SelfViewDeliveryFailed = true
		result.ManualReview = true
		if result.ManualReviewReason == "" {
			result.ManualReviewReason = "self-view disclosure delivery failed"
		}
	} else if result.SelfView.Status == DisclosureDigestMismatch || result.SelfView.Status == DisclosureInvalid {
		result.ManualReview = true
		if result.ManualReviewReason == "" {
			result.ManualReviewReason = "self-view disclosure " + string(result.SelfView.Status)
		}
	}
	return result
}

func decryptAndVerifyDisclosure(output *privacytypes.PrivacyScanOutputV2, envelope []byte, kind privacytypes.EncryptedEnvelopeKindV1, scalar *big.Int, plane privacytypes.DisclosurePlaneV1, digest []byte) DisclosurePlaneEvidence {
	if len(envelope) == 0 {
		return DisclosurePlaneEvidence{Status: DisclosureNotPresent}
	}
	if scalar == nil {
		return DisclosurePlaneEvidence{Status: DisclosureNotAttempted, Reason: "decryption key is unavailable"}
	}
	raw, err := privacytypes.UnwrapEncryptedEnvelopeV1(envelope, kind)
	if err != nil {
		return DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: err.Error()}
	}
	plaintext, err := privacycrypto.AsymDecrypt(raw, scalar)
	if err != nil {
		return DisclosurePlaneEvidence{Status: DisclosureDecryptFailed, Reason: err.Error()}
	}
	return verifyDisclosurePlaintext(output, plaintext, plane, digest)
}

func verifyDisclosurePlaintext(output *privacytypes.PrivacyScanOutputV2, encoded []byte, plane privacytypes.DisclosurePlaneV1, digest []byte) DisclosurePlaneEvidence {
	plaintext, err := privacytypes.UnmarshalDisclosurePlaintextV1(encoded)
	if err != nil {
		return DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: err.Error()}
	}
	commitment := new(big.Int).SetBytes(output.Commitment)
	if plaintext.Plane != plane || plaintext.OutputIndex != output.OutputIndex || plaintext.Commitment.Cmp(commitment) != 0 {
		return DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: "disclosure index/commitment/plane mismatch"}
	}
	var expected *big.Int
	if plane == privacytypes.DisclosurePlaneUserV1 {
		if plaintext.Policy != output.UserPrivacyPolicy || plaintext.DisclosedFieldBitmap != output.UserPrivacyPolicy {
			return DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: "disclosure policy mismatch"}
		}
		expected, err = privacytypes.ComputeBatchUserDisclosureDigestV1(privacytypes.BatchUserDisclosureV1Input{
			OutputIndex: plaintext.OutputIndex, Commitment: plaintext.Commitment, Policy: plaintext.Policy, DisclosedFieldBitmap: plaintext.DisclosedFieldBitmap,
			SelectedAmount: plaintext.Amount, AssetID: plaintext.AssetID,
			SelectedFromSpendKeyX: plaintext.SenderSpendKeyX, SelectedFromSpendKeyY: plaintext.SenderSpendKeyY, SelectedFromViewKeyX: plaintext.SenderViewKeyX, SelectedFromViewKeyY: plaintext.SenderViewKeyY,
			SelectedToSpendKeyX: plaintext.RecipientSpendKeyX, SelectedToSpendKeyY: plaintext.RecipientSpendKeyY, SelectedToViewKeyX: plaintext.RecipientViewKeyX, SelectedToViewKeyY: plaintext.RecipientViewKeyY,
			UserDisclosureBlinding: plaintext.DisclosureBlinding,
		})
	} else {
		if plaintext.Policy != privacytypes.DisclosureFullMarkerV1 || plaintext.DisclosedFieldBitmap != privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
			return DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: "full disclosure policy mismatch"}
		}
		expected, err = privacytypes.ComputeBatchFullDisclosureDigestV1(privacytypes.BatchFullDisclosureV1Input{
			OutputIndex: plaintext.OutputIndex, Commitment: plaintext.Commitment, Amount: plaintext.Amount, AssetID: plaintext.AssetID,
			SenderSpendKeyX: plaintext.SenderSpendKeyX, SenderSpendKeyY: plaintext.SenderSpendKeyY, SenderViewKeyX: plaintext.SenderViewKeyX, SenderViewKeyY: plaintext.SenderViewKeyY,
			RecipientSpendKeyX: plaintext.RecipientSpendKeyX, RecipientSpendKeyY: plaintext.RecipientSpendKeyY, RecipientViewKeyX: plaintext.RecipientViewKeyX, RecipientViewKeyY: plaintext.RecipientViewKeyY,
			FullDisclosureBlinding: plaintext.DisclosureBlinding,
		})
	}
	if err != nil {
		return DisclosurePlaneEvidence{Status: DisclosureInvalid, Reason: err.Error()}
	}
	if len(digest) != 32 || !bytes.Equal(expected.FillBytes(make([]byte, 32)), digest) {
		return DisclosurePlaneEvidence{Status: DisclosureDigestMismatch, Reason: fmt.Sprintf("%s digest mismatch", planeName(plane)), Plaintext: plaintext}
	}
	return DisclosurePlaneEvidence{Status: DisclosureVerified, Plaintext: plaintext}
}

func planeName(plane privacytypes.DisclosurePlaneV1) string {
	if plane == privacytypes.DisclosurePlaneUserV1 {
		return "user"
	}
	return "full"
}
