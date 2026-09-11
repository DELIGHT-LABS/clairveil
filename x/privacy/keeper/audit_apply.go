package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// applyPrivacyTransition commits only the privacy state required to execute a
// transaction: public principal movement, nullifiers, commitments and wallet
// scan projections. The verified record is deliberately an in-memory witness;
// there is no second, persistent audit transaction ledger or reference index.
func (k Keeper) applyPrivacyTransition(ctx sdk.Context, v *verifiedAuditTransition) (uint64, error) {
	if k.audit == nil || v == nil || v.owner != k.audit || v.record == nil || v.consumed || v.message.Kind() == 0 {
		return 0, fmt.Errorf("invalid verified audit transition")
	}
	v.consumed = true
	if ctx.Value(auditApplyContextKey{}) != nil {
		return 0, fmt.Errorf("reentrant privacy apply is forbidden")
	}
	ctx = ctx.WithValue(auditApplyContextKey{}, k.audit)
	origin, err := k.auditExecutionOrigin(ctx)
	if err != nil {
		return 0, err
	}
	if origin != v.record.Origin {
		return 0, fmt.Errorf("verified execution context changed")
	}
	cache, publish := ctx.CacheContext()
	record := v.record
	if record.Transparent != nil {
		if err := k.applyAuditPrincipal(cache, v.message); err != nil {
			return 0, err
		}
	}
	if err := k.runBatchTransitionHook("audit_bank"); err != nil {
		return 0, err
	}
	for _, n := range record.Inputs {
		used, err := k.hasNullifierStrict(cache, n[:])
		if err != nil {
			return 0, err
		}
		if used {
			return 0, fmt.Errorf("audit nullifier already used")
		}
		if err := k.setNullifierStrict(cache, n[:]); err != nil {
			return 0, err
		}
	}
	for i, o := range record.Outputs {
		index, err := k.getLeafCountStrict(cache)
		if err != nil {
			return 0, err
		}
		if err := k.appendCommitment(cache, o.Commitment[:]); err != nil {
			return 0, err
		}
		record.Outputs[i].LeafIndex = index
	}
	if err := k.runBatchTransitionHook("audit_tree"); err != nil {
		return 0, err
	}
	scanBytes, global, err := k.storeAuditScan(cache, record, v.message)
	if err != nil {
		return 0, err
	}
	written := scanBytes
	if written > v.projectedBound {
		return 0, fmt.Errorf("audit projection exceeds precharged upper bound: %d > %d", written, v.projectedBound)
	}
	if err := k.runBatchTransitionHook("audit_scan"); err != nil {
		return 0, err
	}
	publish()
	return global, nil
}

func (k Keeper) applyAuditPrincipal(ctx sdk.Context, m types.ValidatedAuditMessage) error {
	coin := m.Coin()
	before, err := k.GetReserveSnapshot(ctx, coin.Denom)
	if err != nil {
		return err
	}
	if !before.Collateralized {
		return ErrReserveUnderCollateralized
	}
	deposit := m.Kind() == auditfield.KindDeposit
	if !deposit && coin.Amount.GT(before.Liability) {
		return fmt.Errorf("withdraw amount exceeds liability")
	}
	target := m.Creator()
	if !deposit {
		target = m.Recipient()
	}
	address, err := sdk.AccAddressFromBech32(target)
	if err != nil {
		return err
	}
	module := authtypes.NewModuleAddress(types.ModuleName)
	if address.Equals(module) {
		return fmt.Errorf("invalid principal endpoint")
	}
	externalBefore := k.bankKeeper.GetBalance(ctx, address, coin.Denom).Amount
	if externalBefore.IsNil() || externalBefore.IsNegative() {
		return fmt.Errorf("invalid principal balance")
	}
	wantExternal := new(big.Int).Set(externalBefore.BigInt())
	wantModule := new(big.Int).Set(before.ModuleBalance.BigInt())
	if deposit {
		wantExternal.Sub(wantExternal, coin.Amount.BigInt())
		wantModule.Add(wantModule, coin.Amount.BigInt())
	} else {
		wantExternal.Add(wantExternal, coin.Amount.BigInt())
		wantModule.Sub(wantModule, coin.Amount.BigInt())
	}
	if wantExternal.Sign() < 0 || wantExternal.BitLen() > 256 || wantModule.Sign() < 0 || wantModule.BitLen() > 256 {
		return fmt.Errorf("principal balance overflow or insufficiency")
	}
	if deposit {
		err = k.audit.principal.Lock(ctx, address, coin)
	} else {
		err = k.audit.principal.Release(ctx, address, coin)
	}
	if err != nil {
		return err
	}
	externalAfter := k.bankKeeper.GetBalance(ctx, address, coin.Denom).Amount
	moduleAfter := k.bankKeeper.GetBalance(ctx, module, coin.Denom).Amount
	if externalAfter.IsNil() || moduleAfter.IsNil() || externalAfter.BigInt().Cmp(wantExternal) != 0 || moduleAfter.BigInt().Cmp(wantModule) != 0 {
		return fmt.Errorf("principal transfer did not apply both exact balance deltas")
	}
	if deposit {
		err = k.recordReserveDeposit(ctx, coin)
	} else {
		err = k.recordReserveWithdraw(ctx, coin)
	}
	if err != nil {
		return err
	}
	after, err := k.GetReserveSnapshot(ctx, coin.Denom)
	if err != nil {
		return err
	}
	if !after.Collateralized {
		return ErrReserveUnderCollateralized
	}
	return nil
}

