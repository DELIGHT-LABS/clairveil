package provertransport

import (
	"bytes"
	"context"
	"fmt"
	"time"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	AuditFieldProofRequestVersion  = "v1"
	AuditFieldProofResponseVersion = "v1"
)

// AuditFieldProofRequest is intentionally an explicit trusted-prover boundary.
// Witness is a complete gnark witness and can contain r plus transaction
// secrets; callers must use only a local or independently controlled proverd
// and clear their prepared object immediately after the response is handled.
// The request never has a generic JSON route through wallet persistence.
type AuditFieldProofRequest struct {
	Version      string   `json:"version"`
	CircuitSetID string   `json:"circuit_set_id"`
	CircuitID    string   `json:"circuit_id"`
	ArtifactHash []byte   `json:"artifact_hash"`
	PublicInputs [][]byte `json:"public_inputs"`
	Witness      []byte   `json:"witness"`
}

// AuditFieldProofResponse repeats all public binding material. A client must
// call ValidateAuditFieldProofResponse before it constructs or broadcasts a
// v2 message; framing equality alone is not proof verification.
type AuditFieldProofResponse struct {
	Version      string   `json:"version"`
	CircuitSetID string   `json:"circuit_set_id"`
	CircuitID    string   `json:"circuit_id"`
	ArtifactHash []byte   `json:"artifact_hash"`
	PublicInputs [][]byte `json:"public_inputs"`
	Proof        []byte   `json:"proof"`
}

type AuditFieldArtifactProvider interface {
	AuditFieldR1CS(privacyzk.CircuitID) (constraint.ConstraintSystem, error)
	AuditFieldProvingKey(privacyzk.CircuitID) (groth16.ProvingKey, error)
}

type AuditFieldProofRunner interface {
	ProveAuditField(constraint.ConstraintSystem, groth16.ProvingKey, witness.Witness) (groth16.Proof, error)
}

type ReferenceAuditFieldProver struct {
	Artifacts AuditFieldArtifactProvider
	Runner    AuditFieldProofRunner
}

func (p ReferenceAuditFieldProver) ProveAuditField(request AuditFieldProofRequest) (*AuditFieldProofResponse, error) {
	if p.Artifacts == nil || p.Runner == nil {
		return nil, fmt.Errorf("audit-field prover dependencies are required")
	}
	full, err := DecodeAuditFieldProofWitness(request)
	if err != nil {
		return nil, err
	}
	defer clearAuditWitness(full)
	circuitID := privacyzk.CircuitID(request.CircuitID)
	r1cs, err := p.Artifacts.AuditFieldR1CS(circuitID)
	if err != nil {
		return nil, err
	}
	provingKey, err := p.Artifacts.AuditFieldProvingKey(circuitID)
	if err != nil {
		return nil, err
	}
	proof, err := p.Runner.ProveAuditField(r1cs, provingKey, full)
	if err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if _, err := proof.WriteTo(&encoded); err != nil {
		return nil, err
	}
	return NewAuditFieldProofResponse(request, encoded.Bytes())
}

func NewAuditFieldProofRequest(prepared *privacyaudit.Prepared, full witness.Witness) (*AuditFieldProofRequest, error) {
	if prepared == nil || full == nil {
		return nil, fmt.Errorf("prepared audit transaction and full witness are required")
	}
	inputs := prepared.PublicInputs()
	publicBytes := fieldBytes(inputs)
	if err := validateWitnessPublicInputs(full, inputs); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if _, err := full.WriteTo(&encoded); err != nil {
		return nil, fmt.Errorf("encode audit full witness: %w", err)
	}
	artifactHash := prepared.Snapshot().ArtifactHash
	request := AuditFieldProofRequest{
		Version:      AuditFieldProofRequestVersion,
		CircuitSetID: privacyzk.AuditFieldCircuitSetID,
		CircuitID:    auditCircuitID(prepared.Kind()),
		ArtifactHash: bytes.Clone(artifactHash[:]),
		PublicInputs: publicBytes,
		Witness:      encoded.Bytes(),
	}
	if err := ValidateAuditFieldProofRequest(request); err != nil {
		return nil, err
	}
	return &request, nil
}

// DecodeAuditFieldProofWitness validates the wire framing and proves that the
// opaque full witness has exactly the supplied final PI23 as its public part.
// It is used by proverd before acquiring any circuit-specific witness data.
func DecodeAuditFieldProofWitness(request AuditFieldProofRequest) (witness.Witness, error) {
	inputs, err := validateAuditFieldProofRequest(request)
	if err != nil {
		return nil, err
	}
	full, err := witness.New(ecc.BN254.ScalarField())
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(request.Witness)
	if _, err := full.ReadFrom(reader); err != nil || reader.Len() != 0 {
		return nil, fmt.Errorf("invalid audit full witness")
	}
	if err := validateWitnessPublicInputs(full, inputs); err != nil {
		return nil, err
	}
	return full, nil
}

