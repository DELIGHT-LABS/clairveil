package keeper

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

type auditApplyTestPrincipal struct{ bank *cacheAwareDepositBankKeeper }

func (p auditApplyTestPrincipal) Lock(ctx sdk.Context, actor sdk.AccAddress, coin sdk.Coin) error {
	return p.bank.SendCoinsFromAccountToModule(ctx, actor, privacytypes.ModuleName, sdk.NewCoins(coin))
}

func (p auditApplyTestPrincipal) Release(ctx sdk.Context, recipient sdk.AccAddress, coin sdk.Coin) error {
	return p.bank.SendCoinsFromModuleToAccount(ctx, privacytypes.ModuleName, recipient, sdk.NewCoins(coin))
}

type auditApplyBoundaryFixture struct {
	k         *Keeper
	ctx       sdk.Context
	bank      *cacheAwareDepositBankKeeper
	msg       sdk.Msg
	validated privacytypes.ValidatedAuditMessage
	record    *auditTransitionRecord
	actor     sdk.AccAddress
	recipient sdk.AccAddress
}

func newAuditApplyBoundaryFixture(t testing.TB, kind auditfield.Kind) auditApplyBoundaryFixture {
	t.Helper()
	k, ctx, bank := setupCacheAwareDepositKeeper(t)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	k.audit = &auditRuntime{principal: auditApplyTestPrincipal{bank: bank}, identity: &privacytypes.CircuitSetIdentity{}, nonce: [32]byte{31: 1}, initialHeight: 1, authority: authority}
	actorString, recipientString := testAddress(0x61), testAddress(0x62)
	actor, err := sdk.AccAddressFromBech32(actorString)
	require.NoError(t, err)
	recipient, err := sdk.AccAddressFromBech32(recipientString)
	require.NoError(t, err)
	module := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bank.setTestBalance(t, ctx, actor, "uclair", 100)
	bank.setTestBalance(t, ctx, recipient, "uclair", 0)
	bank.setTestBalance(t, ctx, module, "uclair", 20)
	require.NoError(t, k.recordReserveDeposit(ctx, sdk.NewInt64Coin("uclair", 14)))

	secret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(37).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	require.NoError(t, err)
	envelopeSize, err := kind.EnvelopeSize()
	require.NoError(t, err)
	keyID := key.ID()
	auth := &privacyv2.AuditAuthorization{KeyId: keyID[:], Epoch: 1, Envelope: make([]byte, envelopeSize)}
	proof := make([]byte, 164)
	root := fixedFieldBytes(700 + uint64(kind))
	output := func(value uint64, deposit bool, full bool) *privacyv2.OutputEffect {
		envelopeKind := privacytypes.EnvelopeTransferNoteV1
		if deposit {
			envelopeKind = privacytypes.EnvelopeDepositNoteV1
		}
		effect := &privacyv2.OutputEffect{Commitment: fixedFieldBytes(value), Ciphertext: testKeeperEnvelopeTB(t, envelopeKind)}
		if !deposit {
			effect.ViewTag = []byte{1, 2}
		}
		if full {
			effect.SelfFullDisclosureDigest = fixedFieldBytes(value + 1000)
		}
		return effect
	}
	var msg sdk.Msg
	switch kind {
	case auditfield.KindDeposit:
		msg = &privacyv2.MsgDeposit{Creator: actorString, Amount: "7uclair", Output: output(801, true, false), Proof: proof, ExpiresAtUnix: msgServerTestExpiry, Audit: auth}
	case auditfield.KindWithdraw:
		msg = &privacyv2.MsgWithdraw{Creator: actorString, Recipient: recipientString, Amount: "7uclair", Root: root, Nullifier: fixedFieldBytes(811), Proof: proof, ExpiresAtUnix: msgServerTestExpiry, Audit: auth}
	case auditfield.KindTransfer2x2:
		msg = &privacyv2.MsgTransfer{Creator: actorString, Root: root, Nullifiers: [][]byte{fixedFieldBytes(821), fixedFieldBytes(822)}, Outputs: []*privacyv2.OutputEffect{output(823, false, true), output(824, false, false)}, Proof: proof, ExpiresAtUnix: msgServerTestExpiry, Audit: auth}
	case auditfield.KindBatch16x32:
		msg = &privacyv2.MsgBatchTransfer{Creator: actorString, Root: root, Nullifiers: [][]byte{fixedFieldBytes(831)}, Outputs: []*privacyv2.OutputEffect{output(832, false, true)}, Proof: proof, ExpiresAtUnix: msgServerTestExpiry, Audit: auth}
	default:
		t.Fatalf("unsupported fixture kind %d", kind)
	}
	validated, err := privacytypes.ValidateAuditMessage(msg)
	require.NoError(t, err)
	origin, err := k.auditExecutionOrigin(ctx)
	require.NoError(t, err)
	network, err := k.auditNetwork(ctx)
	require.NoError(t, err)
	record := &auditTransitionRecord{
		Height: uint64(ctx.BlockHeight()), Origin: origin, Kind: kind, Network: network,
		SetID: auditfield.CircuitSetID, Proof: validated.Proof(), KeyID: keyID, Epoch: 1, Suite: 2,
		PK: key.Point().Bytes(), Envelope: validated.Envelope(), Root: validated.Root(), Inputs: validated.Nullifiers(),
	}
	record.PI[7] = auditfield.Field32FromUint64(uint64(validated.Expiry()))
	record.AuxHash = sha256.Sum256(append([]byte(auditfield.AuditAuxDomain), validated.Aux()...))
	for _, effect := range validated.Outputs() {
		commitment, parseErr := auditfield.ParseField32(effect.Commitment)
		require.NoError(t, parseErr)
		record.Outputs = append(record.Outputs, auditOutputRef{Commitment: commitment})
	}
	if kind == auditfield.KindDeposit || kind == auditfield.KindWithdraw {
		from, to := []byte(actor), []byte(module)
		if kind == auditfield.KindWithdraw {
			from, to = []byte(module), []byte(recipient)
		}
		record.Transparent = &auditTransparentEffect{Kind: kind, From: from, To: to, Denom: "uclair", Amount: auditfield.Field32FromUint64(7)}
	}
	return auditApplyBoundaryFixture{k: k, ctx: ctx, bank: bank, msg: msg, validated: validated, record: record, actor: actor, recipient: recipient}
}

