package types

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
)

// ValidatedAuditMessage owns detached input bytes. It is message validation,
// never proof verification or an authorization capability.
type ValidatedAuditMessage struct {
	kind                 auditfield.Kind
	creator, recipient   string
	coin                 sdk.Coin
	root                 auditfield.Field32
	nullifiers           []auditfield.Field32
	outputs              []*privacyv2.OutputEffect
	proof, aux, envelope []byte
	keyID                [32]byte
	epoch                uint64
	expiry               int64
}

func (m ValidatedAuditMessage) Kind() auditfield.Kind { return m.kind }
func (m ValidatedAuditMessage) Creator() string       { return m.creator }
func (m ValidatedAuditMessage) Recipient() string     { return m.recipient }
func (m ValidatedAuditMessage) Coin() sdk.Coin {
	if m.coin.Amount.IsNil() {
		return sdk.Coin{}
	}
	return sdk.NewCoin(m.coin.Denom, m.coin.Amount.AddRaw(0))
}
func (m ValidatedAuditMessage) Root() auditfield.Field32 { return m.root }
func (m ValidatedAuditMessage) Nullifiers() []auditfield.Field32 {
	return append([]auditfield.Field32(nil), m.nullifiers...)
}
func (m ValidatedAuditMessage) Outputs() []*privacyv2.OutputEffect {
	out := make([]*privacyv2.OutputEffect, len(m.outputs))
	for i, v := range m.outputs {
		out[i] = proto.Clone(v).(*privacyv2.OutputEffect)
	}
	return out
}
func (m ValidatedAuditMessage) Proof() []byte    { return append([]byte(nil), m.proof...) }
func (m ValidatedAuditMessage) Aux() []byte      { return append([]byte(nil), m.aux...) }
func (m ValidatedAuditMessage) Envelope() []byte { return append([]byte(nil), m.envelope...) }
func (m ValidatedAuditMessage) KeyID() [32]byte  { return m.keyID }
func (m ValidatedAuditMessage) Epoch() uint64    { return m.epoch }
func (m ValidatedAuditMessage) Expiry() int64    { return m.expiry }

