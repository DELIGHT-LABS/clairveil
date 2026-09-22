// Package audit owns the client-side preparation boundary for the audit-field
// v2 messages. It deliberately does not treat a node query as authenticated
// provenance: callers must obtain a Snapshot from a trusted chain view and
// re-check it immediately before proving and broadcasting.
package audit

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Snapshot is the complete public namespace of a prepared v2 transaction.
// ArtifactHash is the canonical SHA-256 of the active artifact manifest, not
// an operator supplied label. A change to any field invalidates preparation.
type Snapshot struct {
	Network      [32]byte
	Epoch        uint64
	Key          auditfield.AuditKey
	StateHeight  int64
	ArtifactHash [32]byte
}

func (s Snapshot) Validate() error {
	if s.Epoch == 0 || s.StateHeight < 0 {
		return fmt.Errorf("invalid audit key snapshot")
	}
	if s.Network == ([32]byte{}) || s.ArtifactHash == ([32]byte{}) {
		return fmt.Errorf("audit key snapshot requires network and artifact identities")
	}
	point := s.Key.Point().Bytes()
	key, err := auditfield.ParseAuditKey(point, s.Key.IDBytes())
	if err != nil || key.ID() != s.Key.ID() {
		return fmt.Errorf("invalid audit key snapshot")
	}
	return nil
}

// SameNamespace reports whether two snapshots can safely reuse a prepared
// message. StateHeight intentionally does not participate: a newer query with
// the same network/key/artifacts may retransmit the exact same bytes.
func (s Snapshot) SameNamespace(other Snapshot) bool {
	return s.Network == other.Network && s.Epoch == other.Epoch &&
		s.Key.ID() == other.Key.ID() && s.ArtifactHash == other.ArtifactHash
}

// PrepareInput contains only data that is either committed by the final
// message or is a private plaintext relation witness. For deposit and
// withdraw, Principal is respectively the canonical creator and recipient.
// It is not a fee payer or a funder override.
type PrepareInput struct {
	Snapshot      Snapshot
	Kind          auditfield.Kind
	Creator       string
	Recipient     string
	Amount        string
	ExpiresAtUnix int64
	Root          []byte
	Inputs        [][]byte
	Outputs       []*privacyv2.OutputEffect
	PublicAsset   []byte
	Principal     []byte
	Plaintext     []auditfield.Field32
}

// Prepared owns one fresh r/nonce pair. It is valid only for byte-identical
// retransmission. Clear must be called after proving, cancellation or a
// failed attempt; exported accessors return defensive copies.
type Prepared struct {
	snapshot  Snapshot
	kind      auditfield.Kind
	context   auditfield.AuditContext
	root      auditfield.CipherRoot
	envelope  auditfield.EnvelopeFrame
	witness   auditfield.EncryptionWitness
	pi        [23]auditfield.Field32
	creator   string
	recipient string
	amount    string
	expires   int64
	inputs    []auditfield.Field32
	outputs   []*privacyv2.OutputEffect
	cleared   bool
}

