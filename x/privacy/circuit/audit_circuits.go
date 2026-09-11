package circuit

import (
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/consensys/gnark/std/hash/mimc"
	"github.com/consensys/gnark/std/signature/eddsa"
)

// These four circuits are the inactive audit-field set. They reuse the actual
// NoteV1 relations; no legacy public fields or second signature are witnesses.
type DepositAuditFieldV1 struct {
	Public              AuditPublic `gnark:",public"`
	Random              AuditRandomness
	Commitment          frontend.Variable    `gnark:",secret"`
	ReceiverSpendPubKey twistededwards.Point `gnark:",secret"`
	ReceiverViewPubKey  twistededwards.Point `gnark:",secret"`
	Randomness          frontend.Variable    `gnark:",secret"`
}

func (c *DepositAuditFieldV1) noteRelation() *DepositCircuit {
	return &DepositCircuit{
		Commitment:          c.Commitment,
		Amount:              c.Public.PublicAmount,
		AssetID:             c.Public.PublicAsset,
		ReceiverSpendPubKey: c.ReceiverSpendPubKey,
		ReceiverViewPubKey:  c.ReceiverViewPubKey,
		Randomness:          c.Randomness,
	}
}

type SpendAuditFieldV1 struct {
	Public              AuditPublic `gnark:",public"`
	Random              AuditRandomness
	Nullifier           frontend.Variable              `gnark:",secret"`
	ReceiverSpendPubKey eddsa.PublicKey                `gnark:",secret"`
	ReceiverViewPubKey  eddsa.PublicKey                `gnark:",secret"`
	Signature           eddsa.Signature                `gnark:",secret"`
	Randomness          frontend.Variable              `gnark:",secret"`
	Path                [MerkleDepth]frontend.Variable `gnark:",secret"`
	PathHelper          [MerkleDepth]frontend.Variable `gnark:",secret"`
}

func (c *SpendAuditFieldV1) noteRelation() *SpendCircuit {
	return &SpendCircuit{
		MerkleRoot:          c.Public.MerkleRoot,
		ChainDomainHi:       c.Public.NetworkHi,
		ChainDomainLo:       c.Public.NetworkLo,
		ExpiresAtUnix:       c.Public.ExpiresAtUnix,
		Nullifier:           c.Nullifier,
		Amount:              c.Public.PublicAmount,
		RecipientDigestHi:   c.Public.PublicTargetHi,
		RecipientDigestLo:   c.Public.PublicTargetLo,
		AssetID:             c.Public.PublicAsset,
		ReceiverSpendPubKey: c.ReceiverSpendPubKey,
		ReceiverViewPubKey:  c.ReceiverViewPubKey,
		Signature:           c.Signature,
		Randomness:          c.Randomness,
		Path:                c.Path,
		PathHelper:          c.PathHelper,
	}
}

type JoinSplitAuditFieldV1 struct {
	Public                 AuditPublic `gnark:",public"`
	Random                 AuditRandomness
	Nullifiers             [NumInputs]frontend.Variable              `gnark:",secret"`
	Commitments            [NumOutputs]frontend.Variable             `gnark:",secret"`
	UserPrivacyPolicy      frontend.Variable                         `gnark:",secret"`
	UserDisclosureDigest   frontend.Variable                         `gnark:",secret"`
	FullDisclosureDigest   frontend.Variable                         `gnark:",secret"`
	AssetID                frontend.Variable                         `gnark:",secret"`
	InputAmounts           [NumInputs]frontend.Variable              `gnark:",secret"`
	InputRandomness        [NumInputs]frontend.Variable              `gnark:",secret"`
	InputPaths             [NumInputs][MerkleDepth]frontend.Variable `gnark:",secret"`
	InputPathHelpers       [NumInputs][MerkleDepth]frontend.Variable `gnark:",secret"`
	OwnerSignature         eddsa.Signature                           `gnark:",secret"`
	InputSpendPubKeys      [NumInputs]eddsa.PublicKey                `gnark:",secret"`
	InputViewPubKeys       [NumInputs]eddsa.PublicKey                `gnark:",secret"`
	OutputAmounts          [NumOutputs]frontend.Variable             `gnark:",secret"`
	OutputRandomness       [NumOutputs]frontend.Variable             `gnark:",secret"`
	OutputSpendPubKeys     [NumOutputs]eddsa.PublicKey               `gnark:",secret"`
	OutputViewPubKeys      [NumOutputs]eddsa.PublicKey               `gnark:",secret"`
	UserDisclosureBlinding frontend.Variable                         `gnark:",secret"`
	FullDisclosureBlinding frontend.Variable                         `gnark:",secret"`
}