func (f auditApplyBoundaryFixture) apply() (uint64, error) {
	return f.k.applyPrivacyTransition(f.ctx, &verifiedAuditTransition{owner: f.k.audit, message: f.validated, record: f.record, projectedBound: 1 << 20})
}

func delegatedAuditFixture(t testing.TB, fixture auditApplyBoundaryFixture, funder sdk.AccAddress) *delegatedDepositExecution {
	t.Helper()
	payload, err := proto.Marshal(fixture.msg)
	require.NoError(t, err)
	return &delegatedDepositExecution{funder: append(sdk.AccAddress(nil), funder...), payload: payload}
}

type auditApplyBoundarySnapshot struct {
	leaves, sequence         uint64
	actor, recipient, module string
	deposited, withdrawn     string
	nullifiers, commitments  []bool
	events                   int
}

func snapshotAuditApplyBoundary(t testing.TB, fixture auditApplyBoundaryFixture) auditApplyBoundarySnapshot {
	t.Helper()
	module := authtypes.NewModuleAddress(privacytypes.ModuleName)
	reserve, err := fixture.k.GetReserveSnapshot(fixture.ctx, "uclair")
	require.NoError(t, err)
	sequence, err := fixture.k.GetPrivacyGlobalSequence(fixture.ctx)
	require.NoError(t, err)
	snapshot := auditApplyBoundarySnapshot{
		leaves: fixture.k.GetLeafCount(fixture.ctx), sequence: sequence,
		actor:     fixture.bank.testBalance(t, fixture.ctx, fixture.actor, "uclair").String(),
		recipient: fixture.bank.testBalance(t, fixture.ctx, fixture.recipient, "uclair").String(),
		module:    fixture.bank.testBalance(t, fixture.ctx, module, "uclair").String(),
		deposited: reserve.TotalDeposited.String(), withdrawn: reserve.TotalWithdrawn.String(),
		events: len(fixture.ctx.EventManager().Events()),
	}
	for _, nullifier := range fixture.record.Inputs {
		found, lookupErr := fixture.k.hasNullifierStrict(fixture.ctx, nullifier[:])
		require.NoError(t, lookupErr)
		snapshot.nullifiers = append(snapshot.nullifiers, found)
	}
	for _, output := range fixture.record.Outputs {
		found, lookupErr := fixture.k.HasCommitment(fixture.ctx, output.Commitment[:])
		require.NoError(t, lookupErr)
		snapshot.commitments = append(snapshot.commitments, found)
	}
	return snapshot
}

