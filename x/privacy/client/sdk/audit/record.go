package audit

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

const (
	auditTransitionStorePrefix byte = 0x13
	auditEpochStorePrefix      byte = 0x16
	auditCancellationPrefix    byte = 0x1d
	auditSequenceStorePrefix   byte = 0x1c
	maxTransitionRecordBytes        = 16 << 10
)

// AuditOrigin is the immutable public execution origin recorded with a
// transition.  It is provenance metadata, not an authority to execute a
// transfer.
type AuditOrigin struct {
	Kind   byte
	Anchor [32]byte
	Height uint64
}

// AuditOutput is one commitment created by an authenticated transition.
type AuditOutput struct {
	Commitment     auditfield.Field32
	LeafIndex      uint64
	LeafIndexFound bool
}

// TransparentAuditEffect is the public bank effect at a deposit or withdraw
// boundary.  Transfer and batch transitions have no such effect.
type TransparentAuditEffect struct {
	Kind   auditfield.Kind
	From   []byte
	To     []byte
	Denom  string
	Amount uint64
}

// VerifiedAuditRecord has no public constructor. Its zero value is invalid,
// and every byte/slice accessor returns a detached copy. Current values are
// created only by VerifyCollectedTransactions after binding an original
// successful transaction event to its historical key and Groth16 verifier.
type VerifiedAuditRecord struct{ record *auditRecord }

type auditRecord struct {
	wire                 []byte
	sequence, height     uint64
	origin               AuditOrigin
	kind                 auditfield.Kind
	network              [32]byte
	setID                string
	vkHash, schemaHash   [32]byte
	publicInputs         [23]auditfield.Field32
	proof                []byte
	keyID                [32]byte
	epoch                uint64
	suite                uint16
	publicKey, envelope  []byte
	auxHash, contextHash [32]byte
	cipherRoot           auditfield.CipherRoot
	root                 auditfield.Field32
	inputs               []auditfield.Field32
	outputs              []AuditOutput
	transparent          *TransparentAuditEffect
	sourceHeight         uint64
}

func (r VerifiedAuditRecord) valid() bool { return r.record != nil && r.record.sequence != 0 }

func (r VerifiedAuditRecord) Wire() []byte {
	if !r.valid() {
		return nil
	}
	return bytes.Clone(r.record.wire)
}
func (r VerifiedAuditRecord) Sequence() uint64 {
	if !r.valid() {
		return 0
	}
	return r.record.sequence
}
func (r VerifiedAuditRecord) Height() uint64 {
	if !r.valid() {
		return 0
	}
	return r.record.height
}
func (r VerifiedAuditRecord) Kind() auditfield.Kind {
	if !r.valid() {
		return 0
	}
	return r.record.kind
}
func (r VerifiedAuditRecord) Origin() AuditOrigin {
	if !r.valid() {
		return AuditOrigin{}
	}
	return r.record.origin
}
func (r VerifiedAuditRecord) Network() [32]byte {
	if !r.valid() {
		return [32]byte{}
	}
	return r.record.network
}
func (r VerifiedAuditRecord) PublicInputs() []auditfield.Field32 {
	if !r.valid() {
		return nil
	}
	return append([]auditfield.Field32(nil), r.record.publicInputs[:]...)
}
func (r VerifiedAuditRecord) Proof() []byte {
	if !r.valid() {
		return nil
	}
	return bytes.Clone(r.record.proof)
}
func (r VerifiedAuditRecord) KeyID() [32]byte {
	if !r.valid() {
		return [32]byte{}
	}
	return r.record.keyID
}
func (r VerifiedAuditRecord) Epoch() uint64 {
	if !r.valid() {
		return 0
	}
	return r.record.epoch
}
func (r VerifiedAuditRecord) PublicKey() []byte {
	if !r.valid() {
		return nil
	}
	return bytes.Clone(r.record.publicKey)
}
func (r VerifiedAuditRecord) Envelope() []byte {
	if !r.valid() {
		return nil
	}
	return bytes.Clone(r.record.envelope)
}
func (r VerifiedAuditRecord) CipherRoot() auditfield.CipherRoot {
	if !r.valid() {
		return auditfield.CipherRoot{}
	}
	return r.record.cipherRoot
}
func (r VerifiedAuditRecord) Inputs() []auditfield.Field32 {
	if !r.valid() {
		return nil
	}
	return append([]auditfield.Field32(nil), r.record.inputs...)
}
func (r VerifiedAuditRecord) Outputs() []AuditOutput {
	if !r.valid() {
		return nil
	}
	return append([]AuditOutput(nil), r.record.outputs...)
}
func (r VerifiedAuditRecord) TransparentEffect() (TransparentAuditEffect, bool) {
	if !r.valid() || r.record.transparent == nil {
		return TransparentAuditEffect{}, false
	}
	effect := *r.record.transparent
	effect.From, effect.To = bytes.Clone(effect.From), bytes.Clone(effect.To)
	return effect, true
}