func ValidateAuditFieldProofRequest(request AuditFieldProofRequest) error {
	full, err := DecodeAuditFieldProofWitness(request)
	if full != nil {
		defer clearAuditWitness(full)
	}
	return err
}

func NewAuditFieldProofResponse(request AuditFieldProofRequest, proof []byte) (*AuditFieldProofResponse, error) {
	if err := ValidateAuditFieldProofRequest(request); err != nil {
		return nil, err
	}
	if err := privacyzk.ValidateCanonicalProofBN254(proof); err != nil {
		return nil, err
	}
	return &AuditFieldProofResponse{
		Version:      AuditFieldProofResponseVersion,
		CircuitSetID: request.CircuitSetID,
		CircuitID:    request.CircuitID,
		ArtifactHash: bytes.Clone(request.ArtifactHash),
		PublicInputs: cloneByteSlices(request.PublicInputs),
		Proof:        bytes.Clone(proof),
	}, nil
}

func ValidateAuditFieldProofResponseFraming(request AuditFieldProofRequest, response AuditFieldProofResponse) error {
	if response.Version != AuditFieldProofResponseVersion || response.CircuitSetID != request.CircuitSetID || response.CircuitID != request.CircuitID ||
		!bytes.Equal(response.ArtifactHash, request.ArtifactHash) || len(response.PublicInputs) != len(request.PublicInputs) {
		return fmt.Errorf("audit proof response framing mismatch")
	}
	for i := range request.PublicInputs {
		if !bytes.Equal(response.PublicInputs[i], request.PublicInputs[i]) {
			return fmt.Errorf("audit proof response framing mismatch")
		}
	}
	if err := privacyzk.ValidateCanonicalProofBN254(response.Proof); err != nil {
		return err
	}
	return nil
}

// ValidateAuditFieldProofResponse rejects replay across any prepared
// namespace and then performs a local verification against the same final
// PI23 and exact local artifact identity.
func ValidateAuditFieldProofResponse(prepared *privacyaudit.Prepared, registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity, response AuditFieldProofResponse) error {
	if prepared == nil {
		return fmt.Errorf("prepared audit transaction is required")
	}
	if response.Version != AuditFieldProofResponseVersion || response.CircuitSetID != privacyzk.AuditFieldCircuitSetID || response.CircuitID != auditCircuitID(prepared.Kind()) {
		return fmt.Errorf("audit proof response identity mismatch")
	}
	snapshot := prepared.Snapshot()
	if !bytes.Equal(response.ArtifactHash, snapshot.ArtifactHash[:]) {
		return fmt.Errorf("audit proof response artifact identity mismatch")
	}
	inputs := prepared.PublicInputs()
	if !equalFieldBytes(response.PublicInputs, inputs) {
		return fmt.Errorf("audit proof response final public inputs mismatch")
	}
	return prepared.VerifyProofForArtifact(registry, identity, snapshot.ArtifactHash, response.Proof)
}

// ProvePreparedAuditField completes the only supported remote v2 flow:
// immutable preparation -> explicitly trusted proverd -> local VK/PI23
// verification -> v2 message. It always clears r and the witness boundary
// after the attempt. A byte-identical retransmission uses the resulting
// message bytes; it never reuses a prepared object for a second proving run.
// AuditSnapshotSource obtains a fresh, independently authenticated v2
// namespace. It is invoked immediately before prove and again after the
// response, so activation, artifact replacement, and expiry cannot race a
// prepared r/nonce into message construction.
type AuditSnapshotSource func(context.Context) (privacyaudit.Snapshot, error)

func ProvePreparedAuditField(ctx context.Context, client HTTPProverClient, prepared *privacyaudit.Prepared, full witness.Witness, snapshots AuditSnapshotSource, registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity) (sdk.Msg, error) {
	return ProvePreparedAuditFieldAt(ctx, client, prepared, full, snapshots, registry, identity, time.Now)
}

func ProvePreparedAuditFieldAt(ctx context.Context, client HTTPProverClient, prepared *privacyaudit.Prepared, full witness.Witness, snapshots AuditSnapshotSource, registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity, now func() time.Time) (sdk.Msg, error) {
	if prepared == nil {
		return nil, fmt.Errorf("prepared audit transaction is required")
	}
	if full == nil || snapshots == nil {
		return nil, fmt.Errorf("audit full witness and fresh snapshot source are required")
	}
	if now == nil {
		now = time.Now
	}
	defer prepared.Clear()
	defer clearAuditWitness(full)
	current, err := snapshots(ctx)
	if err != nil {
		return nil, err
	}
	if !prepared.ValidFor(current, now()) {
		return nil, fmt.Errorf("prepared audit transaction is stale before proving")
	}
	request, err := NewAuditFieldProofRequest(prepared, full)
	if err != nil {
		return nil, err
	}
	defer clear(request.Witness)
	response, err := client.ProveAuditField(ctx, *request)
	if err != nil {
		return nil, err
	}
	current, err = snapshots(ctx)
	if err != nil {
		return nil, err
	}
	if !prepared.ValidFor(current, now()) {
		return nil, fmt.Errorf("prepared audit transaction is stale after proving")
	}
	if err := ValidateAuditFieldProofResponseForSnapshot(prepared, current, registry, identity, *response); err != nil {
		return nil, err
	}
	return prepared.BuildMessage(response.Proof)
}