func TestAuditApplyBoundaryAcceptsAllFourKinds(t *testing.T) {
	for _, kind := range []auditfield.Kind{auditfield.KindDeposit, auditfield.KindWithdraw, auditfield.KindTransfer2x2, auditfield.KindBatch16x32} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			fixture := newAuditApplyBoundaryFixture(t, kind)
			sequence, err := fixture.apply()
			require.NoError(t, err)
			require.EqualValues(t, 1, sequence)
			after := snapshotAuditApplyBoundary(t, fixture)
			require.EqualValues(t, len(fixture.record.Outputs), after.leaves)
			require.EqualValues(t, 1, after.sequence)
			for _, found := range after.nullifiers {
				require.True(t, found)
			}
			for _, found := range after.commitments {
				require.True(t, found)
			}
			require.Len(t, fixture.ctx.EventManager().Events(), 1)
		})
	}
}

func TestAuditApplyBoundaryDelegatedDepositDebitsOnlyFunder(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x63}, 20))
	fixture.bank.setTestBalance(t, fixture.ctx, funder, "uclair", 40)
	delegated := delegatedAuditFixture(t, fixture, funder)

	_, err := fixture.k.applyPrivacyTransition(fixture.ctx, &verifiedAuditTransition{
		owner: fixture.k.audit, message: fixture.validated, record: fixture.record,
		projectedBound: 1 << 20, delegated: delegated,
	})
	require.NoError(t, err)
	require.Equal(t, "100", fixture.bank.testBalance(t, fixture.ctx, fixture.actor, "uclair").String())
	require.Equal(t, "33", fixture.bank.testBalance(t, fixture.ctx, funder, "uclair").String())
	require.Equal(t, funder, fixture.bank.lastAccountSender)

	events := fixture.ctx.EventManager().Events()
	require.Len(t, events, 1)
	attrs := map[string]string{}
	for _, attr := range events[0].Attributes {
		attrs[attr.Key] = attr.Value
	}
	require.Equal(t, funder.String(), attrs[privacytypes.AttributeKeyDelegatedFunder])
	require.Equal(t, base64.StdEncoding.EncodeToString(delegated.payload), attrs[privacytypes.AttributeKeyDelegatedDepositPayload])
}

func TestAuditApplyBoundaryNormalDepositStillDebitsCreator(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	_, err := fixture.apply()
	require.NoError(t, err)
	require.Equal(t, "93", fixture.bank.testBalance(t, fixture.ctx, fixture.actor, "uclair").String())
	event := fixture.ctx.EventManager().Events()[0]
	for _, attr := range event.Attributes {
		require.NotEqual(t, privacytypes.AttributeKeyDelegatedFunder, attr.Key)
		require.NotEqual(t, privacytypes.AttributeKeyDelegatedDepositPayload, attr.Key)
	}
}

