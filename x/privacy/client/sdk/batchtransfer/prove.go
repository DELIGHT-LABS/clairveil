package batchtransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/signature/eddsa"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// LocalBatchJoinSplitProofRunner proves in-process and never performs implicit
// remote failover.
type LocalBatchJoinSplitProofRunner struct{}

func (LocalBatchJoinSplitProofRunner) ProveBatchJoinSplit(r1cs constraint.ConstraintSystem, pk groth16.ProvingKey, w witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, pk, w)
}

func ProvePreparedBatchTransfer(payload *PreparedBatchTransferPayload, artifacts BatchJoinSplitArtifactProvider, runner BatchJoinSplitProofRunner) (*PreparedBatchTransferProof, error) {
	return BuildPreparedBatchTransferProofAt(payload, artifacts, runner, time.Now())
}

func BuildPreparedBatchTransferProofAt(payload *PreparedBatchTransferPayload, artifacts BatchJoinSplitArtifactProvider, runner BatchJoinSplitProofRunner, now time.Time) (*PreparedBatchTransferProof, error) {
	if err := ValidatePreparedBatchTransferPayloadMetadataAt(payload, now); err != nil {
		return nil, err
	}
	if artifacts == nil || runner == nil {
		return nil, fmt.Errorf("batch artifacts and local proof runner are required")
	}
	assignment, err := buildAssignment(payload)
	if err != nil {
		return nil, err
	}
	w, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("build batch witness: %w", err)
	}
	r1cs, err := artifacts.BatchJoinSplitR1CS()
	if err != nil {
		return nil, err
	}
	pk, err := artifacts.BatchJoinSplitProvingKey()
	if err != nil {
		return nil, err
	}
	proof, err := runner.ProveBatchJoinSplit(r1cs, pk, w)
	if err != nil {
		return nil, fmt.Errorf("batch proof generation failed: %w", err)
	}
	var buf bytes.Buffer
	if _, err := proof.WriteTo(&buf); err != nil {
		return nil, err
	}
	result := &PreparedBatchTransferProof{Version: PreparedBatchTransferProofVersion, RequestPayloadHash: payload.PayloadHash, Proof: buf.Bytes(), CircuitSetID: payload.CircuitSetID}
	if err := ValidatePreparedBatchTransferProofAt(payload, result, now); err != nil {
		return nil, err
	}
	return result, nil
}

func ValidatePreparedBatchTransferProofAt(payload *PreparedBatchTransferPayload, proof *PreparedBatchTransferProof, now time.Time) error {
	if err := ValidatePreparedBatchTransferPayloadMetadataAt(payload, now); err != nil {
		return err
	}
	if proof == nil || proof.Version != PreparedBatchTransferProofVersion {
		return fmt.Errorf("unsupported prepared batch proof version")
	}
	if proof.RequestPayloadHash != payload.PayloadHash {
		return fmt.Errorf("prepared proof payload hash mismatch")
	}
	if proof.CircuitSetID != "" && proof.CircuitSetID != payload.CircuitSetID {
		return fmt.Errorf("prepared proof circuit set mismatch")
	}
	if len(proof.Proof) != privacytypes.BatchTransferProofSizeV1 {
		return fmt.Errorf("batch proof must be exactly %d bytes", privacytypes.BatchTransferProofSizeV1)
	}
	return nil
}

func WritePreparedBatchTransferProof(path string, proof *PreparedBatchTransferProof) error {
	bz, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBatchFile(path, bz)
}

func BuildMsgBatchTransfer(payload *PreparedBatchTransferPayload, proof *PreparedBatchTransferProof, creator string) (*privacytypes.MsgBatchTransfer, error) {
	if err := ValidatePreparedBatchTransferProofAt(payload, proof, time.Now()); err != nil {
		return nil, err
	}
	if creator == "" {
		creator = payload.Creator
	}
	msg := payload.effectMessage(append([]byte(nil), proof.Proof...), creator)
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	return msg, nil
}

func BroadcastBatchTransfer(ctx context.Context, broadcaster BatchTransferBroadcaster, payload *PreparedBatchTransferPayload, proof *PreparedBatchTransferProof, creator string) (*sdk.TxResponse, error) {
	if broadcaster == nil {
		return nil, fmt.Errorf("batch transfer broadcaster is required")
	}
	msg, err := BuildMsgBatchTransfer(payload, proof, creator)
	if err != nil {
		return nil, err
	}
	res, err := broadcaster.BroadcastBatchTransferMessage(ctx, msg)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("batch transfer broadcaster returned nil response")
	}
	if res.Code != 0 {
		return res, fmt.Errorf("tx failed with code %d: %s", res.Code, res.RawLog)
	}
	return res, nil
}

