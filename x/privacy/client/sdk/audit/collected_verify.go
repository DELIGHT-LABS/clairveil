package audit

import (
	"context"
	"crypto/sha256"
	"fmt"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// CollectedProofVerifier pins proof verification to the circuit registry and
// identity that were valid at the transaction height.
type CollectedProofVerifier interface {
	VerifyCollectedProof(context.Context, uint64, auditfield.Kind, [][]byte, []byte) error
}

// HistoricalRegistryVerifier is the production adapter for the existing
// Groth16 verifier. Resolve must not substitute a current registry/identity
// for the requested historical height.
type HistoricalRegistryVerifier struct {
	Resolve func(context.Context, uint64) (*privacyzk.ArtifactRegistry, *privacytypes.CircuitSetIdentity, error)
}

func (v HistoricalRegistryVerifier) VerifyCollectedProof(ctx context.Context, height uint64, kind auditfield.Kind, pi [][]byte, proof []byte) error {
	if v.Resolve == nil {
		return fmt.Errorf("historical circuit resolver is required")
	}
	registry, identity, err := v.Resolve(ctx, height)
	if err != nil {
		return err
	}
	if registry == nil || identity == nil {
		return fmt.Errorf("historical circuit verifier is unavailable at height %d", height)
	}
	return VerifyFinalProof(kind, pi, proof, registry, identity)
}

// VerifyCollectedTransactions reconstructs exactly the proof-bound material
// from original successful messages. It creates no replay or persistent
// transition record; VerifiedAuditRecord remains only the in-process gate used
// by the established decryption and lineage implementation.
func VerifyCollectedTransactions(ctx context.Context, network [32]byte, rows []CollectedAuditTx, verifier CollectedProofVerifier) ([]VerifiedAuditRecord, error) {
	if network == ([32]byte{}) || verifier == nil {
		return nil, fmt.Errorf("network and historical proof verifier are required")
	}
	verified := make([]VerifiedAuditRecord, len(rows))
	for i, row := range rows {
		record, pi, err := collectedRecord(network, row)
		if err != nil {
			return nil, fmt.Errorf("collected transition %d: %w", row.GlobalSequence, err)
		}
		if err := verifier.VerifyCollectedProof(ctx, row.Height, row.Kind(), pi, row.Message.Proof()); err != nil {
			return nil, fmt.Errorf("collected transition %d proof: %w", row.GlobalSequence, err)
		}
		verified[i] = VerifiedAuditRecord{record: record}
	}
	return verified, nil
}

func collectedRecord(network [32]byte, row CollectedAuditTx) (*auditRecord, [][]byte, error) {
	m := row.Message
	if row.Height == 0 || row.GlobalSequence == 0 || row.TxHash == ([32]byte{}) || row.Key.Epoch != m.Epoch() || row.Key.Key.ID() != m.KeyID() || row.Key.ActivationHeight > row.Height {
		return nil, nil, fmt.Errorf("message, event, and historical key are inconsistent")
	}
	inputs := m.Nullifiers()
	outputs := m.Outputs()
	commitments := make([]auditfield.Field32, len(outputs))
	for i := range outputs {
		var err error
		commitments[i], err = auditfield.ParseField32(outputs[i].Commitment)
		if err != nil {
			return nil, nil, err
		}
	}
	aux, auxBytes, err := privacytypes.AuditOutputsToAux(m.Kind(), outputs)
	if err != nil {
		return nil, nil, err
	}
	point := row.Key.Key.Point().Bytes()
	input := PrepareInput{
		Snapshot: Snapshot{Network: network, Epoch: m.Epoch(), Key: row.Key.Key, StateHeight: int64(row.Height), ArtifactHash: [32]byte{1}},
		Kind:     m.Kind(), Creator: m.Creator(), Recipient: m.Recipient(), ExpiresAtUnix: m.Expiry(), Outputs: outputs,
	}
	if len(inputs) > 0 {
		input.Root = m.Root().Bytes()
		input.Inputs = make([][]byte, len(inputs))
		for i := range inputs {
			input.Inputs[i] = inputs[i].Bytes()
		}
	}
	var transparent *TransparentAuditEffect
	if m.Kind() == auditfield.KindDeposit || m.Kind() == auditfield.KindWithdraw {
		coin := m.Coin()
		asset, err := privacyfield.CanonicalBytesFromBigInt(privacytypes.ComputeAssetIDV1(coin.Denom))
		if err != nil {
			return nil, nil, err
		}
		input.Amount, input.PublicAsset = coin.String(), asset
		creator, err := sdk.AccAddressFromBech32(m.Creator())
		if err != nil {
			return nil, nil, err
		}
		module := authtypes.NewModuleAddress(privacytypes.ModuleName)
		if m.Kind() == auditfield.KindDeposit {
			input.Principal = creator
			transparent = &TransparentAuditEffect{Kind: m.Kind(), From: creator, To: module, Denom: coin.Denom, Amount: coin.Amount.Uint64()}
		} else {
			recipient, err := sdk.AccAddressFromBech32(m.Recipient())
			if err != nil {
				return nil, nil, err
			}
			input.Principal = recipient
			transparent = &TransparentAuditEffect{Kind: m.Kind(), From: module, To: recipient, Denom: coin.Denom, Amount: coin.Amount.Uint64()}
		}
	}
	pi, err := buildPublicInputs(input, inputs, commitments, aux, auxBytes, m.Root())
	if err != nil {
		return nil, nil, err
	}
	contextValue, err := auditfield.NewAuditContext(m.Kind(), pi[:21])
	if err != nil {
		return nil, nil, err
	}
	envelope, err := auditfield.ParseEnvelopeFrame(m.Kind(), uint8(len(inputs)), uint8(len(outputs)), m.Envelope())
	if err != nil {
		return nil, nil, err
	}
	cipherRoot, err := auditfield.ComputeCipherRoot(contextValue, envelope)
	if err != nil {
		return nil, nil, err
	}
	pi[21], pi[22] = cipherRoot.Left, cipherRoot.Right
	contextHash, err := contextValue.ContextHash(envelope.Nonce())
	if err != nil {
		return nil, nil, err
	}
	auxHash := sha256.Sum256(append([]byte(auditfield.AuditAuxDomain), auxBytes...))
	recordOutputs := make([]AuditOutput, len(commitments))
	for i := range commitments {
		// Global Merkle positions are not needed for C -> Cin lineage and are
		// not present in the execution event. Never synthesize one from the
		// output slot or from a scan row.
		recordOutputs[i] = AuditOutput{Commitment: commitments[i]}
	}
	record := &auditRecord{
		sequence: row.GlobalSequence, height: row.Height,
		origin: AuditOrigin{Kind: 1, Anchor: row.TxHash, Height: row.Height}, kind: m.Kind(), network: network,
		setID: auditfield.CircuitSetID, publicInputs: pi, proof: m.Proof(), keyID: m.KeyID(), epoch: m.Epoch(), suite: auditfield.AuditSuite,
		publicKey: point[:], envelope: m.Envelope(), auxHash: auxHash, contextHash: contextHash, cipherRoot: cipherRoot,
		root: m.Root(), inputs: inputs, outputs: recordOutputs, transparent: transparent, sourceHeight: row.Height,
	}
	rawPI := make([][]byte, len(pi))
	for i := range pi {
		rawPI[i] = pi[i].Bytes()
	}
	return record, rawPI, nil
}