func Prepare(input PrepareInput) (*Prepared, error) {
	if err := input.Snapshot.Validate(); err != nil {
		return nil, err
	}
	if input.ExpiresAtUnix <= 0 || input.Creator == "" {
		return nil, fmt.Errorf("canonical creator and positive expiry are required")
	}
	if err := input.Kind.ValidateActiveCounts(uint8(len(input.Inputs)), uint8(len(input.Outputs))); err != nil {
		return nil, err
	}
	if (input.Kind == auditfield.KindDeposit || input.Kind == auditfield.KindWithdraw) && (input.Amount == "" || len(input.PublicAsset) != auditfield.FieldSize || len(input.Principal) == 0) {
		return nil, fmt.Errorf("transparent audit transaction requires amount, asset and principal")
	}
	if input.Kind == auditfield.KindWithdraw && input.Recipient == "" {
		return nil, fmt.Errorf("withdraw recipient is required")
	}
	if input.Kind != auditfield.KindWithdraw && input.Recipient != "" {
		return nil, fmt.Errorf("recipient is only valid for withdraw")
	}

	inputs, err := parseUniqueFields(input.Inputs, "input")
	if err != nil {
		return nil, err
	}
	var root auditfield.Field32
	if len(inputs) != 0 {
		root, err = auditfield.ParseField32(input.Root)
		if err != nil || root.IsZero() {
			return nil, fmt.Errorf("nonzero canonical merkle root is required")
		}
	} else if len(input.Root) != 0 {
		return nil, fmt.Errorf("deposit must not carry a merkle root")
	}

	aux, auxBytes, err := privacytypes.AuditOutputsToAux(input.Kind, input.Outputs)
	if err != nil {
		return nil, err
	}
	outputs := make([]auditfield.Field32, len(aux))
	for i, output := range input.Outputs {
		outputs[i], err = auditfield.ParseField32(output.Commitment)
		if err != nil {
			return nil, err
		}
	}

	pi, err := buildPublicInputs(input, inputs, outputs, aux, auxBytes, root)
	if err != nil {
		return nil, err
	}
	context, err := auditfield.NewAuditContext(input.Kind, pi[:21])
	if err != nil {
		return nil, err
	}
	plain, err := auditfield.NewAuditPlain(input.Kind, input.Plaintext)
	if err != nil {
		return nil, err
	}
	envelope, rootHash, encryptionWitness, err := auditfield.EncryptAuditForProver(context, plain)
	if err != nil {
		return nil, err
	}
	pi[21], pi[22] = rootHash.Left, rootHash.Right
	prepared := &Prepared{
		snapshot: input.Snapshot, kind: input.Kind, context: context, root: rootHash,
		envelope: envelope, witness: encryptionWitness, pi: pi, creator: input.Creator,
		recipient: input.Recipient, amount: input.Amount, expires: input.ExpiresAtUnix,
		inputs: inputs, outputs: cloneOutputs(input.Outputs),
	}
	return prepared, nil
}

func buildPublicInputs(input PrepareInput, inputs, outputs []auditfield.Field32, aux []auditfield.AuxOutput, auxBytes []byte, root auditfield.Field32) ([23]auditfield.Field32, error) {
	var pi [23]auditfield.Field32
	pi[0], pi[1] = auditfield.DigestFields(input.Snapshot.Network)
	pi[2], pi[3] = auditfield.DigestFields(input.Snapshot.Key.ID())
	pi[4] = auditfield.Field32FromUint64(input.Snapshot.Epoch)
	x, y, err := input.Snapshot.Key.Point().Coordinates()
	if err != nil {
		return pi, err
	}
	pi[5], pi[6] = x, y
	pi[7] = auditfield.Field32FromUint64(uint64(input.ExpiresAtUnix))
	pi[8] = root
	pi[9], pi[10] = auditfield.Field32FromUint64(uint64(len(inputs))), auditfield.Field32FromUint64(uint64(len(outputs)))
	pi[11], err = vectorRoot(input.Kind, privacytypes.BatchVectorNullifierV1, inputs)
	if err != nil {
		return pi, err
	}
	pi[12], err = vectorRoot(input.Kind, privacytypes.BatchVectorCommitmentV1, outputs)
	if err != nil {
		return pi, err
	}
	if err := disclosureRoots(input.Kind, input.Outputs, aux, &pi); err != nil {
		return pi, err
	}
	if input.Kind == auditfield.KindDeposit || input.Kind == auditfield.KindWithdraw {
		coin, err := parsePositiveCoin(input.Amount)
		if err != nil {
			return pi, err
		}
		asset, err := auditfield.ParseField32(input.PublicAsset)
		if err != nil || asset.IsZero() {
			return pi, fmt.Errorf("canonical nonzero public asset is required")
		}
		digest, err := auditfield.PublicTargetDigest(input.Kind, input.Principal)
		if err != nil {
			return pi, err
		}
		pi[15], pi[16] = auditfield.Field32(coin.Bytes32()), asset
		pi[17], pi[18] = auditfield.DigestFields(digest)
	}
	h := sha256.Sum256(append([]byte(auditfield.AuditAuxDomain), auxBytes...))
	pi[19], pi[20] = auditfield.DigestFields(h)
	return pi, nil
}

func parsePositiveCoin(value string) (privacyamount.Amount128, error) {
	coin, err := sdk.ParseCoinNormalized(value)
	if err != nil {
		return privacyamount.Amount128{}, err
	}
	if !coin.IsPositive() || coin.String() != value || coin.Amount.BigInt().BitLen() > privacyamount.BitLength {
		return privacyamount.Amount128{}, fmt.Errorf("audit amount must be canonical positive uint128 coin")
	}
	return privacyamount.Parse(coin.Amount.String())
}

