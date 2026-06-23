package transfer

import (
	"context"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type StepDisclosureConfig struct {
	UserPrivacyPolicy              uint32
	UserDisclosureMode             privacytypes.UserDisclosureMode
	UserDisclosureTargetPubKey     *crypto_tedwards.PointAffine
	UserDisclosureTargetPubKeyBz   []byte
	AuditDisclosureTargetPubKey    *crypto_tedwards.PointAffine
	AuditDisclosureTargetPubKeyBz  []byte
	DisableSelfViewDisclosure      bool
	SelfViewDisclosureTargetPubKey *crypto_tedwards.PointAffine
}

type BuildTransferStepMessageInput struct {
	Creator              string
	Inputs               [2]privacyscan.FoundNote
	RecipientSpendPubKey *crypto_tedwards.PointAffine
	RecipientViewPubKey  *crypto_tedwards.PointAffine
	TransferAmount       *big.Int
	TransferDenom        string
	SenderSpendPubKey    *crypto_tedwards.PointAffine
	SenderViewPubKey     *crypto_tedwards.PointAffine
	IsFinal              bool
	Disclosure           StepDisclosureConfig
}

func EffectiveStepDisclosureConfig(config StepDisclosureConfig, isFinal bool) StepDisclosureConfig {
	if isFinal {
		return config
	}

	return StepDisclosureConfig{
		UserPrivacyPolicy:              privacytypes.TransferPrivacyPolicyAllPrivate,
		UserDisclosureMode:             privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		AuditDisclosureTargetPubKey:    config.AuditDisclosureTargetPubKey,
		AuditDisclosureTargetPubKeyBz:  append([]byte(nil), config.AuditDisclosureTargetPubKeyBz...),
		DisableSelfViewDisclosure:      config.DisableSelfViewDisclosure,
		SelfViewDisclosureTargetPubKey: config.SelfViewDisclosureTargetPubKey,
	}
}

func BuildTransferStepMessage(
	ctx context.Context,
	merklePaths MerklePathProvider,
	signer NoteHashSigner,
	artifacts JoinSplitArtifactProvider,
	runner JoinSplitProofRunner,
	input BuildTransferStepMessageInput,
) (*privacytypes.MsgTransfer, error) {
	effectiveDisclosure := EffectiveStepDisclosureConfig(input.Disclosure, input.IsFinal)
	return BuildTransferMessage(
		ctx,
		merklePaths,
		signer,
		artifacts,
		runner,
		BuildTransferMessageInput{
			Creator:                        input.Creator,
			Inputs:                         input.Inputs,
			RecipientSpendPubKey:           input.RecipientSpendPubKey,
			RecipientViewPubKey:            input.RecipientViewPubKey,
			TransferAmount:                 input.TransferAmount,
			TransferDenom:                  input.TransferDenom,
			SenderSpendPubKey:              input.SenderSpendPubKey,
			SenderViewPubKey:               input.SenderViewPubKey,
			UserPrivacyPolicy:              effectiveDisclosure.UserPrivacyPolicy,
			UserDisclosureMode:             effectiveDisclosure.UserDisclosureMode,
			UserDisclosureTargetPubKey:     effectiveDisclosure.UserDisclosureTargetPubKey,
			UserDisclosureTargetPubKeyBz:   effectiveDisclosure.UserDisclosureTargetPubKeyBz,
			AuditDisclosureTargetPubKey:    effectiveDisclosure.AuditDisclosureTargetPubKey,
			AuditDisclosureTargetPubKeyBz:  effectiveDisclosure.AuditDisclosureTargetPubKeyBz,
			DisableSelfViewDisclosure:      effectiveDisclosure.DisableSelfViewDisclosure,
			SelfViewDisclosureTargetPubKey: effectiveDisclosure.SelfViewDisclosureTargetPubKey,
		},
	)
}