func auditEventType(kind auditfield.Kind) string {
	switch kind {
	case 1:
		return types.EventTypeDeposit
	case 2:
		return types.EventTypeWithdraw
	case 3:
		return types.EventTypeShieldedTransfer
	case 4:
		return types.EventTypeBatchTransferV1
	}
	return ""
}
func (k Keeper) storeAuditScan(ctx sdk.Context, r *auditTransitionRecord, m types.ValidatedAuditMessage) (uint64, uint64, error) {
	global, err := k.allocatePrivacyGlobalSequence(ctx)
	if err != nil {
		return 0, 0, err
	}
	effect := auditExecutionID(r.Network, r.Origin, global, ctx.MsgIndex())
	summary, outputs := auditScanProjection(global, ctx.BlockHeight(), r, m.Outputs(), effect)
	if err := k.storePrivacyScan(ctx, summary, outputs); err != nil {
		return 0, 0, err
	}
	// The original tx supplies ciphertext and proof. Events expose only the
	// successful execution identity and location needed to select that leaf.
	attrs := []sdk.Attribute{
		sdk.NewAttribute("execution_id", hex.EncodeToString(effect[:])),
		sdk.NewAttribute("global_sequence", fmt.Sprint(global)),
		sdk.NewAttribute("message_index", fmt.Sprint(ctx.MsgIndex())),
	}
	if r.Origin.Kind == 1 {
		attrs = append(attrs, sdk.NewAttribute("tx_hash", hex.EncodeToString(r.Origin.Anchor[:])))
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(summary.EventType, attrs...))
	count := uint64(summary.Size() + 17 + 9 + 8)
	for _, o := range outputs {
		count += uint64(o.Size() + 21)
	}
	return count, global, nil
}