func TestAuditApplyBoundaryDelegatedFailureRollsBack(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x64}, 20))
	fixture.bank.setTestBalance(t, fixture.ctx, funder, "uclair", 40)
	before := snapshotAuditApplyBoundary(t, fixture)
	fixture.k.batchTransitionHook = func(stage string) error {
		if stage == "audit_scan" {
			return errors.New("injected delegated failure")
		}
		return nil
	}
	_, err := fixture.k.applyPrivacyTransition(fixture.ctx, &verifiedAuditTransition{
		owner: fixture.k.audit, message: fixture.validated, record: fixture.record,
		projectedBound: 1 << 20, delegated: delegatedAuditFixture(t, fixture, funder),
	})
	require.ErrorContains(t, err, "injected delegated failure")
	require.Equal(t, before, snapshotAuditApplyBoundary(t, fixture))
	require.Equal(t, "40", fixture.bank.testBalance(t, fixture.ctx, funder, "uclair").String())
}

func TestAuditApplyBoundaryOuterCacheDiscardDropsStateAndEvents(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	funder := sdk.AccAddress(bytes.Repeat([]byte{0x65}, 20))
	fixture.bank.setTestBalance(t, fixture.ctx, funder, "uclair", 40)
	parent := fixture.ctx
	outer, _ := parent.CacheContext()
	fixture.ctx = outer
	_, err := fixture.k.applyPrivacyTransition(outer, &verifiedAuditTransition{
		owner: fixture.k.audit, message: fixture.validated, record: fixture.record,
		projectedBound: 1 << 20, delegated: delegatedAuditFixture(t, fixture, funder),
	})
	require.NoError(t, err)
	require.Len(t, outer.EventManager().Events(), 1)

	fixture.ctx = parent
	require.Equal(t, "40", fixture.bank.testBalance(t, parent, funder, "uclair").String())
	require.EqualValues(t, 0, fixture.k.GetLeafCount(parent))
	require.Empty(t, parent.EventManager().Events())
}

func TestDepositWithFunderV2RejectsModuleFunders(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	message := fixture.msg.(*privacyv2.MsgDeposit)
	for name, funder := range map[string]sdk.AccAddress{
		"privacy":    authtypes.NewModuleAddress(privacytypes.ModuleName),
		"governance": authtypes.NewModuleAddress(govtypes.ModuleName),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fixture.k.DepositWithFunderV2(fixture.ctx, message, funder)
			require.ErrorContains(t, err, "invalid delegated deposit funder")
		})
	}
}

func installAuditBoundaryInitialKey(t testing.TB, fixture auditApplyBoundaryFixture) {
	t.Helper()
	network, err := fixture.k.auditNetwork(fixture.ctx)
	require.NoError(t, err)
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(37).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	require.NoError(t, err)
	pop, err := privacycrypto.CreateAuditPoP96(secret, network, 1, 0)
	require.NoError(t, err)
	popBytes, err := pop.Bytes()
	require.NoError(t, err)
	record := &auditKeyRecord{Epoch: 1, KeyID: key.ID(), Suite: 2, PK: key.Point().Bytes(), ActivationHeight: 0, RegisteredHeight: 0, Origin: auditOrigin{Kind: 3}, PoP: popBytes}
	encoded, err := encodeAuditKey(record, network, 1)
	require.NoError(t, err)
	store := fixture.k.storeService.OpenKVStore(fixture.ctx)
	require.NoError(t, store.Set(auditEpochKey(auditEpochPrefix, 1), encoded))
	active := make([]byte, 18)
	binary.BigEndian.PutUint16(active, 1)
	binary.BigEndian.PutUint64(active[2:10], 1)
	require.NoError(t, store.Set([]byte{auditActivePrefix}, active))
	root := fixture.validated.Root()
	if !root.IsZero() {
		require.NoError(t, store.Set(privacytypes.GetHistoricalRootKey(root[:]), []byte{1}))
	}
}

