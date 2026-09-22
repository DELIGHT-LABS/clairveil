package transfer

import (
	"bytes"
	"fmt"
	"math/big"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// JoinSplitOwnerIntentSigningRequestV1 is the structured 2x2 boundary exposed
// to the shielded owner before a signature is released. The recipient output
// index and enabled state are intentionally not caller-selectable.
type JoinSplitOwnerIntentSigningRequestV1 struct {
	Intent                    *big.Int
	ChainID                   string
	Effect                    *privacytypes.MsgTransfer
	AssetID                   [32]byte
	UserPrivacyPolicy         uint32
	RecipientOutputRandomness privacycrypto.FieldValue
	UserDisclosureBlinding    privacycrypto.FieldValue
	FullDisclosureBlinding    privacycrypto.FieldValue
	InputNotes                [2]*privacytypes.SecretNoteV1
	RecipientOutputNote       *privacytypes.SecretNoteV1
	ChangeOutputNote          *privacytypes.SecretNoteV1
	SenderSpendPubKeyX        privacycrypto.FieldValue
	SenderSpendPubKeyY        privacycrypto.FieldValue
	SenderViewPubKeyX         privacycrypto.FieldValue
	SenderViewPubKeyY         privacycrypto.FieldValue
}

type OwnerIntentSigner interface {
	SignOwnerIntent(request JoinSplitOwnerIntentSigningRequestV1) ([]byte, error)
}

// ValidateJoinSplitOwnerIntentSigningRequestV1 reconstructs every secret
// relation bound by the final public effect before releasing an owner
// signature. Private note fields remain fixed-field values throughout.
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
	expected, err := privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi:        chainDomain.Hi,
		ChainDomainLo:        chainDomain.Lo,
		MerkleRoot:           new(big.Int).SetBytes(request.Effect.Root),
		AssetID:              new(big.Int).SetBytes(request.AssetID[:]),
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
	if expected.Cmp(request.Intent) != 0 {
		return fmt.Errorf("transfer signing intent does not match the final effect")
	}
	return nil
}

func validateJoinSplitOwnerDisclosureProjectionV1(request JoinSplitOwnerIntentSigningRequestV1) error {
	for i, note := range request.InputNotes {
		if note == nil {
			return fmt.Errorf("transfer signing input SecretNoteV1 %d is required", i)
		}
		if err := note.ValidateV1(); err != nil {
			return fmt.Errorf("invalid transfer signing input SecretNoteV1 %d: %w", i, err)
		}
		if !noteAssetMatchesSigningAsset(*note, request.AssetID) {
			return fmt.Errorf("transfer signing input SecretNoteV1 %d asset id does not match the owner intent", i)
		}
		if !secretNoteUsesSenderKeys(request, *note) {
			return fmt.Errorf("transfer signing input SecretNoteV1 %d does not belong to the expected owner", i)
		}
		nullifier, err := note.NullifierV1()
		if err != nil {
			return fmt.Errorf("invalid transfer signing input nullifier %d: %w", i, err)
		}
		if len(request.Effect.Nullifiers) <= i || !bytes.Equal(fieldValueBytes(nullifier), request.Effect.Nullifiers[i]) {
			return fmt.Errorf("transfer signing input SecretNoteV1 %d does not match the final effect nullifier", i)
		}
	}

	if request.RecipientOutputNote == nil {
		return fmt.Errorf("transfer signing recipient output SecretNoteV1 is required")
	}
	if err := request.RecipientOutputNote.ValidateV1(); err != nil {
		return fmt.Errorf("invalid transfer signing recipient output SecretNoteV1: %w", err)
	}
	if !noteAssetMatchesSigningAsset(*request.RecipientOutputNote, request.AssetID) {
		return fmt.Errorf("transfer signing recipient output asset id does not match the owner intent")
	}
	if !request.RecipientOutputNote.Randomness.Equal(request.RecipientOutputRandomness) {
		return fmt.Errorf("transfer signing recipient output randomness does not match the output SecretNoteV1")
	}
	recipientCommitment, err := request.RecipientOutputNote.CommitmentV1()
	if err != nil {
		return fmt.Errorf("invalid transfer signing recipient output commitment: %w", err)
	}
	if len(request.Effect.NewCommitments) <= int(privacytypes.TransferDisclosureRecipientOutputIndex) ||
		!bytes.Equal(fieldValueBytes(recipientCommitment), request.Effect.NewCommitments[privacytypes.TransferDisclosureRecipientOutputIndex]) {
		return fmt.Errorf("transfer signing recipient output SecretNoteV1 does not match the final effect commitment")
	}

	if request.ChangeOutputNote == nil {
		return fmt.Errorf("transfer signing change output SecretNoteV1 is required")
	}
	if err := request.ChangeOutputNote.ValidateV1(); err != nil {
		return fmt.Errorf("invalid transfer signing change output SecretNoteV1: %w", err)
	}
	if !noteAssetMatchesSigningAsset(*request.ChangeOutputNote, request.AssetID) {
		return fmt.Errorf("transfer signing change output asset id does not match the owner intent")
	}
	if !secretNoteUsesSenderKeys(request, *request.ChangeOutputNote) {
		return fmt.Errorf("transfer signing change output does not return to the expected owner")
	}
	changeCommitment, err := request.ChangeOutputNote.CommitmentV1()
	if err != nil {
		return fmt.Errorf("invalid transfer signing change output commitment: %w", err)
	}
	if len(request.Effect.NewCommitments) <= 1 || !bytes.Equal(fieldValueBytes(changeCommitment), request.Effect.NewCommitments[1]) {
		return fmt.Errorf("transfer signing change output SecretNoteV1 does not match the final effect commitment")
	}

	inputTotal, inputErr := request.InputNotes[0].Amount.Add(request.InputNotes[1].Amount)
	outputTotal, outputErr := request.RecipientOutputNote.Amount.Add(request.ChangeOutputNote.Amount)
	if inputErr != nil || outputErr != nil || inputTotal != outputTotal {
		return fmt.Errorf("transfer signing input and output amounts are not conserved")
	}

	disclosureInput := DisclosureBuildInput{
		OutputCommitment:       fieldValueBytes(recipientCommitment),
		FromNote:               *request.InputNotes[0],
		RecipientNote:          *request.RecipientOutputNote,
		UserDisclosureBlinding: request.UserDisclosureBlinding,
		FullDisclosureBlinding: request.FullDisclosureBlinding,
	}
	if request.UserPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
		if len(request.Effect.UserDisclosureDigest) != 0 {
			return fmt.Errorf("transfer signing all-private effect must omit the user disclosure digest")
		}
	} else {
		expectedUserDigest, err := fixedTransferDigest(disclosureInput, request.UserPrivacyPolicy)
		if err != nil {
			return fmt.Errorf("failed to rebuild transfer signing user disclosure digest: %w", err)
		}
		if !bytes.Equal(expectedUserDigest, request.Effect.UserDisclosureDigest) {
			return fmt.Errorf("transfer signing user disclosure preimage does not match the final effect")
		}
	}
	expectedFullDigest, err := fixedTransferDigest(disclosureInput, privacytypes.DisclosureFullMarkerV1)
	if err != nil {
		return fmt.Errorf("failed to rebuild transfer signing audit disclosure digest: %w", err)
	}
	if !bytes.Equal(expectedFullDigest, request.Effect.AuditDisclosureDigest) {
		return fmt.Errorf("transfer signing audit disclosure preimage does not match the final effect")
	}
	return nil
}