type recordDecoder struct {
	raw []byte
	pos int
	err error
}

func (d *recordDecoder) take(size int) []byte {
	if d.err != nil {
		return make([]byte, size)
	}
	if size < 0 || size > len(d.raw)-d.pos {
		d.err = fmt.Errorf("truncated audit transition record")
		return make([]byte, size)
	}
	result := d.raw[d.pos : d.pos+size]
	d.pos += size
	return result
}
func (d *recordDecoder) u8() byte    { return d.take(1)[0] }
func (d *recordDecoder) u16() uint16 { return binary.BigEndian.Uint16(d.take(2)) }
func (d *recordDecoder) u64() uint64 { return binary.BigEndian.Uint64(d.take(8)) }
func (d *recordDecoder) lp(limit int) []byte {
	n := binary.BigEndian.Uint32(d.take(4))
	if uint64(n) > uint64(limit) {
		d.err = fmt.Errorf("audit transition field exceeds bound")
		return nil
	}
	return bytes.Clone(d.take(int(n)))
}
func (d *recordDecoder) hash() [32]byte {
	var result [32]byte
	copy(result[:], d.take(32))
	return result
}
func (d *recordDecoder) field() auditfield.Field32 {
	var result auditfield.Field32
	copy(result[:], d.take(32))
	return result
}
func (d *recordDecoder) origin() AuditOrigin {
	return AuditOrigin{Kind: d.u8(), Anchor: d.hash(), Height: d.u64()}
}
func (d *recordDecoder) done() error {
	if d.err != nil {
		return d.err
	}
	if d.pos != len(d.raw) {
		return fmt.Errorf("trailing audit transition record bytes")
	}
	return nil
}