func validAuditBoundaryEnvelope(t testing.TB, fixture auditApplyBoundaryFixture) []byte {
	t.Helper()
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(37).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	require.NoError(t, err)
	var inputs [21]auditfield.Field32
	inputs[2], inputs[3] = auditfield.DigestFields(key.ID())
	inputs[4] = auditfield.Field32FromUint64(1)
	inputs[5], inputs[6], err = key.Point().Coordinates()
	require.NoError(t, err)
	inputs[7] = auditfield.Field32FromUint64(uint64(msgServerTestExpiry))
	inputs[9] = auditfield.Field32FromUint64(uint64(len(fixture.validated.Nullifiers())))
	inputs[10] = auditfield.Field32FromUint64(uint64(len(fixture.validated.Outputs())))
	context, err := auditfield.NewAuditContext(fixture.validated.Kind(), inputs[:])
	require.NoError(t, err)
	count, err := fixture.validated.Kind().PlaintextFieldCount()
	require.NoError(t, err)
	plain, err := auditfield.NewAuditPlain(fixture.validated.Kind(), make([]auditfield.Field32, count))
	require.NoError(t, err)
	envelope, _, witness, err := auditfield.EncryptAuditForProver(context, plain)
	require.NoError(t, err)
	witness.Clear()
	raw, err := envelope.Bytes()
	require.NoError(t, err)
	return raw
}

func TestExternalAuditPayloadCannotReuseVerifierPublicInputs(t *testing.T) {
	// executeAudit passes these exact PI23 values to the existing Groth16
	// verifier. A semantically valid payload change must therefore change the
	// public witness instead of reaching apply under the original proof.
	for _, kind := range []auditfield.Kind{auditfield.KindDeposit, auditfield.KindWithdraw, auditfield.KindTransfer2x2, auditfield.KindBatch16x32} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			fixture := newAuditApplyBoundaryFixture(t, kind)
			envelope := validAuditBoundaryEnvelope(t, fixture)
			switch msg := fixture.msg.(type) {
			case *privacyv2.MsgDeposit:
				msg.Audit.Envelope = envelope
			case *privacyv2.MsgWithdraw:
				msg.Audit.Envelope = envelope
			case *privacyv2.MsgTransfer:
				msg.Audit.Envelope = envelope
			case *privacyv2.MsgBatchTransfer:
				msg.Audit.Envelope = envelope
			}
			validatedMessage, err := privacytypes.ValidateAuditMessage(fixture.msg)
			require.NoError(t, err)
			fixture.validated = validatedMessage
			installAuditBoundaryInitialKey(t, fixture)
			original, err := fixture.k.buildAuditPublic(fixture.ctx, fixture.validated)
			require.NoError(t, err)
			_, err = auditPublicWitness(original.PI)
			require.NoError(t, err)

			changed := proto.Clone(fixture.msg).(sdk.Msg)
			switch msg := changed.(type) {
			case *privacyv2.MsgDeposit:
				msg.Amount = "8uclair"
			case *privacyv2.MsgWithdraw:
				msg.Amount = "8uclair"
			case *privacyv2.MsgTransfer:
				msg.Outputs[0].Commitment = fixedFieldBytes(901)
			case *privacyv2.MsgBatchTransfer:
				msg.Outputs[0].Commitment = fixedFieldBytes(902)
			}
			validated, err := privacytypes.ValidateAuditMessage(changed)
			require.NoError(t, err)
			altered, err := fixture.k.buildAuditPublic(fixture.ctx, validated)
			require.NoError(t, err)
			_, err = auditPublicWitness(altered.PI)
			require.NoError(t, err)
			require.NotEqual(t, original.PI, altered.PI, "the existing verifier must receive different public inputs for a changed external payload")
		})
	}
}

