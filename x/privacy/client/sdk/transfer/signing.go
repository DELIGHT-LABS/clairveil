package transfer

import (
	"fmt"
	"math/big"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// JoinSplitOwnerIntentSigningRequestV1 is the structured 2x2 boundary exposed
// to the shielded owner before a signature is released. The recipient output
// index and enabled state are intentionally not caller-selectable.
type JoinSplitOwnerIntentSigningRequestV1 struct {
	Intent                    *big.Int
	ChainID                   string
	Effect                    *privacytypes.MsgTransfer
	AssetID                   *big.Int
	UserPrivacyPolicy         uint32
	RecipientOutputRandomness *big.Int
	UserDisclosureBlinding    *big.Int
	FullDisclosureBlinding    *big.Int
}

type OwnerIntentSigner interface {
	SignOwnerIntent(request JoinSplitOwnerIntentSigningRequestV1) ([]byte, error)
}

// ValidateJoinSplitOwnerIntentSigningRequestV1 applies the same frozen
// DISCLOSURE-BLINDING-SEPARATION V1 relation as native/prepared/circuit code,
// then independently rebuilds the exact owner intent from the final effect.
func ValidateJoinSplitOwnerIntentSigningRequestV1(request JoinSplitOwnerIntentSigningRequestV1) error {
	if request.Effect == nil {
		return fmt.Errorf("transfer signing effect is required")
	}
	if request.Intent == nil {
		return fmt.Errorf("transfer signing intent is required")
	}
	if request.Effect.UserPrivacyPolicy != request.UserPrivacyPolicy {
		return fmt.Errorf("transfer signing privacy policy does not match the final effect")
	}
	if err := ValidateJoinSplitOwnerDisclosureBlindingV1(request); err != nil {
		return fmt.Errorf("transfer signing disclosure blinding separation: %w", err)
	}
	if err := request.Effect.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid final transfer effect: %w", err)
	}

	chainDomain, err := privacytypes.ComputeChainDomainV1(request.ChainID, privacytypes.ActiveCircuitSetID)
	if err != nil {
		return fmt.Errorf("failed to compute transfer signing chain domain: %w", err)
	}
	payloadDigest, err := privacytypes.ComputeTransferPayloadDigestV1(request.Effect)
	if err != nil {
		return fmt.Errorf("failed to compute transfer signing payload digest: %w", err)
	}
	expectedIntent, err := privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi:        chainDomain.Hi,
		ChainDomainLo:        chainDomain.Lo,
		MerkleRoot:           new(big.Int).SetBytes(request.Effect.Root),
		AssetID:              request.AssetID,
		Nullifiers:           [2]*big.Int{new(big.Int).SetBytes(request.Effect.Nullifiers[0]), new(big.Int).SetBytes(request.Effect.Nullifiers[1])},
		Commitments:          [2]*big.Int{new(big.Int).SetBytes(request.Effect.NewCommitments[0]), new(big.Int).SetBytes(request.Effect.NewCommitments[1])},
		UserDisclosureDigest: new(big.Int).SetBytes(request.Effect.UserDisclosureDigest),
		FullDisclosureDigest: new(big.Int).SetBytes(request.Effect.AuditDisclosureDigest),
		PayloadDigestHi:      payloadDigest.Hi,
		PayloadDigestLo:      payloadDigest.Lo,
		ExpiresAtUnix:        request.Effect.ExpiresAtUnix,
	})
	if err != nil {
		return fmt.Errorf("failed to compute transfer signing owner intent: %w", err)
	}
	if expectedIntent.Cmp(request.Intent) != 0 {
		return fmt.Errorf("transfer signing intent does not match the final effect")
	}
	return nil
}

// ValidateJoinSplitOwnerDisclosureBlindingV1 exposes the exact structured
// signing-layer projection of DBS-01..03 for conformance adapters. Output 0 is
// always active; disabled Batch capacity semantics cannot be selected here.
func ValidateJoinSplitOwnerDisclosureBlindingV1(request JoinSplitOwnerIntentSigningRequestV1) error {
	return privacytypes.ValidateDisclosureBlindingSeparationV1(
		privacytypes.DisclosureBlindingSeparationV1Input{
			OutputIndex:            privacytypes.TransferDisclosureRecipientOutputIndex,
			Enabled:                true,
			PrivacyPolicy:          request.UserPrivacyPolicy,
			OutputRandomness:       request.RecipientOutputRandomness,
			UserDisclosureBlinding: request.UserDisclosureBlinding,
			FullDisclosureBlinding: request.FullDisclosureBlinding,
		},
	)
}

// SignValidatedJoinSplitOwnerIntentV1 centralizes the required validation for
// production signer adapters. The callback is never invoked for an invalid
// structured request.
func SignValidatedJoinSplitOwnerIntentV1(
	request JoinSplitOwnerIntentSigningRequestV1,
	sign func(*big.Int) ([]byte, error),
) ([]byte, error) {
	if sign == nil {
		return nil, fmt.Errorf("transfer owner signing callback is required")
	}
	if err := ValidateJoinSplitOwnerIntentSigningRequestV1(request); err != nil {
		return nil, err
	}
	return sign(request.Intent)
}