func auditExecutionID(network [32]byte, origin auditOrigin, global uint64, messageIndex int) [32]byte {
	var encoded [16]byte
	binary.BigEndian.PutUint64(encoded[:8], global)
	binary.BigEndian.PutUint64(encoded[8:], uint64(messageIndex))
	return sha256.Sum256(append(append(append(append([]byte("clairveil/privacy/execution-id/v1"), network[:]...), origin.Anchor[:]...), encoded[:]...), byte(origin.Kind)))
}
func validateAuditScanSummary(s *types.PrivacyScanSummaryV2) error {
	if s.CircuitSetId != auditfield.CircuitSetID || s.AuditKeyEpoch == 0 || len(s.EffectId) != 32 {
		return fmt.Errorf("invalid audit scan identity")
	}
	id, err := hex.DecodeString(s.AuditKeyId)
	if err != nil || hex.EncodeToString(id) != s.AuditKeyId {
		return fmt.Errorf("invalid audit scan key ID")
	}
	if _, err := auditfield.ParseAuditKey(s.AuditTargetPubkey, id); err != nil {
		return err
	}
	var kind auditfield.Kind
	for k := auditfield.Kind(1); k <= 4; k++ {
		if auditEventType(k) == s.EventType {
			kind = k
		}
	}
	return kind.ValidateActiveCounts(uint8(len(s.Nullifiers)), uint8(s.OutputCount))
}
func validateAuditScanOutput(o *types.PrivacyScanOutputV2) error {
	if len(o.AuditDisclosurePayload) != 0 {
		return fmt.Errorf("audit scan must not carry legacy audit ciphertext")
	}
	if o.EventType == types.EventTypeDeposit || (o.EventType == types.EventTypeShieldedTransfer && o.OutputIndex == 1) {
		return validatePrivacyScanOutputEventV2(o)
	}
	if len(o.EncryptedNote) != 0 || len(o.ViewTag) != types.ViewTagLength {
		return fmt.Errorf("invalid audit recovery output")
	}
	if _, err := types.UnwrapEncryptedEnvelopeV1(o.Ciphertext, types.EnvelopeTransferNoteV1); err != nil {
		return err
	}
	if err := validatePrivacyScanUserDisclosureV2(o); err != nil {
		return err
	}
	if err := validatePrivacyScanActiveFieldV2("self full digest", o.FullDisclosureDigest); err != nil {
		return err
	}
	if len(o.SelfViewDisclosurePayload) > 0 {
		_, err := types.UnwrapEncryptedEnvelopeV1(o.SelfViewDisclosurePayload, types.EnvelopeSelfViewDisclosureV1)
		return err
	}
	return nil
}

func auditScanProjection(global uint64, height int64, r *auditTransitionRecord, source []*v2.OutputEffect, effect [32]byte) (*types.PrivacyScanSummaryV2, []*types.PrivacyScanOutputV2) {
	summary := &types.PrivacyScanSummaryV2{GlobalSequence: global, Height: height, EventType: auditEventType(r.Kind), OutputCount: uint32(len(r.Outputs)), CircuitSetId: r.SetID, PayloadVersion: types.FixedPayloadVersionV1, ScanSchemaVersion: types.PrivacyScanSchemaVersionV2, AuditKeyId: hex.EncodeToString(r.KeyID[:]), AuditKeyEpoch: r.Epoch, AuditTargetPubkey: append([]byte(nil), r.PK...), EffectId: effect[:]}
	if r.Origin.Kind == 1 {
		summary.TxHash = append([]byte(nil), r.Origin.Anchor[:]...)
	}
	for _, n := range r.Inputs {
		summary.Nullifiers = append(summary.Nullifiers, n.Bytes())
	}
	outputs := make([]*types.PrivacyScanOutputV2, len(r.Outputs))
	for i, o := range source {
		out := &types.PrivacyScanOutputV2{GlobalSequence: global, Height: height, OutputIndex: uint32(i), EffectId: effect[:], Commitment: append([]byte(nil), o.Commitment...), Ciphertext: append([]byte(nil), o.Ciphertext...), ViewTag: append([]byte(nil), o.ViewTag...), LeafIndex: r.Outputs[i].LeafIndex, LeafIndexFound: true, UserPrivacyPolicy: o.UserPrivacyPolicy, UserDisclosureMode: types.UserDisclosureMode(o.UserDisclosureMode).String(), UserDisclosureDigest: append([]byte(nil), o.UserDisclosureDigest...), UserDisclosureTargetPubkey: append([]byte(nil), o.UserDisclosureTargetPubkey...), UserDisclosurePayload: append([]byte(nil), o.UserDisclosurePayload...), FullDisclosureDigest: append([]byte(nil), o.SelfFullDisclosureDigest...), SelfViewDisclosurePayload: append([]byte(nil), o.SelfViewDisclosurePayload...), CircuitSetId: summary.CircuitSetId, PayloadVersion: summary.PayloadVersion, ScanSchemaVersion: summary.ScanSchemaVersion, AuditKeyId: summary.AuditKeyId, AuditKeyEpoch: summary.AuditKeyEpoch, AuditTargetPubkey: append([]byte(nil), r.PK...), TxHash: append([]byte(nil), summary.TxHash...), EventType: summary.EventType}
		if r.Kind == 1 || (r.Kind == 3 && i == 1) {
			out.UserDisclosureMode = ""
		}
		if r.Kind == 1 {
			out.EncryptedNote, out.Ciphertext = out.Ciphertext, nil
		}
		outputs[i] = out
	}

	return summary, outputs
}