func buildAssignment(p *PreparedBatchTransferPayload) (*circuit.BatchJoinSplit16x32, error) {
	chain, err := privacytypes.ComputeChainDomainV1(p.ChainID, privacytypes.ActiveCircuitSetID)
	if err != nil {
		return nil, err
	}
	a := &circuit.BatchJoinSplit16x32{MerkleRoot: new(big.Int).SetBytes(p.Root), ChainDomainHi: chain.Hi, ChainDomainLo: chain.Lo, ExpiresAtUnix: big.NewInt(p.ExpiresAtUnix), InputCount: big.NewInt(int64(len(p.Inputs))), OutputCount: big.NewInt(int64(len(p.Outputs))), NullifierRoot: p.NullifierRoot, CommitmentRoot: p.CommitmentRoot, UserDisclosureRoot: p.UserDisclosureRoot, FullDisclosureRoot: p.FullDisclosureRoot, PayloadDigestHi: p.PayloadDigestHi, PayloadDigestLo: p.PayloadDigestLo, AssetID: p.AssetID}
	ownerSpend, _ := pointFromCoordinates(p.Inputs[0].Note.ReceiverSpendPubKeyX, p.Inputs[0].Note.ReceiverSpendPubKeyY)
	ownerView, _ := pointFromCoordinates(p.Inputs[0].Note.ReceiverViewPubKeyX, p.Inputs[0].Note.ReceiverViewPubKeyY)
	assignKey(&a.OwnerSpendPubKey, ownerSpend)
	assignKey(&a.OwnerViewPubKey, ownerView)
	if err := assignSig(&a.OwnerSignature, p.OwnerSignature); err != nil {
		return nil, err
	}
	for i := 0; i < circuit.MaxBatchJoinSplitInputs; i++ {
		assignKey(&a.InputSpendPubKeys[i], ownerSpend)
		assignKey(&a.InputViewPubKeys[i], ownerView)
		a.InputAmounts[i] = big.NewInt(0)
		a.InputRandomness[i] = big.NewInt(0)
		for j := 0; j < circuit.MerkleDepth; j++ {
			a.InputPaths[i][j] = big.NewInt(0)
			a.InputPathHelpers[i][j] = big.NewInt(0)
		}
	}
	for i, in := range p.Inputs {
		a.InputAmounts[i] = in.Note.Amount
		a.InputRandomness[i] = in.Note.Randomness
		for j, raw := range in.MerklePath {
			if j >= circuit.MerkleDepth {
				return nil, fmt.Errorf("input path exceeds circuit depth")
			}
			v, ok := new(big.Int).SetString(raw, 16)
			if !ok {
				return nil, fmt.Errorf("invalid path")
			}
			a.InputPaths[i][j] = v
			a.InputPathHelpers[i][j] = new(big.Int).SetUint64(uint64(in.MerklePathHelper[j]))
		}
	}
	for i := 0; i < circuit.MaxBatchJoinSplitOutputs; i++ {
		assignKey(&a.OutputSpendPubKeys[i], ownerSpend)
		assignKey(&a.OutputViewPubKeys[i], ownerView)
		a.OutputAmounts[i] = big.NewInt(0)
		a.OutputRandomness[i] = big.NewInt(0)
		a.OutputPrivacyPolicies[i] = big.NewInt(0)
		a.UserDisclosureBlindings[i] = big.NewInt(0)
		a.FullDisclosureBlindings[i] = big.NewInt(0)
	}
	for i, out := range p.Outputs {
		sp, _ := pointFromCoordinates(out.Note.ReceiverSpendPubKeyX, out.Note.ReceiverSpendPubKeyY)
		vp, _ := pointFromCoordinates(out.Note.ReceiverViewPubKeyX, out.Note.ReceiverViewPubKeyY)
		assignKey(&a.OutputSpendPubKeys[i], sp)
		assignKey(&a.OutputViewPubKeys[i], vp)
		a.OutputAmounts[i] = out.Note.Amount
		a.OutputRandomness[i] = out.Note.Randomness
		a.OutputPrivacyPolicies[i] = new(big.Int).SetUint64(uint64(out.PrivacyPolicy))
		a.UserDisclosureBlindings[i] = out.UserDisclosureBlinding
		a.FullDisclosureBlindings[i] = out.FullDisclosureBlinding
	}
	return a, nil
}

func assignKey(dst *eddsa.PublicKey, p *crypto_tedwards.PointAffine) {
	x, y := pointCoordinates(p)
	dst.A.X = x
	dst.A.Y = y
}
func assignSig(dst *eddsa.Signature, b []byte) error {
	if _, err := privacycrypto.DecodeCanonicalEdDSASignature(b); err != nil {
		return err
	}
	var r crypto_tedwards.PointAffine
	if _, err := r.SetBytes(b[:32]); err != nil {
		return err
	}
	x, y := pointCoordinates(&r)
	dst.R.X = x
	dst.R.Y = y
	dst.S = new(big.Int).SetBytes(b[32:])
	return nil
}