func (c *JoinSplitAuditFieldV1) noteRelation() *JoinSplitCircuit {
	return &JoinSplitCircuit{
		MerkleRoot:             c.Public.MerkleRoot,
		ChainDomainHi:          c.Public.NetworkHi,
		ChainDomainLo:          c.Public.NetworkLo,
		ExpiresAtUnix:          c.Public.ExpiresAtUnix,
		Nullifiers:             c.Nullifiers,
		Commitments:            c.Commitments,
		UserPrivacyPolicy:      c.UserPrivacyPolicy,
		UserDisclosureDigest:   c.UserDisclosureDigest,
		FullDisclosureDigest:   c.FullDisclosureDigest,
		PayloadDigestHi:        c.Public.AuxHi,
		PayloadDigestLo:        c.Public.AuxLo,
		AssetID:                c.AssetID,
		InputAmounts:           c.InputAmounts,
		InputRandomness:        c.InputRandomness,
		InputPaths:             c.InputPaths,
		InputPathHelpers:       c.InputPathHelpers,
		OwnerSignature:         c.OwnerSignature,
		InputSpendPubKeys:      c.InputSpendPubKeys,
		InputViewPubKeys:       c.InputViewPubKeys,
		OutputAmounts:          c.OutputAmounts,
		OutputRandomness:       c.OutputRandomness,
		OutputSpendPubKeys:     c.OutputSpendPubKeys,
		OutputViewPubKeys:      c.OutputViewPubKeys,
		UserDisclosureBlinding: c.UserDisclosureBlinding,
		FullDisclosureBlinding: c.FullDisclosureBlinding,
	}
}

type BatchJoinSplitAuditFieldV1 struct {
	Public                  AuditPublic `gnark:",public"`
	Random                  AuditRandomness
	AssetID                 frontend.Variable                                       `gnark:",secret"`
	OwnerSpendPubKey        eddsa.PublicKey                                         `gnark:",secret"`
	OwnerViewPubKey         eddsa.PublicKey                                         `gnark:",secret"`
	OwnerSignature          eddsa.Signature                                         `gnark:",secret"`
	InputAmounts            [MaxBatchJoinSplitInputs]frontend.Variable              `gnark:",secret"`
	InputRandomness         [MaxBatchJoinSplitInputs]frontend.Variable              `gnark:",secret"`
	InputSpendPubKeys       [MaxBatchJoinSplitInputs]eddsa.PublicKey                `gnark:",secret"`
	InputViewPubKeys        [MaxBatchJoinSplitInputs]eddsa.PublicKey                `gnark:",secret"`
	InputPaths              [MaxBatchJoinSplitInputs][MerkleDepth]frontend.Variable `gnark:",secret"`
	InputPathHelpers        [MaxBatchJoinSplitInputs][MerkleDepth]frontend.Variable `gnark:",secret"`
	OutputAmounts           [MaxBatchJoinSplitOutputs]frontend.Variable             `gnark:",secret"`
	OutputRandomness        [MaxBatchJoinSplitOutputs]frontend.Variable             `gnark:",secret"`
	OutputSpendPubKeys      [MaxBatchJoinSplitOutputs]eddsa.PublicKey               `gnark:",secret"`
	OutputViewPubKeys       [MaxBatchJoinSplitOutputs]eddsa.PublicKey               `gnark:",secret"`
	OutputPrivacyPolicies   [MaxBatchJoinSplitOutputs]frontend.Variable             `gnark:",secret"`
	UserDisclosureBlindings [MaxBatchJoinSplitOutputs]frontend.Variable             `gnark:",secret"`
	FullDisclosureBlindings [MaxBatchJoinSplitOutputs]frontend.Variable             `gnark:",secret"`
}

func (c *BatchJoinSplitAuditFieldV1) noteRelation() *BatchJoinSplit16x32 {
	return &BatchJoinSplit16x32{
		MerkleRoot:              c.Public.MerkleRoot,
		ChainDomainHi:           c.Public.NetworkHi,
		ChainDomainLo:           c.Public.NetworkLo,
		ExpiresAtUnix:           c.Public.ExpiresAtUnix,
		InputCount:              c.Public.InputCount,
		OutputCount:             c.Public.OutputCount,
		NullifierRoot:           c.Public.NullifierRoot,
		CommitmentRoot:          c.Public.CommitmentRoot,
		UserDisclosureRoot:      c.Public.UserDisclosureRoot,
		FullDisclosureRoot:      c.Public.SelfViewRoot,
		PayloadDigestHi:         c.Public.AuxHi,
		PayloadDigestLo:         c.Public.AuxLo,
		AssetID:                 c.AssetID,
		OwnerSpendPubKey:        c.OwnerSpendPubKey,
		OwnerViewPubKey:         c.OwnerViewPubKey,
		OwnerSignature:          c.OwnerSignature,
		InputAmounts:            c.InputAmounts,
		InputRandomness:         c.InputRandomness,
		InputSpendPubKeys:       c.InputSpendPubKeys,
		InputViewPubKeys:        c.InputViewPubKeys,
		InputPaths:              c.InputPaths,
		InputPathHelpers:        c.InputPathHelpers,
		OutputAmounts:           c.OutputAmounts,
		OutputRandomness:        c.OutputRandomness,
		OutputSpendPubKeys:      c.OutputSpendPubKeys,
		OutputViewPubKeys:       c.OutputViewPubKeys,
		OutputPrivacyPolicies:   c.OutputPrivacyPolicies,
		UserDisclosureBlindings: c.UserDisclosureBlindings,
		FullDisclosureBlindings: c.FullDisclosureBlindings,
	}
}