func parseAuditRecord(raw []byte) (*auditRecord, error) {
	if len(raw) == 0 || len(raw) > maxTransitionRecordBytes {
		return nil, fmt.Errorf("audit transition record exceeds 16KiB")
	}
	d := &recordDecoder{raw: raw}
	if d.u16() != 2 {
		return nil, fmt.Errorf("unsupported audit transition record version")
	}
	record := &auditRecord{
		wire: bytes.Clone(raw), sequence: d.u64(), height: d.u64(), origin: d.origin(), kind: auditfield.Kind(d.u8()), network: d.hash(),
		setID: string(d.lp(64)), vkHash: d.hash(), schemaHash: d.hash(),
	}
	if d.u8() != 23 {
		return nil, fmt.Errorf("audit transition requires PI23")
	}
	for i := range record.publicInputs {
		record.publicInputs[i] = d.field()
	}
	record.proof = d.lp(4096)
	record.keyID = d.hash()
	record.epoch = d.u64()
	record.suite = d.u16()
	record.publicKey = bytes.Clone(d.take(64))
	record.envelope = d.lp(5808)
	record.auxHash, record.contextHash = d.hash(), d.hash()
	record.cipherRoot = auditfield.CipherRoot{Left: d.field(), Right: d.field()}
	record.root = d.field()
	inputCount := d.u8()
	if inputCount > 16 {
		return nil, fmt.Errorf("audit transition input capacity exceeded")
	}
	for i := byte(0); i < inputCount; i++ {
		if d.u8() != i {
			return nil, fmt.Errorf("audit transition input slot mismatch")
		}
		record.inputs = append(record.inputs, d.field())
	}
	outputCount := d.u8()
	if outputCount > 32 {
		return nil, fmt.Errorf("audit transition output capacity exceeded")
	}
	for i := byte(0); i < outputCount; i++ {
		if d.u8() != i {
			return nil, fmt.Errorf("audit transition output slot mismatch")
		}
		record.outputs = append(record.outputs, AuditOutput{Commitment: d.field(), LeafIndex: d.u64(), LeafIndexFound: true})
	}
	switch d.u8() {
	case 0:
	case 1:
		kind := auditfield.Kind(d.u8())
		from, to, denom := d.lp(255), d.lp(255), string(d.lp(128))
		amount := d.field()
		value, err := fieldUint64(amount)
		if err != nil {
			return nil, err
		}
		record.transparent = &TransparentAuditEffect{Kind: kind, From: from, To: to, Denom: denom, Amount: value}
	default:
		return nil, fmt.Errorf("invalid audit transition effect presence")
	}
	record.sourceHeight = d.u64()
	if err := d.done(); err != nil {
		return nil, err
	}
	if err := record.validate(); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *auditRecord) validate() error {
	if r == nil || r.sequence == 0 || r.height == 0 || r.height > math.MaxInt64 || r.origin.Height != r.height || r.sourceHeight != r.height {
		return fmt.Errorf("invalid audit transition sequence or height")
	}
	if r.origin.Kind < 1 || r.origin.Kind > 2 || len(r.inputs) > 16 || len(r.outputs) > 32 {
		return fmt.Errorf("invalid audit transition origin or capacity")
	}
	if err := r.kind.ValidateActiveCounts(uint8(len(r.inputs)), uint8(len(r.outputs))); err != nil {
		return err
	}
	if r.setID != auditfield.CircuitSetID || r.suite != auditfield.AuditSuite || r.epoch == 0 {
		return fmt.Errorf("invalid audit transition identity")
	}
	circuitID, err := auditCircuitID(r.kind)
	if err != nil {
		return err
	}
	schemaHash, err := privacyzk.PublicInputSchemaSHA256(string(circuitID))
	if err != nil || hex.EncodeToString(r.schemaHash[:]) != schemaHash {
		return fmt.Errorf("audit transition schema hash mismatch")
	}
	if err := privacyzk.ValidateCanonicalProofBN254(r.proof); err != nil {
		return fmt.Errorf("invalid audit transition proof framing: %w", err)
	}
	for i, field := range r.publicInputs {
		if _, err := auditfield.ParseField32(field[:]); err != nil {
			return fmt.Errorf("invalid audit transition PI[%d]: %w", i, err)
		}
	}
	context, err := auditfield.NewAuditContext(r.kind, r.publicInputs[:21])
	if err != nil {
		return err
	}
	key, err := auditfield.ParseAuditKey(r.publicKey, r.keyID[:])
	if err != nil || key.ID() != context.AuditKey().ID() || r.publicInputs[4] != auditfield.Field32FromUint64(r.epoch) {
		return fmt.Errorf("audit transition key does not match PI")
	}
	networkHi, networkLo := auditfield.DigestFields(r.network)
	if r.publicInputs[0] != networkHi || r.publicInputs[1] != networkLo {
		return fmt.Errorf("audit transition network does not match PI")
	}
	auxHi, auxLo := auditfield.DigestFields(r.auxHash)
	if r.publicInputs[19] != auxHi || r.publicInputs[20] != auxLo {
		return fmt.Errorf("audit transition auxiliary hash does not match PI")
	}
	if r.publicInputs[8] != r.root || r.publicInputs[9] != auditfield.Field32FromUint64(uint64(len(r.inputs))) || r.publicInputs[10] != auditfield.Field32FromUint64(uint64(len(r.outputs))) {
		return fmt.Errorf("audit transition shape does not match PI")
	}
	envelope, err := auditfield.ParseEnvelopeFrame(r.kind, uint8(len(r.inputs)), uint8(len(r.outputs)), r.envelope)
	if err != nil {
		return err
	}
	cipherRoot, err := auditfield.ComputeCipherRoot(context, envelope)
	if err != nil || cipherRoot != r.cipherRoot || r.publicInputs[21] != cipherRoot.Left || r.publicInputs[22] != cipherRoot.Right {
		return fmt.Errorf("audit transition cipher root mismatch")
	}
	contextHash, err := context.ContextHash(envelope.Nonce())
	if err != nil || contextHash != r.contextHash {
		return fmt.Errorf("audit transition context hash mismatch")
	}
	outputs := make([]auditfield.Field32, len(r.outputs))
	for i, output := range r.outputs {
		if output.LeafIndex >= 1<<32 || (i > 0 && output.LeafIndex != r.outputs[i-1].LeafIndex+1) {
			return fmt.Errorf("invalid audit transition output leaf order")
		}
		outputs[i] = output.Commitment
	}
	for _, values := range [][]auditfield.Field32{r.inputs, outputs} {
		seen := make(map[auditfield.Field32]bool, len(values))
		for _, value := range values {
			if value.IsZero() || seen[value] {
				return fmt.Errorf("duplicate or zero audit transition field")
			}
			seen[value] = true
		}
	}
	nullifierRoot, err := auditVectorRoot(r.kind, privacytypes.BatchVectorNullifierV1, r.inputs)
	if err != nil || nullifierRoot != r.publicInputs[11] {
		return fmt.Errorf("audit transition nullifier root mismatch")
	}
	commitmentRoot, err := auditVectorRoot(r.kind, privacytypes.BatchVectorCommitmentV1, outputs)
	if err != nil || commitmentRoot != r.publicInputs[12] {
		return fmt.Errorf("audit transition commitment root mismatch")
	}
	for _, index := range inactivePIIndices(r.kind) {
		if !r.publicInputs[index].IsZero() {
			return fmt.Errorf("inactive audit transition PI is nonzero")
		}
	}
	return r.validateTransparentEffect()
}

