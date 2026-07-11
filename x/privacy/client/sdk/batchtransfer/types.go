package batchtransfer

import (
	"context"
	"errors"
	"math/big"
	"time"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	PreparedBatchTransferPayloadVersion = "batch-transfer-payload-v1"
	PreparedBatchTransferProofVersion   = "batch-transfer-proof-v1"
	BatchTransferCircuitSetID           = privacytypes.ActiveCircuitSetID
)

var (
	ErrWalletSyncRequired  = errors.New("batch transfer input root mismatch: wallet sync and replan required")
	ErrPreparationRequired = errors.New("selected inputs cannot fund the batch within the 16-input limit: preparation required")
)

type OutputKind string

const (
	OutputPayment OutputKind = "payment"
	OutputChange  OutputKind = "change"
	OutputPadding OutputKind = "padding"
)

type OutputMode string

const (
	OutputModeCompact OutputMode = "compact"
	OutputModeExact32 OutputMode = "exact32"
)

type InputNote struct {
	Note privacytypes.Note `json:"note"`
}

type Payment struct {
	SpendPubKey            *crypto_tedwards.PointAffine
	ViewPubKey             *crypto_tedwards.PointAffine
	Amount                 *big.Int
	PrivacyPolicy          uint32
	DisclosureMode         privacytypes.UserDisclosureMode
	DisclosureTargetPubKey *crypto_tedwards.PointAffine
}

type PlanBatchTransferInput struct {
	Inputs           []InputNote
	Payments         []Payment
	OwnerSpendPubKey *crypto_tedwards.PointAffine
	OwnerViewPubKey  *crypto_tedwards.PointAffine
	Mode             OutputMode
	PaddingCount     int
}

type PlannedOutput struct {
	Kind                   OutputKind
	SpendPubKey            *crypto_tedwards.PointAffine
	ViewPubKey             *crypto_tedwards.PointAffine
	Amount                 *big.Int
	PrivacyPolicy          uint32
	DisclosureMode         privacytypes.UserDisclosureMode
	DisclosureTargetPubKey *crypto_tedwards.PointAffine
}

type BatchTransferPlan struct {
	Inputs       []InputNote
	Outputs      []PlannedOutput
	InputTotal   *big.Int
	PaymentTotal *big.Int
	Change       *big.Int
}

type MerklePathResult struct {
	Root       []byte
	Path       []string
	PathHelper []uint32
}

type MerklePathProvider interface {
	LookupMerklePath(context.Context, string) (*MerklePathResult, error)
}

type PreparedBatchTransferInput struct {
	Note             privacytypes.Note `json:"note"`
	MerklePath       []string          `json:"merkle_path"`
	MerklePathHelper []uint32          `json:"merkle_path_helper"`
	Nullifier        []byte            `json:"nullifier"`
}

type PreparedBatchTransferOutput struct {
	Kind                   OutputKind                      `json:"kind"`
	Note                   privacytypes.Note               `json:"note"`
	PrivacyPolicy          uint32                          `json:"privacy_policy"`
	DisclosureMode         privacytypes.UserDisclosureMode `json:"disclosure_mode"`
	DisclosureTargetPubKey []byte                          `json:"disclosure_target_pubkey,omitempty"`
	UserDisclosureBlinding *big.Int                        `json:"user_disclosure_blinding"`
	FullDisclosureBlinding *big.Int                        `json:"full_disclosure_blinding"`
}

type PreparedBatchTransfer struct {
	Root    []byte
	AssetID *big.Int
	Inputs  []PreparedBatchTransferInput
	Outputs []PreparedBatchTransferOutput
}

type BuildPreparedBatchTransferPayloadInput struct {
	Creator                        string
	ChainID                        string
	ExpiresAtUnix                  int64
	AuditKeyID                     string
	AuditKeyEpoch                  uint64
	AuditDisclosureTargetPubKey    *crypto_tedwards.PointAffine
	SelfViewDisclosureTargetPubKey *crypto_tedwards.PointAffine
	DisableSelfViewDisclosure      bool
}

type BatchTransferSigningOutput struct {
	Kind                   OutputKind
	Commitment             []byte
	RecipientSpendPubKey   []byte
	RecipientViewPubKey    []byte
	Amount                 *big.Int
	AssetID                *big.Int
	Randomness             *big.Int
	PrivacyPolicy          uint32
	DisclosureMode         privacytypes.UserDisclosureMode
	UserDisclosureBlinding *big.Int
	FullDisclosureBlinding *big.Int
	WireOutput             *privacytypes.BatchTransferOutput
}