func ValidateAuditMessage(raw sdk.Msg) (ValidatedAuditMessage, error) {
	fail := func(err error) (ValidatedAuditMessage, error) { return ValidatedAuditMessage{}, err }
	var m ValidatedAuditMessage
	var amount string
	var root []byte
	var nullifiers [][]byte
	var outputs []*privacyv2.OutputEffect
	var auth *privacyv2.AuditAuthorization
	switch v := raw.(type) {
	case *privacyv2.MsgDeposit:
		if v == nil {
			return fail(fmt.Errorf("nil deposit"))
		}
		m.kind = 1
		m.creator = v.Creator
		amount = v.Amount
		outputs = []*privacyv2.OutputEffect{v.Output}
		m.proof = v.Proof
		m.expiry = v.ExpiresAtUnix
		auth = v.Audit
	case *privacyv2.MsgWithdraw:
		if v == nil {
			return fail(fmt.Errorf("nil withdraw"))
		}
		m.kind = 2
		m.creator = v.Creator
		amount = v.Amount
		m.recipient = v.Recipient
		root = v.Root
		nullifiers = [][]byte{v.Nullifier}
		m.proof = v.Proof
		m.expiry = v.ExpiresAtUnix
		auth = v.Audit
	case *privacyv2.MsgTransfer:
		if v == nil {
			return fail(fmt.Errorf("nil transfer"))
		}
		m.kind = 3
		m.creator = v.Creator
		root = v.Root
		nullifiers = v.Nullifiers
		outputs = v.Outputs
		m.proof = v.Proof
		m.expiry = v.ExpiresAtUnix
		auth = v.Audit
	case *privacyv2.MsgBatchTransfer:
		if v == nil {
			return fail(fmt.Errorf("nil batch"))
		}
		m.kind = 4
		m.creator = v.Creator
		root = v.Root
		nullifiers = v.Nullifiers
		outputs = v.Outputs
		m.proof = v.Proof
		m.expiry = v.ExpiresAtUnix
		auth = v.Audit
	default:
		return fail(fmt.Errorf("unsupported audit message"))
	}
	if proto.Size(raw) > MaxBatchTransferMessageBytesV1 {
		return fail(fmt.Errorf("audit message exceeds 128KiB"))
	}
	if len(nullifiers) > 16 || len(outputs) > 32 {
		return fail(fmt.Errorf("audit capacity exceeded"))
	}
	if err := m.kind.ValidateActiveCounts(uint8(len(nullifiers)), uint8(len(outputs))); err != nil {
		return fail(err)
	}
	if auth == nil || len(auth.KeyId) != 32 || auth.Epoch == 0 || m.expiry <= 0 {
		return fail(fmt.Errorf("audit authorization and positive expiry are required"))
	}
	want, _ := m.kind.EnvelopeSize()
	if len(auth.Envelope) != want {
		return fail(fmt.Errorf("incorrect audit envelope size"))
	}
	if len(m.proof) != 164 {
		return fail(fmt.Errorf("audit proof requires 164 bytes"))
	}
	if _, err := canonicalAuditAddress(m.creator); err != nil {
		return fail(err)
	}
	if m.kind == auditfield.KindWithdraw {
		if _, err := canonicalAuditAddress(m.recipient); err != nil {
			return fail(err)
		}
	}
	if m.kind == 1 || m.kind == 2 {
		coin, err := sdk.ParseCoinNormalized(amount)
		if err != nil {
			return fail(err)
		}
		if !coin.IsPositive() || coin.String() != amount || coin.Amount.BigInt().BitLen() > 64 {
			return fail(fmt.Errorf("audit amount must be canonical positive uint64 coin"))
		}
		m.coin = coin
	}
	if len(nullifiers) > 0 {
		f, err := auditfield.ParseField32(root)
		if err != nil {
			return fail(err)
		}
		m.root = f
	}
	seen := map[auditfield.Field32]bool{}
	for _, n := range nullifiers {
		f, err := auditfield.ParseField32(n)
		if err != nil {
			return fail(err)
		}
		if f.IsZero() || seen[f] {
			return fail(fmt.Errorf("zero or duplicate nullifier"))
		}
		seen[f] = true
		m.nullifiers = append(m.nullifiers, f)
	}
	var err error
	_, m.aux, err = AuditOutputsToAux(m.kind, outputs)
	if err != nil {
		return fail(err)
	}
	for _, o := range outputs {
		m.outputs = append(m.outputs, proto.Clone(o).(*privacyv2.OutputEffect))
	}
	m.proof = append([]byte(nil), m.proof...)
	m.envelope = append([]byte(nil), auth.Envelope...)
	copy(m.keyID[:], auth.KeyId)
	m.epoch = auth.Epoch
	return m, nil
}
func canonicalAuditAddress(value string) (sdk.AccAddress, error) {
	a, err := sdk.AccAddressFromBech32(value)
	if err != nil {
		return nil, err
	}
	if a.String() != value || len(a) == 0 || len(a) > 255 {
		return nil, fmt.Errorf("noncanonical audit address")
	}
	return a, nil
}