func noteAssetMatchesSigningAsset(note privacytypes.SecretNoteV1, assetID [32]byte) bool {
	asset := note.AssetID.Bytes()
	return bytes.Equal(asset[:], assetID[:])
}

func secretNoteUsesSenderKeys(request JoinSplitOwnerIntentSigningRequestV1, note privacytypes.SecretNoteV1) bool {
	return note.ReceiverSpendPubKeyX.Equal(request.SenderSpendPubKeyX) &&
		note.ReceiverSpendPubKeyY.Equal(request.SenderSpendPubKeyY) &&
		note.ReceiverViewPubKeyX.Equal(request.SenderViewPubKeyX) &&
		note.ReceiverViewPubKeyY.Equal(request.SenderViewPubKeyY)
}

func fieldValueBytes(value privacycrypto.FieldValue) []byte {
	raw := value.Bytes()
	return append([]byte(nil), raw[:]...)
}

// ValidateJoinSplitOwnerDisclosureBlindingV1 applies DBS-01..03 over the
// typed secret-field representation used by this signing boundary.
func ValidateJoinSplitOwnerDisclosureBlindingV1(request JoinSplitOwnerIntentSigningRequestV1) error {
	if request.UserPrivacyPolicy > privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
		return fmt.Errorf("privacy policy is outside the canonical range 0..7")
	}
	if request.UserPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate {
		if !request.UserDisclosureBlinding.IsZero() {
			return fmt.Errorf("all-private user disclosure blinding must use the zero sentinel")
		}
	} else {
		if request.UserDisclosureBlinding.IsZero() {
			return fmt.Errorf("enabled user disclosure blinding must be non-zero")
		}
		if request.UserDisclosureBlinding.Equal(request.RecipientOutputRandomness) {
			return fmt.Errorf("user disclosure blinding must differ from recipient output randomness")
		}
	}
	if request.FullDisclosureBlinding.IsZero() {
		return fmt.Errorf("active full disclosure blinding must be non-zero")
	}
	if request.FullDisclosureBlinding.Equal(request.RecipientOutputRandomness) {
		return fmt.Errorf("full disclosure blinding must differ from recipient output randomness")
	}
	if request.FullDisclosureBlinding.Equal(request.UserDisclosureBlinding) {
		return fmt.Errorf("full disclosure blinding must differ from user disclosure blinding")
	}
	return nil
}

// SignValidatedJoinSplitOwnerIntentV1 centralizes the required validation for
// production signer adapters. The callback is never invoked for an invalid
// structured request.
func SignValidatedJoinSplitOwnerIntentV1(request JoinSplitOwnerIntentSigningRequestV1, sign func(*big.Int) ([]byte, error)) ([]byte, error) {
	if sign == nil {
		return nil, fmt.Errorf("transfer owner signing callback is required")
	}
	if err := ValidateJoinSplitOwnerIntentSigningRequestV1(request); err != nil {
		return nil, err
	}
	return sign(request.Intent)
}