func parseUniqueFields(raw [][]byte, name string) ([]auditfield.Field32, error) {
	values := make([]auditfield.Field32, len(raw))
	seen := make(map[auditfield.Field32]struct{}, len(raw))
	for i, item := range raw {
		field, err := auditfield.ParseField32(item)
		if err != nil || field.IsZero() {
			return nil, fmt.Errorf("%s %d must be canonical and nonzero", name, i)
		}
		if _, exists := seen[field]; exists {
			return nil, fmt.Errorf("duplicate %s", name)
		}
		seen[field] = struct{}{}
		values[i] = field
	}
	return values, nil
}

func vectorRoot(kind auditfield.Kind, label privacytypes.BatchVectorKindV1, values []auditfield.Field32) (auditfield.Field32, error) {
	if len(values) == 0 {
		return auditfield.Field32{}, nil
	}
	capacity := len(values)
	if kind == auditfield.KindBatch16x32 {
		capacity = 32
		if label == privacytypes.BatchVectorNullifierV1 {
			capacity = 16
		}
	}
	vector := make([]*big.Int, capacity)
	for i := range vector {
		vector[i] = new(big.Int)
		if i < len(values) {
			vector[i].SetBytes(values[i][:])
		}
	}
	root, err := privacytypes.ComputeBatchVectorRootV1(label, uint32(len(values)), vector)
	if err != nil {
		return auditfield.Field32{}, err
	}
	return auditfield.ParseField32(root.FillBytes(make([]byte, auditfield.FieldSize)))
}

func disclosureRoots(kind auditfield.Kind, outputs []*privacyv2.OutputEffect, aux []auditfield.AuxOutput, pi *[23]auditfield.Field32) error {
	switch kind {
	case auditfield.KindDeposit, auditfield.KindWithdraw:
		return nil
	case auditfield.KindTransfer2x2:
		if len(outputs) != 2 || len(aux) != 2 {
			return fmt.Errorf("transfer requires two outputs")
		}
		value := privacycrypto.MimcHash(privacytypes.DomainFieldV1("clairveil.audit.user-2x2.v1"), new(big.Int).SetUint64(uint64(outputs[0].UserPrivacyPolicy)), new(big.Int).SetBytes(aux[0].UserDigest[:]))
		root, err := auditfield.ParseField32(value.FillBytes(make([]byte, auditfield.FieldSize)))
		if err != nil {
			return err
		}
		pi[13], pi[14] = root, aux[0].SelfFullDigest
		return nil
	case auditfield.KindBatch16x32:
		policies := make([]uint32, 32)
		digests := make([]*big.Int, 32)
		fulls := make([]auditfield.Field32, len(aux))
		for i := range digests {
			digests[i] = new(big.Int)
			if i < len(aux) {
				policies[i] = outputs[i].UserPrivacyPolicy
				digests[i].SetBytes(aux[i].UserDigest[:])
				fulls[i] = aux[i].SelfFullDigest
			}
		}
		root, err := privacytypes.ComputeBatchUserDisclosureVectorRootV1(uint32(len(aux)), policies, digests)
		if err != nil {
			return err
		}
		pi[13], err = auditfield.ParseField32(root.FillBytes(make([]byte, auditfield.FieldSize)))
		if err != nil {
			return err
		}
		pi[14], err = vectorRoot(kind, privacytypes.BatchVectorFullDisclosureV1, fulls)
		return err
	default:
		return fmt.Errorf("unsupported audit kind")
	}
}

func (p *Prepared) Snapshot() Snapshot    { return p.snapshot }
func (p *Prepared) Kind() auditfield.Kind { return p.kind }
func (p *Prepared) PublicInputs() []auditfield.Field32 {
	return append([]auditfield.Field32(nil), p.pi[:]...)
}

// OutputEffects returns the exact output wire effects that were committed into
// the final PI. Callers that translate an existing normal-flow payload into
// an audit-field witness must compare against this value rather than rebuild
// a similar-looking output after the envelope has been created.
func (p *Prepared) OutputEffects() []*privacyv2.OutputEffect {
	if p == nil || p.cleared {
		return nil
	}
	return cloneOutputs(p.outputs)
}
func (p *Prepared) CipherRoot() auditfield.CipherRoot { return p.root }
func (p *Prepared) ExpiresAtUnix() int64              { return p.expires }