func (r *auditRecord) validateTransparentEffect() error {
	transparent := r.kind == auditfield.KindDeposit || r.kind == auditfield.KindWithdraw
	if transparent != (r.transparent != nil) {
		return fmt.Errorf("audit transition transparent effect presence mismatch")
	}
	if !transparent {
		return nil
	}
	effect := r.transparent
	if effect.Kind != r.kind || len(effect.From) == 0 || len(effect.From) > 255 || len(effect.To) == 0 || len(effect.To) > 255 || sdk.ValidateDenom(effect.Denom) != nil || sdk.VerifyAddressFormat(effect.From) != nil || sdk.VerifyAddressFormat(effect.To) != nil || (r.kind == auditfield.KindWithdraw && effect.Amount == 0) || r.publicInputs[15] != auditfield.Field32FromUint64(effect.Amount) || r.publicInputs[16].IsZero() {
		return fmt.Errorf("invalid audit transition transparent effect")
	}
	module := authtypes.NewModuleAddress(privacytypes.ModuleName)
	if r.kind == auditfield.KindDeposit && (!bytes.Equal(effect.To, module) || bytes.Equal(effect.From, module)) {
		return fmt.Errorf("invalid audit transition deposit endpoints")
	}
	if r.kind == auditfield.KindWithdraw && (!bytes.Equal(effect.From, module) || bytes.Equal(effect.To, module)) {
		return fmt.Errorf("invalid audit transition withdraw endpoints")
	}
	target := effect.From
	if r.kind == auditfield.KindWithdraw {
		target = effect.To
	}
	digest, err := auditfield.PublicTargetDigest(r.kind, target)
	if err != nil {
		return err
	}
	targetHi, targetLo := auditfield.DigestFields(digest)
	if r.publicInputs[17] != targetHi || r.publicInputs[18] != targetLo {
		return fmt.Errorf("audit transition target does not match PI")
	}
	return nil
}

func inactivePIIndices(kind auditfield.Kind) []int {
	switch kind {
	case auditfield.KindDeposit:
		return []int{8, 11, 13, 14}
	case auditfield.KindWithdraw:
		return []int{12, 13, 14}
	case auditfield.KindTransfer2x2, auditfield.KindBatch16x32:
		return []int{15, 16, 17, 18}
	default:
		return nil
	}
}

func auditCircuitID(kind auditfield.Kind) (privacyzk.CircuitID, error) {
	ids := privacyzk.AuditFieldCircuitIDs()
	if kind < auditfield.KindDeposit || kind > auditfield.KindBatch16x32 {
		return "", fmt.Errorf("invalid audit transition kind")
	}
	return ids[int(kind)-1], nil
}

func auditVectorRoot(kind auditfield.Kind, label privacytypes.BatchVectorKindV1, values []auditfield.Field32) (auditfield.Field32, error) {
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
	return auditfield.ParseField32(root.FillBytes(make([]byte, 32)))
}

func fieldUint64(field auditfield.Field32) (uint64, error) {
	for _, value := range field[:24] {
		if value != 0 {
			return 0, fmt.Errorf("audit field exceeds uint64")
		}
	}
	return binary.BigEndian.Uint64(field[24:]), nil
}

func storeSequenceKey(prefix byte, sequence uint64) []byte {
	key := make([]byte, 9)
	key[0] = prefix
	binary.BigEndian.PutUint64(key[1:], sequence)
	return key
}
