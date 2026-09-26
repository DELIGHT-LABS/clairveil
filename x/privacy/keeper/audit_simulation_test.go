package keeper

import (
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAuditAssetSimulationContextAndOrigin(t *testing.T) {
	f := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	for _, ctx := range []sdk.Context{f.ctx, f.ctx.WithTxBytes(nil).WithIsCheckTx(true), f.ctx.WithIsReCheckTx(true)} {
		marked := f.k.WithAuditAssetSimulation(ctx)
		require.False(t, isAuditAssetSimulation(ctx))
		require.True(t, isAuditAssetSimulation(marked))
		require.Equal(t, ctx.TxBytes(), marked.TxBytes())
		require.Equal(t, ctx.IsCheckTx(), marked.IsCheckTx())
		require.Equal(t, ctx.IsReCheckTx(), marked.IsReCheckTx())
		require.Equal(t, ctx.ExecMode(), marked.ExecMode())
		require.Same(t, ctx.MultiStore(), marked.MultiStore())
		require.Same(t, ctx.GasMeter(), marked.GasMeter())
		require.Equal(t, ctx.BlockGasMeter(), marked.BlockGasMeter())
		require.Same(t, ctx.EventManager(), marked.EventManager())
		original, originalErr := f.k.auditExecutionOrigin(ctx)
		actual, actualErr := f.k.auditAssetExecutionOrigin(ctx)
		require.Equal(t, original, actual)
		require.Equal(t, originalErr, actualErr)
		origin, err := f.k.auditAssetExecutionOrigin(marked)
		require.NoError(t, err)
		require.EqualValues(t, 1, origin.Kind)
		require.Len(t, origin.Anchor, 32)
		require.NotEqual(t, [32]byte{}, origin.Anchor)
		require.EqualValues(t, ctx.BlockHeight(), origin.Height)
		again, err := f.k.auditAssetExecutionOrigin(marked.WithTxBytes([]byte("different")))
		require.NoError(t, err)
		require.Equal(t, origin, again)
	}
	marked := f.k.WithAuditAssetSimulation(f.ctx.WithTxBytes(nil).WithIsCheckTx(true))
	_, err := f.k.auditExecutionOrigin(marked)
	require.ErrorContains(t, err, "delivery context")
	require.ErrorContains(t, f.k.checkAuditAuthority(marked, f.k.audit.authority), "delivery context")
	require.ErrorContains(t, f.k.checkAuditAuthority(marked.WithIsCheckTx(false).WithIsReCheckTx(true), f.k.audit.authority), "delivery context")
	require.ErrorContains(t, f.k.checkAuditAuthority(f.k.WithAuditAssetSimulation(f.ctx), "invalid"), "authority")
	_, err = f.k.auditAssetExecutionOrigin(marked.WithBlockHeight(0))
	require.ErrorContains(t, err, "initialization")
	require.NoError(t, auditinit.Run(marked, func(ctx sdk.Context) error {
		_, err := f.k.auditAssetExecutionOrigin(ctx)
		require.ErrorContains(t, err, "initialization")
		return nil
	}))
	f.k.audit = nil
	_, err = f.k.auditAssetExecutionOrigin(marked)
	require.ErrorContains(t, err, "initialization")
}

func preparedAuditSimulation(t *testing.T, kind auditfield.Kind) auditApplyBoundaryFixture {
	t.Helper()
	f := newAuditApplyBoundaryFixture(t, kind)
	envelope := validAuditBoundaryEnvelope(t, f)
	switch m := f.msg.(type) {
	case *v2.MsgDeposit:
		m.Audit.Envelope = envelope
	case *v2.MsgWithdraw:
		m.Audit.Envelope = envelope
	case *v2.MsgTransfer:
		m.Audit.Envelope = envelope
	case *v2.MsgBatchTransfer:
		m.Audit.Envelope = envelope
	}
	var err error
	f.validated, err = privacytypes.ValidateAuditMessage(f.msg)
	require.NoError(t, err)
	installAuditBoundaryInitialKey(t, f)
	isolated, _ := f.ctx.CacheContext()
	f.ctx = f.k.WithAuditAssetSimulation(isolated.WithTxBytes(nil).WithIsCheckTx(true))
	return f
}

func TestAuditAssetSimulationValidation(t *testing.T) {
	f := preparedAuditSimulation(t, auditfield.KindDeposit)
	server := auditMsgServer{Keeper: *f.k}
	unmarked := newAuditApplyBoundaryFixture(t, auditfield.KindDeposit)
	_, err := server.executeAudit(unmarked.ctx.WithTxBytes(nil), f.msg)
	require.ErrorContains(t, err, "original tx")
	_, err = server.executeAudit(f.ctx.WithBlockHeight(0), f.msg)
	require.ErrorContains(t, err, "initialization")
	require.NoError(t, auditinit.Run(f.ctx, func(ctx sdk.Context) error {
		_, err := server.executeAudit(ctx, f.msg)
		require.ErrorContains(t, err, "initialization")
		return nil
	}))
	disabled := auditMsgServer{Keeper: *f.k}
	disabled.audit = nil
	_, err = disabled.executeAudit(f.ctx, f.msg)
	require.ErrorContains(t, err, "not configured")
	before := snapshotAuditApplyBoundary(t, f)
	gas := f.ctx.GasMeter().GasConsumed()
	_, err = server.executeAudit(f.ctx, f.msg)
	// The fixture has an invalid proof: this checks the real verifier entry and
	// rejection, not successful cryptographic verification or a proving setup.
	require.ErrorContains(t, err, "audit proof verification failed")
	require.Greater(t, f.ctx.GasMeter().GasConsumed(), gas)
	require.Equal(t, before, snapshotAuditApplyBoundary(t, f))
	_, err = server.executeAudit(f.ctx.WithBlockTime(time.Unix(msgServerTestExpiry, 0)), f.msg)
	require.ErrorContains(t, err, "expired")
	m := f.msg.(*v2.MsgDeposit)
	m.Audit.Epoch++
	_, err = server.executeAudit(f.ctx, m)
	require.ErrorContains(t, err, "not the active key")
	m.Audit.Epoch--
	m.Creator = f.k.audit.authority
	_, err = server.executeAudit(f.ctx, m)
	require.ErrorContains(t, err, "governance cannot execute")
}

func TestAuditAssetSimulationApply(t *testing.T) {
	for _, kind := range []auditfield.Kind{auditfield.KindDeposit, auditfield.KindWithdraw, auditfield.KindTransfer2x2, auditfield.KindBatch16x32} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			f := preparedAuditSimulation(t, kind)
			record, err := f.k.buildAuditPublic(f.ctx, f.validated)
			require.NoError(t, err)
			f.record = record
			before := snapshotAuditApplyBoundary(t, f)
			// Reuse the existing synthetic verified-transition fixture to test apply.
			// No successful cryptographic proof is claimed by this test.
			_, err = f.k.applyPrivacyTransition(f.ctx.WithBlockHeight(f.ctx.BlockHeight()+1), &verifiedAuditTransition{owner: f.k.audit, message: f.validated, record: record, projectedBound: 1 << 20})
			require.ErrorContains(t, err, "execution context changed")
			require.Equal(t, before, snapshotAuditApplyBoundary(t, f))
			bound, err := prechargeAuditMessage(f.ctx, f.msg)
			require.NoError(t, err)
			gas := f.ctx.GasMeter().GasConsumed()
			sequence, err := f.k.applyPrivacyTransition(f.ctx, &verifiedAuditTransition{owner: f.k.audit, message: f.validated, record: record, projectedBound: bound})
			require.NoError(t, err)
			require.Greater(t, f.ctx.GasMeter().GasConsumed(), gas, "apply retains metered KV writes")
			after := snapshotAuditApplyBoundary(t, f)
			require.EqualValues(t, 1, sequence)
			require.EqualValues(t, len(record.Outputs), after.leaves)
			require.EqualValues(t, 1, after.sequence)
			for _, found := range after.nullifiers {
				require.True(t, found)
			}
			for _, found := range after.commitments {
				require.True(t, found)
			}
			if kind == auditfield.KindDeposit {
				require.Equal(t, "93", after.actor)
				require.Equal(t, "27", after.module)
			}
			if kind == auditfield.KindWithdraw {
				require.Equal(t, "7", after.recipient)
				require.Equal(t, "13", after.module)
			}
			summary, err := f.k.getPrivacyScanSummaryV2(f.ctx, f.ctx.BlockHeight(), sequence)
			require.NoError(t, err)
			require.Len(t, summary.TxHash, 32)
			require.Equal(t, record.Origin.Anchor[:], summary.TxHash)
			events := f.ctx.EventManager().Events()
			require.Len(t, events, 1)
			attrs := map[string]string{}
			for _, attr := range events[0].Attributes {
				attrs[attr.Key] = attr.Value
			}
			require.Equal(t, hex.EncodeToString(summary.TxHash), attrs["tx_hash"])
			_, err = f.k.buildAuditPublic(f.ctx, f.validated)
			require.Error(t, err, "later calls see published commitments/nullifiers")
		})
	}
}