func (p *Prepared) EnvelopeBytes() ([]byte, error) {
	if p == nil || p.cleared {
		return nil, fmt.Errorf("prepared audit transaction is cleared")
	}
	return p.envelope.Bytes()
}

// OwnerIntent returns the only owner-signature digest accepted by the
// integrated withdraw/transfer/batch circuits. It includes the actual nonce
// and final cipher root, so changing key, expiry, artifact namespace or
// output requires re-encrypting and re-signing before proving.
func (p *Prepared) OwnerIntent() ([32]byte, error) {
	if p == nil || p.cleared {
		return [32]byte{}, fmt.Errorf("prepared audit transaction is cleared")
	}
	if p.kind != auditfield.KindWithdraw && p.kind != auditfield.KindTransfer2x2 && p.kind != auditfield.KindBatch16x32 {
		return [32]byte{}, fmt.Errorf("deposit does not have an owner intent signature")
	}
	t, err := p.context.T(p.envelope.Nonce())
	if err != nil {
		return [32]byte{}, err
	}
	fields := make([]*big.Int, 0, len(t)+3)
	fields = append(fields, privacytypes.DomainFieldV1(auditfield.AuditOwnerIntentDomain))
	for _, field := range t {
		fields = append(fields, new(big.Int).SetBytes(field[:]))
	}
	fields = append(fields, new(big.Int).SetBytes(p.root.Left[:]), new(big.Int).SetBytes(p.root.Right[:]))
	value := privacycrypto.MimcHash(fields...)
	var result [32]byte
	copy(result[:], value.FillBytes(make([]byte, 32)))
	return result, nil
}

// ProverScalarBE32 is an explicit, short-lived prover-boundary export. It is
// never included in messages, logs or generic JSON. The caller must clear the
// returned array after constructing its trusted local/proverd witness.
func (p *Prepared) ProverScalarBE32() ([32]byte, error) {
	if p == nil || p.cleared {
		return [32]byte{}, fmt.Errorf("prepared audit transaction is cleared")
	}
	return p.witness.ToProverWitnessBE32()
}

func (p *Prepared) ValidFor(snapshot Snapshot, now time.Time) bool {
	return p != nil && !p.cleared && snapshot.Validate() == nil && p.snapshot.SameNamespace(snapshot) && (now.IsZero() || now.Unix() < p.expires)
}

func (p *Prepared) Clear() {
	if p == nil || p.cleared {
		return
	}
	p.witness.Clear()
	p.cleared = true
	for i := range p.inputs {
		p.inputs[i] = auditfield.Field32{}
	}
	p.inputs = nil
	for i, output := range p.outputs {
		if output == nil {
			continue
		}
		clear(output.Commitment)
		clear(output.Ciphertext)
		clear(output.ViewTag)
		clear(output.UserDisclosureDigest)
		clear(output.UserDisclosureTargetPubkey)
		clear(output.UserDisclosurePayload)
		clear(output.SelfFullDisclosureDigest)
		clear(output.SelfViewDisclosurePayload)
		p.outputs[i] = nil
	}
	p.outputs = nil
	p.pi = [23]auditfield.Field32{}
	p.root = auditfield.CipherRoot{}
	p.creator, p.recipient, p.amount = "", "", ""
}