func TestAuditApplyBoundaryRollsBackEveryMidApplyStage(t *testing.T) {
	for _, kind := range []auditfield.Kind{auditfield.KindDeposit, auditfield.KindWithdraw, auditfield.KindTransfer2x2, auditfield.KindBatch16x32} {
		for _, stage := range []string{"audit_bank", "audit_tree", "audit_scan"} {
			t.Run(fmt.Sprint(kind)+"/"+stage, func(t *testing.T) {
				fixture := newAuditApplyBoundaryFixture(t, kind)
				before := snapshotAuditApplyBoundary(t, fixture)
				fixture.k.batchTransitionHook = func(got string) error {
					if got == stage {
						return errors.New("injected apply failure")
					}
					return nil
				}
				_, err := fixture.apply()
				require.ErrorContains(t, err, "injected apply failure")
				require.Equal(t, before, snapshotAuditApplyBoundary(t, fixture))
			})
		}
	}
}

func TestAuditManagementCommandsRemainAllowed(t *testing.T) {
	fixture := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	k, ctx := fixture.k, fixture.ctx
	network, err := k.auditNetwork(ctx)
	require.NoError(t, err)
	initialSecret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(37).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	initialKey, err := privacycrypto.AuditKeyFromSecret(initialSecret)
	require.NoError(t, err)
	initialPoP, err := privacycrypto.CreateAuditPoP96(initialSecret, network, 1, 0)
	require.NoError(t, err)
	initialPoPBytes, err := initialPoP.Bytes()
	require.NoError(t, err)
	initial := &auditKeyRecord{Epoch: 1, KeyID: initialKey.ID(), Suite: 2, PK: initialKey.Point().Bytes(), ActivationHeight: 0, RegisteredHeight: 0, Origin: auditOrigin{Kind: 3}, PoP: initialPoPBytes}
	encoded, err := encodeAuditKey(initial, network, 1)
	require.NoError(t, err)
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(auditEpochKey(auditEpochPrefix, 1), encoded))
	active := make([]byte, 18)
	binary.BigEndian.PutUint16(active, 1)
	binary.BigEndian.PutUint64(active[2:10], 1)
	require.NoError(t, store.Set([]byte{auditActivePrefix}, active))

	nextSecret, err := privacycrypto.ImportAuditSecretKeyBE32(big.NewInt(41).FillBytes(make([]byte, 32)))
	require.NoError(t, err)
	nextKey, err := privacycrypto.AuditKeyFromSecret(nextSecret)
	require.NoError(t, err)
	nextPoP, err := privacycrypto.CreateAuditPoP96(nextSecret, network, 2, 80)
	require.NoError(t, err)
	nextPoPBytes, err := nextPoP.Bytes()
	require.NoError(t, err)
	server := auditMsgServer{Keeper: *k}
	authority := k.audit.authority
	_, err = server.ScheduleAuditKeyEpoch(sdk.WrapSDKContext(ctx), &privacyv2.MsgScheduleAuditKeyEpoch{Authority: authority, Epoch: 2, ActivationHeight: 80, PublicKey: nextKey.Point().Bytes(), Suite: 2, PossessionProof: nextPoPBytes})
	require.NoError(t, err)
	_, err = server.CancelPendingAuditEpoch(sdk.WrapSDKContext(ctx), &privacyv2.MsgCancelPendingAuditEpoch{Authority: authority, Epoch: 2})
	require.NoError(t, err)
	_, err = server.SetPrivacyHalt(sdk.WrapSDKContext(ctx), &privacyv2.MsgSetPrivacyHalt{Authority: authority, Halted: true})
	require.NoError(t, err)
	_, err = server.SetPrivacyHalt(sdk.WrapSDKContext(ctx), &privacyv2.MsgSetPrivacyHalt{Authority: authority, Halted: false})
	require.NoError(t, err)
	halted, err := k.auditHalted(ctx)
	require.NoError(t, err)
	require.False(t, halted)
	counts := map[string]int{}
	for _, event := range ctx.EventManager().Events() {
		counts[event.Type]++
	}
	require.Equal(t, 1, counts[privacytypes.EventTypeAuditKeyScheduled])
	require.Equal(t, 1, counts[privacytypes.EventTypeAuditKeyCancelled])
	require.Equal(t, 2, counts[privacytypes.EventTypePrivacyHaltChanged])
}
