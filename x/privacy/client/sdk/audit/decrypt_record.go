package audit

import (
	"fmt"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
)

// AuditNote is an auditor-visible decrypted note fact.  It deliberately does
// not reuse legacy disclosure DTOs: an audit note includes the relation-only
// fields needed for C -> Cin -> N lineage, while legacy disclosure remains a
// recipient-scoped compatibility plane.
type AuditNote struct {
	Commitment auditfield.Field32
	Asset      auditfield.Field32
	Amount     uint64
	SpendX     auditfield.Field32
	SpendY     auditfield.Field32
	ViewX      auditfield.Field32
	ViewY      auditfield.Field32
}

// DecryptedAuditRecord has no public constructor.  It can only be returned
// from DecryptVerifiedRecord, so report builders cannot be fed arbitrary
// plaintext or unverified C/Cin values.
type DecryptedAuditRecord struct {
	record *VerifiedAuditRecord
	plain  []auditfield.Field32
	asset  auditfield.Field32
	inputs []auditfield.Field32 // Cin, in the consensus input slot order.
	output []AuditNote
}

func (r DecryptedAuditRecord) valid() bool {
	return r.record != nil && r.record.valid() && len(r.plain) != 0
}

func (r DecryptedAuditRecord) Record() VerifiedAuditRecord {
	if !r.valid() {
		return VerifiedAuditRecord{}
	}
	return *r.record
}
func (r DecryptedAuditRecord) Plaintext() []auditfield.Field32 {
	if !r.valid() {
		return nil
	}
	return append([]auditfield.Field32(nil), r.plain...)
}
func (r DecryptedAuditRecord) Asset() auditfield.Field32 {
	if !r.valid() {
		return auditfield.Field32{}
	}
	return r.asset
}
func (r DecryptedAuditRecord) InputCommitments() []auditfield.Field32 {
	if !r.valid() {
		return nil
	}
	return append([]auditfield.Field32(nil), r.inputs...)
}
func (r DecryptedAuditRecord) OutputNotes() []AuditNote {
	if !r.valid() {
		return nil
	}
	return append([]AuditNote(nil), r.output...)
}

// DecryptVerifiedRecord parses the already record-bound envelope and delegates
// ECDH/KDF/tag/root validation to crypto.DecryptAudit. A zero value or a
// transaction that has not passed VerifyCollectedTransactions is rejected.
func DecryptVerifiedRecord(secret privacycrypto.AuditSecretKey, record VerifiedAuditRecord) (DecryptedAuditRecord, error) {
	if !record.valid() {
		return DecryptedAuditRecord{}, fmt.Errorf("verified audit record is required")
	}
	context, err := auditfield.NewAuditContext(record.record.kind, record.record.publicInputs[:21])
	if err != nil {
		return DecryptedAuditRecord{}, err
	}
	envelope, err := auditfield.ParseEnvelopeFrame(record.record.kind, uint8(len(record.record.inputs)), uint8(len(record.record.outputs)), record.record.envelope)
	if err != nil {
		return DecryptedAuditRecord{}, err
	}
	plain, err := privacycrypto.DecryptAudit(secret, context, envelope, record.record.cipherRoot)
	if err != nil {
		return DecryptedAuditRecord{}, err
	}
	fields := plain.Fields()
	asset, inputs, output, err := decodeAuditPlain(record, fields)
	if err != nil {
		return DecryptedAuditRecord{}, err
	}
	copyRecord := record
	return DecryptedAuditRecord{record: &copyRecord, plain: fields, asset: asset, inputs: inputs, output: output}, nil
}