// BuildMessage binds a locally revalidated proof to the original immutable
// prepared bytes. The returned message is v2 only; existing v1 SDK messages
// remain untouched for their explicit legacy route.
func (p *Prepared) BuildMessage(proof []byte) (sdk.Msg, error) {
	if p == nil || p.cleared {
		return nil, fmt.Errorf("prepared audit transaction is cleared")
	}
	if err := privacyzk.ValidateCanonicalProofBN254(proof); err != nil {
		return nil, err
	}
	envelope, err := p.envelope.Bytes()
	if err != nil {
		return nil, err
	}
	id := p.snapshot.Key.IDBytes()
	auth := &privacyv2.AuditAuthorization{KeyId: id, Epoch: p.snapshot.Epoch, Envelope: envelope}
	switch p.kind {
	case auditfield.KindDeposit:
		return &privacyv2.MsgDeposit{Creator: p.creator, Amount: p.amount, Output: cloneOutputs(p.outputs)[0], Proof: bytes.Clone(proof), ExpiresAtUnix: p.expires, Audit: auth}, nil
	case auditfield.KindWithdraw:
		return &privacyv2.MsgWithdraw{Creator: p.creator, Amount: p.amount, Recipient: p.recipient, Root: p.pi[8].Bytes(), Nullifier: p.inputs[0].Bytes(), Proof: bytes.Clone(proof), ExpiresAtUnix: p.expires, Audit: auth}, nil
	case auditfield.KindTransfer2x2:
		return &privacyv2.MsgTransfer{Creator: p.creator, Root: p.pi[8].Bytes(), Nullifiers: fieldsBytes(p.inputs), Outputs: cloneOutputs(p.outputs), Proof: bytes.Clone(proof), ExpiresAtUnix: p.expires, Audit: auth}, nil
	case auditfield.KindBatch16x32:
		return &privacyv2.MsgBatchTransfer{Creator: p.creator, Root: p.pi[8].Bytes(), Nullifiers: fieldsBytes(p.inputs), Outputs: cloneOutputs(p.outputs), Proof: bytes.Clone(proof), ExpiresAtUnix: p.expires, Audit: auth}, nil
	default:
		return nil, fmt.Errorf("unsupported audit kind")
	}
}

// VerifyProof rechecks a prover response against the exact final PI23 and the
// snapshot's artifact identity before a message can be built or broadcast.
func (p *Prepared) VerifyProof(registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity, proof []byte) error {
	return p.VerifyProofForArtifact(registry, identity, p.snapshot.ArtifactHash, proof)
}

// VerifyProofForArtifact makes an artifact change explicit at the response
// boundary. A caller that observed a different manifest hash must discard the
// prepared r/nonce and start again instead of accepting an old response.
func (p *Prepared) VerifyProofForArtifact(registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity, artifactHash [32]byte, proof []byte) error {
	if p == nil || p.cleared || registry == nil || identity == nil {
		return fmt.Errorf("prepared audit proof verification requires registry and identity")
	}
	if artifactHash != p.snapshot.ArtifactHash {
		return fmt.Errorf("prepared audit artifact identity changed")
	}
	if err := privacyzk.ValidateCanonicalProofBN254(proof); err != nil {
		return err
	}
	ids := privacyzk.AuditFieldCircuitIDs()
	if p.kind < 1 || int(p.kind) > len(ids) {
		return fmt.Errorf("unsupported audit kind")
	}
	w, err := witness.New(ecc.BN254.ScalarField())
	if err != nil {
		return err
	}
	values := make(chan any, len(p.pi))
	for _, field := range p.pi {
		values <- new(big.Int).SetBytes(field[:])
	}
	close(values)
	if err := w.Fill(len(p.pi), 0, values); err != nil {
		return err
	}
	return registry.VerifyProof(ids[int(p.kind)-1], proof, w, identity)
}

func cloneOutputs(input []*privacyv2.OutputEffect) []*privacyv2.OutputEffect {
	output := make([]*privacyv2.OutputEffect, len(input))
	for i, value := range input {
		if value == nil {
			continue
		}
		output[i] = &privacyv2.OutputEffect{
			Commitment: bytes.Clone(value.Commitment), Ciphertext: bytes.Clone(value.Ciphertext), ViewTag: bytes.Clone(value.ViewTag),
			UserPrivacyPolicy: value.UserPrivacyPolicy, UserDisclosureMode: value.UserDisclosureMode,
			UserDisclosureDigest: bytes.Clone(value.UserDisclosureDigest), UserDisclosureTargetPubkey: bytes.Clone(value.UserDisclosureTargetPubkey),
			UserDisclosurePayload: bytes.Clone(value.UserDisclosurePayload), SelfFullDisclosureDigest: bytes.Clone(value.SelfFullDisclosureDigest), SelfViewDisclosurePayload: bytes.Clone(value.SelfViewDisclosurePayload),
		}
	}
	return output
}

func fieldsBytes(fields []auditfield.Field32) [][]byte {
	output := make([][]byte, len(fields))
	for i, field := range fields {
		output[i] = field.Bytes()
	}
	return output
}
