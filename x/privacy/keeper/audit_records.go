package keeper

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	auditEpochPrefix        byte = 0x16
	auditActivationPrefix   byte = 0x17
	auditActivePrefix       byte = 0x18
	auditCancellationPrefix byte = 0x1d
	auditHaltPrefix         byte = 0x1e
)

// auditOrigin is transient execution provenance for a verified transaction,
// and persistent provenance for audit-key lifecycle entries only.
type auditOrigin struct {
	Kind   byte
	Anchor [32]byte
	Height uint64
}

func (o auditOrigin) validate(genesis bool) error {
	if o.Height > math.MaxInt64 || o.Kind < 1 || o.Kind > 3 || (!genesis && o.Kind == 3) {
		return fmt.Errorf("invalid audit execution origin")
	}
	return nil
}

type auditOutputRef struct {
	Commitment auditfield.Field32
	LeafIndex  uint64
}

type auditTransparentEffect struct {
	Kind     auditfield.Kind
	From, To []byte
	Denom    string
	Amount   auditfield.Field32
}

// auditTransitionRecord is verifier-local data carried from proof validation
// to the atomic state apply. It is never encoded into consensus state.
type auditTransitionRecord struct {
	Height               uint64
	Origin               auditOrigin
	Kind                 auditfield.Kind
	Network              [32]byte
	SetID                string
	VKHash, SchemaHash   [32]byte
	PI                   [23]auditfield.Field32
	Proof                []byte
	KeyID                [32]byte
	Epoch                uint64
	Suite                uint16
	PK                   []byte
	Envelope             []byte
	AuxHash, ContextHash [32]byte
	CipherRoot           auditfield.CipherRoot
	Root                 auditfield.Field32
	Inputs               []auditfield.Field32
	Outputs              []auditOutputRef
	Transparent          *auditTransparentEffect
}

type auditKeyRecord struct {
	Epoch                              uint64
	KeyID                              [32]byte
	Suite                              uint16
	PK                                 []byte
	ActivationHeight, RegisteredHeight uint64
	Origin                             auditOrigin
	PoP                                []byte
}

type auditEncoder struct{ bytes.Buffer }

func (w *auditEncoder) u8(v byte) { w.WriteByte(v) }
func (w *auditEncoder) u16(v uint16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	w.Write(b[:])
}
func (w *auditEncoder) u64(v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	w.Write(b[:])
}
func (w *auditEncoder) origin(o auditOrigin) { w.u8(o.Kind); w.Write(o.Anchor[:]); w.u64(o.Height) }

type auditDecoder struct {
	raw []byte
	pos int
	err error
}

func (r *auditDecoder) take(n int) []byte {
	if r.err != nil || n < 0 || n > len(r.raw)-r.pos {
		r.err = fmt.Errorf("truncated audit key state")
		return make([]byte, n)
	}
	b := r.raw[r.pos : r.pos+n]
	r.pos += n
	return b
}
func (r *auditDecoder) u8() byte       { return r.take(1)[0] }
func (r *auditDecoder) u16() uint16    { return binary.BigEndian.Uint16(r.take(2)) }
func (r *auditDecoder) u64() uint64    { return binary.BigEndian.Uint64(r.take(8)) }
func (r *auditDecoder) hash() [32]byte { var h [32]byte; copy(h[:], r.take(32)); return h }
func (r *auditDecoder) origin() auditOrigin {
	return auditOrigin{Kind: r.u8(), Anchor: r.hash(), Height: r.u64()}
}
func (r *auditDecoder) done() error {
	if r.err != nil {
		return r.err
	}
	if r.pos != len(r.raw) {
		return fmt.Errorf("trailing audit key state bytes")
	}
	return nil
}

func auditCircuitID(kind auditfield.Kind) (zk.CircuitID, error) {
	ids := zk.AuditFieldCircuitIDs()
	if kind < 1 || kind > 4 {
		return "", fmt.Errorf("invalid audit kind")
	}
	return ids[int(kind)-1], nil
}

func auditVectorRoot(kind auditfield.Kind, label types.BatchVectorKindV1, values []auditfield.Field32) (auditfield.Field32, error) {
	if len(values) == 0 {
		return auditfield.Field32{}, nil
	}
	capacity := len(values)
	if kind == auditfield.KindBatch16x32 {
		capacity = 32
		if label == types.BatchVectorNullifierV1 {
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
	value, err := types.ComputeBatchVectorRootV1(label, uint32(len(values)), vector)
	if err != nil {
		return auditfield.Field32{}, err
	}
	return auditfield.ParseField32(value.FillBytes(make([]byte, 32)))
}

func auditEpochKey(prefix byte, epoch uint64) []byte {
	b := make([]byte, 9)
	b[0] = prefix
	binary.BigEndian.PutUint64(b[1:], epoch)
	return b
}

func (v *auditKeyRecord) validate(network [32]byte, initialHeight uint64) error {
	if v == nil || v.Epoch == 0 || v.Suite != 2 || v.ActivationHeight > math.MaxInt64 || v.RegisteredHeight > math.MaxInt64 || v.Origin.Height != v.RegisteredHeight {
		return fmt.Errorf("invalid audit key record")
	}
	if err := v.Origin.validate(true); err != nil {
		return err
	}
	if v.Origin.Kind == 3 {
		if initialHeight == 0 || v.ActivationHeight != initialHeight-1 || v.RegisteredHeight != initialHeight-1 || v.Epoch != 1 {
			return fmt.Errorf("invalid initial audit key")
		}
	} else if v.ActivationHeight <= v.RegisteredHeight {
		return fmt.Errorf("audit key activation must be future")
	}
	key, err := auditfield.ParseAuditKey(v.PK, v.KeyID[:])
	if err != nil {
		return err
	}
	pop, err := auditfield.ParsePoP96(v.PoP)
	if err != nil {
		return err
	}
	return auditfield.VerifyPoP96(key, network, v.Epoch, v.ActivationHeight, pop)
}

func encodeAuditKey(v *auditKeyRecord, network [32]byte, initialHeight uint64) ([]byte, error) {
	if err := v.validate(network, initialHeight); err != nil {
		return nil, err
	}
	w := &auditEncoder{}
	w.u16(1)
	w.u64(v.Epoch)
	w.Write(v.KeyID[:])
	w.u16(v.Suite)
	w.Write(v.PK)
	w.u64(v.ActivationHeight)
	w.u64(v.RegisteredHeight)
	w.origin(v.Origin)
	w.Write(v.PoP)
	return w.Bytes(), nil
}

func decodeAuditKey(raw []byte, network [32]byte, initialHeight uint64) (*auditKeyRecord, error) {
	r := &auditDecoder{raw: raw}
	if r.u16() != 1 {
		return nil, fmt.Errorf("unsupported audit key record version")
	}
	v := &auditKeyRecord{Epoch: r.u64(), KeyID: r.hash(), Suite: r.u16(), PK: append([]byte(nil), r.take(64)...), ActivationHeight: r.u64(), RegisteredHeight: r.u64(), Origin: r.origin(), PoP: append([]byte(nil), r.take(96)...)}
	if err := r.done(); err != nil {
		return nil, err
	}
	if err := v.validate(network, initialHeight); err != nil {
		return nil, err
	}
	return v, nil
}