func ValidateAuditFieldProofResponseForSnapshot(prepared *privacyaudit.Prepared, snapshot privacyaudit.Snapshot, registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity, response AuditFieldProofResponse) error {
	if prepared == nil || !prepared.ValidFor(snapshot, time.Now()) {
		return fmt.Errorf("prepared audit transaction is stale")
	}
	return ValidateAuditFieldProofResponse(prepared, registry, identity, response)
}

func DecodeAuditFieldProofRequestJSON(payload []byte) (*AuditFieldProofRequest, error) {
	var request AuditFieldProofRequest
	if err := decodeStrictJSON(payload, &request); err != nil {
		return nil, fmt.Errorf("invalid audit-field proof request JSON: %w", err)
	}
	return &request, nil
}

func DecodeAuditFieldProofResponseJSON(payload []byte) (*AuditFieldProofResponse, error) {
	var response AuditFieldProofResponse
	if err := decodeStrictJSON(payload, &response); err != nil {
		return nil, fmt.Errorf("invalid audit-field proof response JSON: %w", err)
	}
	return &response, nil
}

func validateAuditFieldProofRequest(request AuditFieldProofRequest) ([]auditfield.Field32, error) {
	if request.Version != AuditFieldProofRequestVersion || request.CircuitSetID != privacyzk.AuditFieldCircuitSetID || !isAuditCircuitID(request.CircuitID) {
		return nil, fmt.Errorf("unsupported audit-field proof request identity")
	}
	if len(request.ArtifactHash) != 32 || bytes.Equal(request.ArtifactHash, make([]byte, 32)) || len(request.Witness) == 0 {
		return nil, fmt.Errorf("audit-field proof request artifact hash and witness are required")
	}
	if len(request.PublicInputs) != 23 {
		return nil, fmt.Errorf("audit-field proof request requires final PI23")
	}
	result := make([]auditfield.Field32, len(request.PublicInputs))
	for i, raw := range request.PublicInputs {
		field, err := auditfield.ParseField32(raw)
		if err != nil {
			return nil, fmt.Errorf("audit-field public input %d is not canonical", i)
		}
		result[i] = field
	}
	return result, nil
}

func validateWitnessPublicInputs(full witness.Witness, expected []auditfield.Field32) error {
	public, err := full.Public()
	if err != nil {
		return fmt.Errorf("audit witness has no public part: %w", err)
	}
	values, ok := public.Vector().(bn254fr.Vector)
	if !ok || len(values) != len(expected) {
		return fmt.Errorf("audit witness public input count mismatch")
	}
	for i, value := range values {
		encoded := value.Bytes()
		if !bytes.Equal(encoded[:], expected[i][:]) {
			return fmt.Errorf("audit witness public input %d mismatch", i)
		}
	}
	return nil
}

func auditCircuitID(kind auditfield.Kind) string {
	ids := privacyzk.AuditFieldCircuitIDs()
	if kind < 1 || int(kind) > len(ids) {
		return ""
	}
	return string(ids[int(kind)-1])
}

func isAuditCircuitID(id string) bool {
	for _, candidate := range privacyzk.AuditFieldCircuitIDs() {
		if id == string(candidate) {
			return true
		}
	}
	return false
}

func fieldBytes(inputs []auditfield.Field32) [][]byte {
	result := make([][]byte, len(inputs))
	for i, input := range inputs {
		result[i] = input.Bytes()
	}
	return result
}

func equalFieldBytes(raw [][]byte, expected []auditfield.Field32) bool {
	if len(raw) != len(expected) {
		return false
	}
	for i, value := range raw {
		if !bytes.Equal(value, expected[i][:]) {
			return false
		}
	}
	return true
}

func cloneByteSlices(values [][]byte) [][]byte {
	result := make([][]byte, len(values))
	for i, value := range values {
		result[i] = bytes.Clone(value)
	}
	return result
}

func clearAuditWitness(full witness.Witness) {
	if full == nil {
		return
	}
	if values, ok := full.Vector().(bn254fr.Vector); ok {
		for i := range values {
			values[i].SetZero()
		}
	}
}

// ClearAuditWitness clears a full witness after a caller-owned local proving
// attempt. It exposes lifecycle hygiene without exposing witness internals.
func ClearAuditWitness(full witness.Witness) { clearAuditWitness(full) }
