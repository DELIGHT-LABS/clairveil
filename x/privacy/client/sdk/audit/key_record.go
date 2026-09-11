package audit

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
)

// KeyOrigin is public chain-history metadata. It is not an authorization
// capability and is retained so a client can reject malformed epoch records.
type KeyOrigin struct {
	Kind   byte
	Anchor [32]byte
	Height uint64
}

// KeyRecord is the canonical v2 audit epoch record returned by Query.AuditKey.
// ParseKeyRecord verifies the exact state codec, deterministic KeyID and PoP.
type KeyRecord struct {
	Epoch            uint64
	Key              auditfield.AuditKey
	ActivationHeight uint64
	RegisteredHeight uint64
	Origin           KeyOrigin
	PossessionProof  auditfield.PoP96
}

// ParseKeyRecord accepts only the keeper's exact v2 state encoding. The
// network and initialHeight come from independently authenticated chain
// metadata; an unauthenticated RPC response alone is never provenance.
func ParseKeyRecord(raw []byte, network [32]byte, initialHeight uint64) (KeyRecord, error) {
	var result KeyRecord
	decoder := keyDecoder{raw: raw}
	if decoder.u16() != 1 {
		return result, fmt.Errorf("unsupported audit key record version")
	}
	result.Epoch = decoder.u64()
	var id [32]byte
	copy(id[:], decoder.take(32))
	if decoder.u16() != auditfield.AuditSuite {
		return result, fmt.Errorf("invalid audit key suite")
	}
	point := decoder.take(auditfield.PointSize)
	result.ActivationHeight = decoder.u64()
	result.RegisteredHeight = decoder.u64()
	result.Origin = KeyOrigin{Kind: decoder.u8()}
	copy(result.Origin.Anchor[:], decoder.take(32))
	result.Origin.Height = decoder.u64()
	pop, err := auditfield.ParsePoP96(decoder.take(96))
	if err != nil {
		return KeyRecord{}, err
	}
	if err := decoder.done(); err != nil {
		return KeyRecord{}, err
	}
	if result.Epoch == 0 || result.ActivationHeight > math.MaxInt64 || result.RegisteredHeight > math.MaxInt64 || result.Origin.Height != result.RegisteredHeight || result.Origin.Kind < 1 || result.Origin.Kind > 3 {
		return KeyRecord{}, fmt.Errorf("invalid audit key record")
	}
	if result.Origin.Kind == 3 {
		if initialHeight == 0 || result.Epoch != 1 || result.ActivationHeight != initialHeight-1 || result.RegisteredHeight != initialHeight-1 {
			return KeyRecord{}, fmt.Errorf("invalid initial audit key record")
		}
	} else if result.ActivationHeight <= result.RegisteredHeight {
		return KeyRecord{}, fmt.Errorf("audit key activation must be future")
	}
	key, err := auditfield.ParseAuditKey(point, id[:])
	if err != nil {
		return KeyRecord{}, err
	}
	if err := auditfield.VerifyPoP96(key, network, result.Epoch, result.ActivationHeight, pop); err != nil {
		return KeyRecord{}, fmt.Errorf("invalid audit key possession proof: %w", err)
	}
	result.Key, result.PossessionProof = key, pop
	return result, nil
}

func (r KeyRecord) Snapshot(network [32]byte, stateHeight int64, artifactHash [32]byte) (Snapshot, error) {
	snapshot := Snapshot{Network: network, Epoch: r.Epoch, Key: r.Key, StateHeight: stateHeight, ArtifactHash: artifactHash}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

type keyDecoder struct {
	raw []byte
	pos int
	err error
}

func (d *keyDecoder) take(size int) []byte {
	if d.err != nil {
		return make([]byte, size)
	}
	if size < 0 || size > len(d.raw)-d.pos {
		d.err = fmt.Errorf("truncated audit key record")
		return make([]byte, size)
	}
	result := d.raw[d.pos : d.pos+size]
	d.pos += size
	return result
}
func (d *keyDecoder) u8() byte    { return d.take(1)[0] }
func (d *keyDecoder) u16() uint16 { return binary.BigEndian.Uint16(d.take(2)) }
func (d *keyDecoder) u64() uint64 { return binary.BigEndian.Uint64(d.take(8)) }
func (d *keyDecoder) done() error {
	if d.err != nil {
		return d.err
	}
	if d.pos != len(d.raw) {
		return fmt.Errorf("trailing audit key record bytes")
	}
	return nil
}
