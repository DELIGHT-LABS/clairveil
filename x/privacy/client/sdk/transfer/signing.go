package transfer

import (
	"bytes"
	"fmt"
	"math/big"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
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
	InputNotes                [2]*privacytypes.Note
	RecipientOutputNote       *privacytypes.Note
	ChangeOutputNote          *privacytypes.Note
	SenderSpendPubKeyX        *big.Int
	SenderSpendPubKeyY        *big.Int
	SenderViewPubKeyX         *big.Int
	SenderViewPubKeyY         *big.Int
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
	if err := validateJoinSplitOwnerDisclosureProjectionV1(request); err != nil {
		return err
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

func validateJoinSplitOwnerDisclosureProjectionV1(request JoinSplitOwnerIntentSigningRequestV1) error {
	inputTotal := new(big.Int)
	for i, note := range request.InputNotes {
		if note == nil {
			return fmt.Errorf("transfer signing input NoteV1 %d is required", i)
		}
		if err := note.ValidateV1(); err != nil {
			return fmt.Errorf("invalid transfer signing input NoteV1 %d: %w", i, err)
		}
		if request.AssetID == nil || note.AssetID.Cmp(request.AssetID) != 0 {
			return fmt.Errorf("transfer signing input NoteV1 %d asset id does not match the owner intent", i)
		}
		if !noteUsesSenderKeys(request, note) {
			return fmt.Errorf("transfer signing input NoteV1 %d does not belong to the expected owner", i)
		}
		nullifier, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeNullifier())
		if err != nil {
			return fmt.Errorf("invalid transfer signing input nullifier %d: %w", i, err)
		}
		if len(request.Effect.Nullifiers) <= i || !bytes.Equal(nullifier, request.Effect.Nullifiers[i]) {
			return fmt.Errorf("transfer signing input NoteV1 %d does not match the final effect nullifier", i)
		}
		inputTotal.Add(inputTotal, note.Amount)
	}

	if request.RecipientOutputNote == nil {
		return fmt.Errorf("transfer signing recipient output NoteV1 is required")
	}
	if err := request.RecipientOutputNote.ValidateV1(); err != nil {
		return fmt.Errorf("invalid transfer signing recipient output NoteV1: %w", err)
	}
	if request.AssetID == nil || request.RecipientOutputNote.AssetID.Cmp(request.AssetID) != 0 {
		return fmt.Errorf("transfer signing recipient output asset id does not match the owner intent")
	}
	if request.RecipientOutputRandomness == nil || request.RecipientOutputNote.Randomness.Cmp(request.RecipientOutputRandomness) != 0 {
		return fmt.Errorf("transfer signing recipient output randomness does not match the output NoteV1")
	}

	commitment, err := privacyfield.CanonicalBytesFromBigInt(request.RecipientOutputNote.ComputeCommitment())
	if err != nil {
		return fmt.Errorf("invalid transfer signing recipient output commitment: %w", err)
	}
	if len(request.Effect.NewCommitments) <= int(privacytypes.TransferDisclosureRecipientOutputIndex) ||
		!bytes.Equal(commitment, request.Effect.NewCommitments[privacytypes.TransferDisclosureRecipientOutputIndex]) {
		return fmt.Errorf("transfer signing recipient output NoteV1 does not match the final effect commitment")
	}
	if request.ChangeOutputNote == nil {
		return fmt.Errorf("transfer signing change output NoteV1 is required")
	}
	if err := request.ChangeOutputNote.ValidateV1(); err != nil {
		return fmt.Errorf("invalid transfer signing change output NoteV1: %w", err)
	}
	if request.AssetID == nil || request.ChangeOutputNote.AssetID.Cmp(request.AssetID) != 0 {
		return fmt.Errorf("transfer signing change output asset id does not match the owner intent")
	}
	if !noteUsesSenderKeys(request, request.ChangeOutputNote) {
		return fmt.Errorf("transfer signing change output does not return to the expected owner")
	}
	changeCommitment, err := privacyfield.CanonicalBytesFromBigInt(request.ChangeOutputNote.ComputeCommitment())
	if err != nil {
		return fmt.Errorf("invalid transfer signing change output commitment: %w", err)
	}
	if len(request.Effect.NewCommitments) <= 1 || !bytes.Equal(changeCommitment, request.Effect.NewCommitments[1]) {
		return fmt.Errorf("transfer signing change output NoteV1 does not match the final effect commitment")
	}
	outputTotal := new(big.Int).Add(request.RecipientOutputNote.Amount, request.ChangeOutputNote.Amount)
	if inputTotal.Cmp(outputTotal) != 0 {
		return fmt.Errorf("transfer signing input and output amounts are not conserved")
	}

	if request.UserPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
		if len(request.Effect.UserDisclosureDigest) != 0 {
			return fmt.Errorf("transfer signing all-private effect must omit the user disclosure digest")
		}
	} else {
		expectedUserDigest, err := privacytypes.ComputeTransferDisclosureDigestBytes(
			request.UserPrivacyPolicy,
			privacytypes.TransferDisclosureRecipientOutputIndex,
			commitment,
			request.RecipientOutputNote.Amount,
			request.RecipientOutputNote.AssetID,
			request.SenderSpendPubKeyX,
			request.SenderSpendPubKeyY,
			request.SenderViewPubKeyX,
			request.SenderViewPubKeyY,
			request.RecipientOutputNote.ReceiverSpendPubKeyX,
			request.RecipientOutputNote.ReceiverSpendPubKeyY,
			request.RecipientOutputNote.ReceiverViewPubKeyX,
			request.RecipientOutputNote.ReceiverViewPubKeyY,
			request.UserDisclosureBlinding,
		)
		if err != nil {
			return fmt.Errorf("failed to rebuild transfer signing user disclosure digest: %w", err)
		}
		if !bytes.Equal(expectedUserDigest, request.Effect.UserDisclosureDigest) {
			return fmt.Errorf("transfer signing user disclosure preimage does not match the final effect")
		}
	}

	expectedFullDigest, err := privacytypes.ComputeAuditTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		commitment,
		request.RecipientOutputNote.Amount,
		request.RecipientOutputNote.AssetID,
		request.SenderSpendPubKeyX,
		request.SenderSpendPubKeyY,
		request.SenderViewPubKeyX,
		request.SenderViewPubKeyY,
		request.RecipientOutputNote.ReceiverSpendPubKeyX,
		request.RecipientOutputNote.ReceiverSpendPubKeyY,
		request.RecipientOutputNote.ReceiverViewPubKeyX,
		request.RecipientOutputNote.ReceiverViewPubKeyY,
		request.FullDisclosureBlinding,
	)
	if err != nil {
		return fmt.Errorf("failed to rebuild transfer signing audit disclosure digest: %w", err)
	}
	if !bytes.Equal(expectedFullDigest, request.Effect.AuditDisclosureDigest) {
		return fmt.Errorf("transfer signing audit disclosure preimage does not match the final effect")
	}
	return nil
}

func noteUsesSenderKeys(request JoinSplitOwnerIntentSigningRequestV1, note *privacytypes.Note) bool {
	if note == nil || request.SenderSpendPubKeyX == nil || request.SenderSpendPubKeyY == nil ||
		request.SenderViewPubKeyX == nil || request.SenderViewPubKeyY == nil {
		return false
	}
	return note.ReceiverSpendPubKeyX.Cmp(request.SenderSpendPubKeyX) == 0 &&
		note.ReceiverSpendPubKeyY.Cmp(request.SenderSpendPubKeyY) == 0 &&
		note.ReceiverViewPubKeyX.Cmp(request.SenderViewPubKeyX) == 0 &&
		note.ReceiverViewPubKeyY.Cmp(request.SenderViewPubKeyY) == 0
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
