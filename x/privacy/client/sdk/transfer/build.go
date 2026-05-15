package transfer

import (
	"context"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type BuildTransferMessageInput struct {
	Creator                       string
	Inputs                        [2]privacyscan.FoundNote
	RecipientSpendPubKey          *crypto_tedwards.PointAffine
	RecipientViewPubKey           *crypto_tedwards.PointAffine
	TransferAmount                *big.Int
	TransferDenom                 string
	SenderSpendPubKey             *crypto_tedwards.PointAffine
	SenderViewPubKey              *crypto_tedwards.PointAffine
	UserPrivacyPolicy             uint32
	UserDisclosureMode            privacytypes.UserDisclosureMode
	UserDisclosureTargetPubKey    *crypto_tedwards.PointAffine
	UserDisclosureTargetPubKeyBz  []byte
	AuditDisclosureTargetPubKey   *crypto_tedwards.PointAffine
	AuditDisclosureTargetPubKeyBz []byte
}

func BuildTransferMessage(
	ctx context.Context,
	merklePaths MerklePathProvider,
	signer NoteHashSigner,
	artifacts JoinSplitArtifactProvider,
	runner JoinSplitProofRunner,
	input BuildTransferMessageInput,
) (*privacytypes.MsgTransfer, error) {
	preparedPayload, err := BuildPreparedTransferPayload(ctx, merklePaths, signer, input)
	if err != nil {
		return nil, err
	}
	proof, err := BuildPreparedTransferProof(*preparedPayload, artifacts, runner)
	if err != nil {
		return nil, err
	}
	return preparedPayload.ToMsg(*proof)
}

func encodedDisclosureTargetBytes(point *crypto_tedwards.PointAffine, provided []byte) []byte {
	if len(provided) != 0 {
		return append([]byte(nil), provided...)
	}
	if point == nil {
		return nil
	}

	pointBytes := point.Bytes()
	return append([]byte(nil), pointBytes[:]...)
}