func decodeAuditPlain(record VerifiedAuditRecord, fields []auditfield.Field32) (auditfield.Field32, []auditfield.Field32, []AuditNote, error) {
	if !record.valid() || len(fields) == 0 || fields[0].IsZero() {
		return auditfield.Field32{}, nil, nil, fmt.Errorf("invalid decrypted audit plaintext")
	}
	asset := fields[0]
	newOutput := func(index int, commitment auditfield.Field32) (AuditNote, error) {
		if index+5 > len(fields) {
			return AuditNote{}, fmt.Errorf("truncated decrypted audit output")
		}
		amount, err := fieldUint64(fields[index])
		if err != nil {
			return AuditNote{}, err
		}
		return AuditNote{Commitment: commitment, Asset: asset, Amount: amount, SpendX: fields[index+1], SpendY: fields[index+2], ViewX: fields[index+3], ViewY: fields[index+4]}, nil
	}
	public := record.record.publicInputs
	switch record.record.kind {
	case auditfield.KindDeposit:
		if len(fields) != 6 || len(record.record.outputs) != 1 || public[16] != asset {
			return auditfield.Field32{}, nil, nil, fmt.Errorf("invalid decrypted deposit plaintext")
		}
		amount, err := fieldUint64(fields[1])
		if err != nil || amount == 0 || public[15] != auditfield.Field32FromUint64(amount) {
			return auditfield.Field32{}, nil, nil, fmt.Errorf("deposit plaintext amount does not match PI")
		}
		note, err := newOutput(1, record.record.outputs[0].Commitment)
		if err != nil {
			return auditfield.Field32{}, nil, nil, err
		}
		return asset, nil, []AuditNote{note}, nil
	case auditfield.KindWithdraw:
		if len(fields) != 2 || len(record.record.inputs) != 1 || public[16] != asset || fields[1].IsZero() {
			return auditfield.Field32{}, nil, nil, fmt.Errorf("invalid decrypted withdraw plaintext")
		}
		if _, err := fieldUint64(public[15]); err != nil || public[15].IsZero() {
			return auditfield.Field32{}, nil, nil, fmt.Errorf("invalid withdraw public amount")
		}
		return asset, []auditfield.Field32{fields[1]}, nil, nil
	case auditfield.KindTransfer2x2:
		if len(fields) != 13 || len(record.record.inputs) != 2 || len(record.record.outputs) != 2 || fields[1].IsZero() || fields[2].IsZero() {
			return auditfield.Field32{}, nil, nil, fmt.Errorf("invalid decrypted transfer plaintext")
		}
		outputs := make([]AuditNote, 2)
		for i := range outputs {
			var err error
			outputs[i], err = newOutput(3+5*i, record.record.outputs[i].Commitment)
			if err != nil {
				return auditfield.Field32{}, nil, nil, err
			}
		}
		return asset, []auditfield.Field32{fields[1], fields[2]}, outputs, nil
	case auditfield.KindBatch16x32:
		if len(fields) != 177 || len(record.record.inputs) == 0 || len(record.record.outputs) == 0 {
			return auditfield.Field32{}, nil, nil, fmt.Errorf("invalid decrypted batch plaintext")
		}
		inputs := make([]auditfield.Field32, len(record.record.inputs))
		for i := 0; i < 16; i++ {
			value := fields[1+i]
			if i < len(inputs) {
				if value.IsZero() {
					return auditfield.Field32{}, nil, nil, fmt.Errorf("zero active batch input commitment")
				}
				inputs[i] = value
			} else if !value.IsZero() {
				return auditfield.Field32{}, nil, nil, fmt.Errorf("nonzero inactive batch input commitment")
			}
		}
		outputs := make([]AuditNote, len(record.record.outputs))
		for i := 0; i < 32; i++ {
			index := 17 + 5*i
			if i < len(outputs) {
				var err error
				outputs[i], err = newOutput(index, record.record.outputs[i].Commitment)
				if err != nil {
					return auditfield.Field32{}, nil, nil, err
				}
				continue
			}
			for _, value := range fields[index : index+5] {
				if !value.IsZero() {
					return auditfield.Field32{}, nil, nil, fmt.Errorf("nonzero inactive batch output plaintext")
				}
			}
		}
		return asset, inputs, outputs, nil
	default:
		return auditfield.Field32{}, nil, nil, fmt.Errorf("unknown decrypted audit record kind")
	}
}