type BatchTransferSigningInput struct {
	Commitment  []byte
	Nullifier   []byte
	SpendPubKey []byte
	ViewPubKey  []byte
	Amount      *big.Int
	AssetID     *big.Int
	Randomness  *big.Int
}

type BatchTransferSigningRequest struct {
	Version                     string
	CircuitSetID                string
	ChainID                     string
	ExpiresAtUnix               int64
	OrderedInputs               []BatchTransferSigningInput
	OrderedInputNullifiers      [][]byte
	OrderedOutputs              []BatchTransferSigningOutput
	OwnerSpendPubKey            []byte
	OwnerViewPubKey             []byte
	Root                        []byte
	AssetID                     *big.Int
	InputTotal                  *big.Int
	AuditKeyID                  string
	AuditKeyEpoch               uint64
	AuditDisclosureTargetPubKey []byte
	SelfViewEnabled             bool
	NullifierRoot               *big.Int
	CommitmentRoot              *big.Int
	UserDisclosureRoot          *big.Int
	FullDisclosureRoot          *big.Int
	CanonicalPayload            []byte
	PayloadDigestHi             *big.Int
	PayloadDigestLo             *big.Int
	ExpectedIntent              *big.Int
	CanonicalEffect             *privacytypes.MsgBatchTransfer
}

type BatchTransferSigner interface {
	SignBatchTransfer(BatchTransferSigningRequest) ([]byte, error)
}

type PreparedBatchTransferPayload struct {
	Version                     string                              `json:"version"`
	CircuitSetID                string                              `json:"circuit_set_id"`
	Creator                     string                              `json:"creator,omitempty"`
	ChainID                     string                              `json:"chain_id"`
	ExpiresAtUnix               int64                               `json:"expires_at_unix"`
	Root                        []byte                              `json:"root"`
	AssetID                     *big.Int                            `json:"asset_id"`
	Inputs                      []PreparedBatchTransferInput        `json:"inputs"`
	Outputs                     []PreparedBatchTransferOutput       `json:"outputs"`
	MessageOutputs              []*privacytypes.BatchTransferOutput `json:"message_outputs"`
	AuditKeyID                  string                              `json:"audit_key_id"`
	AuditKeyEpoch               uint64                              `json:"audit_key_epoch"`
	AuditDisclosureTargetPubKey []byte                              `json:"audit_disclosure_target_pubkey"`
	NullifierRoot               *big.Int                            `json:"nullifier_root"`
	CommitmentRoot              *big.Int                            `json:"commitment_root"`
	UserDisclosureRoot          *big.Int                            `json:"user_disclosure_root"`
	FullDisclosureRoot          *big.Int                            `json:"full_disclosure_root"`
	PayloadDigestHi             *big.Int                            `json:"payload_digest_hi"`
	PayloadDigestLo             *big.Int                            `json:"payload_digest_lo"`
	ExpectedIntent              *big.Int                            `json:"expected_intent"`
	OwnerSignature              []byte                              `json:"owner_signature"`
	PayloadHash                 string                              `json:"payload_hash"`
}

type PreparedBatchTransferProof struct {
	Version            string `json:"version"`
	RequestPayloadHash string `json:"request_payload_hash"`
	Proof              []byte `json:"proof"`
	CircuitSetID       string `json:"circuit_set_id,omitempty"`
	ArtifactChecksum   string `json:"artifact_checksum,omitempty"`
}

type BatchJoinSplitArtifactProvider interface {
	BatchJoinSplitR1CS() (constraint.ConstraintSystem, error)
	BatchJoinSplitProvingKey() (groth16.ProvingKey, error)
}

type BatchJoinSplitProofRunner interface {
	ProveBatchJoinSplit(constraint.ConstraintSystem, groth16.ProvingKey, witness.Witness) (groth16.Proof, error)
}

type BatchTransferBroadcaster interface {
	BroadcastBatchTransferMessage(context.Context, *privacytypes.MsgBatchTransfer) (*sdk.TxResponse, error)
}

func ValidatePreparedBatchTransferPayloadMetadata(payload *PreparedBatchTransferPayload) error {
	return ValidatePreparedBatchTransferPayloadMetadataAt(payload, time.Now())
}