// AuditOutputsToAux validates generated effects and calls the leaf codec once.
// Commitments are separately bound by CommitmentRoot and never added to Aux.
func AuditOutputsToAux(kind auditfield.Kind, outputs []*privacyv2.OutputEffect) ([]auditfield.AuxOutput, []byte, error) {
	out := make([]auditfield.AuxOutput, len(outputs))
	seen := map[auditfield.Field32]bool{}
	for i, o := range outputs {
		if o == nil {
			return nil, nil, fmt.Errorf("nil output")
		}
		c, err := auditfield.ParseField32(o.Commitment)
		if err != nil {
			return nil, nil, err
		}
		if c.IsZero() || seen[c] {
			return nil, nil, fmt.Errorf("zero or duplicate commitment")
		}
		seen[c] = true
		if o.UserDisclosureMode > 2 {
			return nil, nil, fmt.Errorf("unsupported user disclosure mode")
		}
		recoveryKind := EnvelopeTransferNoteV1
		if kind == auditfield.KindDeposit {
			recoveryKind = EnvelopeDepositNoteV1
		}
		if _, err := UnwrapEncryptedEnvelopeV1(o.Ciphertext, recoveryKind); err != nil {
			return nil, nil, err
		}
		var user, full auditfield.Field32
		if len(o.UserDisclosureDigest) > 0 {
			user, err = auditfield.ParseField32(o.UserDisclosureDigest)
			if err != nil {
				return nil, nil, err
			}
		}
		if len(o.SelfFullDisclosureDigest) > 0 {
			full, err = auditfield.ParseField32(o.SelfFullDisclosureDigest)
			if err != nil {
				return nil, nil, err
			}
		}
		if kind == auditfield.KindDeposit || (kind == auditfield.KindTransfer2x2 && i == 1) {
			if len(o.UserDisclosureDigest) != 0 || len(o.SelfFullDisclosureDigest) != 0 {
				return nil, nil, fmt.Errorf("unused output digests must be omitted")
			}
		} else if full.IsZero() {
			return nil, nil, fmt.Errorf("self full disclosure digest must be nonzero")
		}
		if kind == auditfield.KindBatch16x32 {
			if err := validateBatchUserDisclosureWirePrototypeV1(uint32(i), &BatchTransferOutputWirePrototypeV1{Commitment: o.Commitment, UserPrivacyPolicy: o.UserPrivacyPolicy, UserDisclosureMode: UserDisclosureMode(o.UserDisclosureMode), UserDisclosureDigest: o.UserDisclosureDigest, UserDisclosureTargetPubkey: o.UserDisclosureTargetPubkey, UserDisclosurePayload: o.UserDisclosurePayload}); err != nil {
				return nil, nil, err
			}
		}
		if kind == auditfield.KindTransfer2x2 && i == 0 {
			if err := validateUserDisclosure(o.UserPrivacyPolicy, o.UserDisclosureDigest, UserDisclosureMode(o.UserDisclosureMode), o.UserDisclosureTargetPubkey, o.UserDisclosurePayload); err != nil {
				return nil, nil, err
			}
			if o.UserDisclosureMode == 1 {
				p, err := UnmarshalDisclosurePlaintextV1(o.UserDisclosurePayload)
				if err != nil {
					return nil, nil, err
				}
				if p.Commitment.Cmp(new(big.Int).SetBytes(o.Commitment)) != 0 {
					return nil, nil, fmt.Errorf("disclosure commitment mismatch")
				}
				digest, err := ComputeTransferDisclosureDigestBytes(p.Policy, p.OutputIndex, o.Commitment, p.Amount, p.AssetID, p.SenderSpendKeyX, p.SenderSpendKeyY, p.SenderViewKeyX, p.SenderViewKeyY, p.RecipientSpendKeyX, p.RecipientSpendKeyY, p.RecipientViewKeyX, p.RecipientViewKeyY, p.DisclosureBlinding)
				if err != nil || !bytes.Equal(digest, o.UserDisclosureDigest) {
					return nil, nil, fmt.Errorf("disclosure digest mismatch")
				}
			}
		}
		if len(o.SelfViewDisclosurePayload) > 0 {
			if _, err := UnwrapEncryptedEnvelopeV1(o.SelfViewDisclosurePayload, EnvelopeSelfViewDisclosureV1); err != nil {
				return nil, nil, err
			}
		}
		cp := func(b []byte) []byte { return append([]byte(nil), b...) }
		out[i] = auditfield.AuxOutput{RecoveryCiphertext: cp(o.Ciphertext), ViewTag: cp(o.ViewTag), UserPolicy: o.UserPrivacyPolicy, UserMode: uint8(o.UserDisclosureMode), UserDigest: user, UserTargetPubKey: cp(o.UserDisclosureTargetPubkey), UserDisclosurePayload: cp(o.UserDisclosurePayload), SelfFullDigest: full, SelfViewPayload: cp(o.SelfViewDisclosurePayload)}
	}
	encoded, err := auditfield.EncodeAuxFrame(kind, out)
	if err != nil {
		return nil, nil, err
	}
	return out, encoded, nil
}