func (c *DepositAuditFieldV1) Define(api frontend.API) error {
	if err := c.noteRelation().Define(api); err != nil {
		return err
	}
	api.AssertIsEqual(c.Public.CommitmentRoot, auditVector(api, privacytypes.BatchVectorCommitmentV1, []frontend.Variable{c.Commitment}))
	plain := []frontend.Variable{c.Public.PublicAsset, c.Public.PublicAmount, c.ReceiverSpendPubKey.X, c.ReceiverSpendPubKey.Y, c.ReceiverViewPubKey.X, c.ReceiverViewPubKey.Y}
	return auditRelation(api, &c.Public, c.Random, auditfield.KindDeposit, plain, nil, nil)
}
func (c *SpendAuditFieldV1) Define(api frontend.API) error {
	return c.noteRelation().defineRelation(api, func(cin frontend.Variable) error {
		api.AssertIsEqual(c.Public.NullifierRoot, auditVector(api, privacytypes.BatchVectorNullifierV1, []frontend.Variable{c.Nullifier}))
		return auditRelation(api, &c.Public, c.Random, auditfield.KindWithdraw, []frontend.Variable{c.Public.PublicAsset, cin}, &c.ReceiverSpendPubKey, &c.Signature)
	})
}
func (c *JoinSplitAuditFieldV1) Define(api frontend.API) error {
	return c.noteRelation().defineWithIntent(api, func(cin []frontend.Variable) error {
		api.AssertIsEqual(c.Public.NullifierRoot, auditVector(api, privacytypes.BatchVectorNullifierV1, c.Nullifiers[:]))
		api.AssertIsEqual(c.Public.CommitmentRoot, auditVector(api, privacytypes.BatchVectorCommitmentV1, c.Commitments[:]))
		h, _ := mimc.NewMiMC(api)
		h.Write(privacytypes.DomainFieldV1("clairveil.audit.user-2x2.v1"), c.UserPrivacyPolicy, c.UserDisclosureDigest)
		api.AssertIsEqual(c.Public.UserDisclosureRoot, h.Sum())
		api.AssertIsEqual(c.Public.SelfViewRoot, c.FullDisclosureDigest)
		plain := append([]frontend.Variable{c.AssetID}, cin...)
		for i := 0; i < NumOutputs; i++ {
			plain = append(plain, c.OutputAmounts[i], c.OutputSpendPubKeys[i].A.X, c.OutputSpendPubKeys[i].A.Y, c.OutputViewPubKeys[i].A.X, c.OutputViewPubKeys[i].A.Y)
		}
		return auditRelation(api, &c.Public, c.Random, auditfield.KindTransfer2x2, plain, &c.InputSpendPubKeys[0], &c.OwnerSignature)
	})
}
func (c *BatchJoinSplitAuditFieldV1) Define(api frontend.API) error {
	return c.noteRelation().defineRelation(api, func(cin []frontend.Variable) error {
		enabled := exactActivePrefix(api, c.Public.OutputCount, MaxBatchJoinSplitOutputs)
		plain := append([]frontend.Variable{c.AssetID}, cin...)
		for i := 0; i < MaxBatchJoinSplitOutputs; i++ {
			for _, v := range []frontend.Variable{c.OutputAmounts[i], c.OutputSpendPubKeys[i].A.X, c.OutputSpendPubKeys[i].A.Y, c.OutputViewPubKeys[i].A.X, c.OutputViewPubKeys[i].A.Y} {
				plain = append(plain, api.Select(enabled[i], v, 0))
			}
		}
		return auditRelation(api, &c.Public, c.Random, auditfield.KindBatch16x32, plain, &c.OwnerSpendPubKey, &c.OwnerSignature)
	})
}
