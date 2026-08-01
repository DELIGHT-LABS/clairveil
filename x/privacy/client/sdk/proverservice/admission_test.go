package proverservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

func TestAdmissionControllerBoundsQueuePerCircuit(t *testing.T) {
	controller, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 1},
		privacyprovertransport.WithdrawProofCircuitID: {MaxInFlight: 1, MaxQueued: 0},
	}})
	require.NoError(t, err)

	first, err := controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)
	first.StartProve()

	secondResult := make(chan privacyprovertransport.ProofPermit, 1)
	secondErr := make(chan error, 1)
	go func() {
		permit, acquireErr := controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
		secondResult <- permit
		secondErr <- acquireErr
	}()
	require.Eventually(t, func() bool {
		return controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID].Queued == 1
	}, time.Second, time.Millisecond)

	_, err = controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.ErrorIs(t, err, ErrAdmissionQueueFull)

	// Capacity is circuit-local: a saturated JoinSplit gate does not consume a
	// Spend permit.
	withdraw, err := controller.Acquire(context.Background(), privacyprovertransport.WithdrawProofCircuitID)
	require.NoError(t, err)
	withdraw.Release()

	first.Release()
	second := <-secondResult
	require.NoError(t, <-secondErr)
	require.NotNil(t, second)
	second.StartProve()
	second.Release()

	metrics := controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Zero(t, metrics.InFlight)
	require.Zero(t, metrics.Queued)
	require.Zero(t, metrics.Proving)
	require.Equal(t, uint64(2), metrics.TotalAdmitted)
	require.Equal(t, uint64(1), metrics.TotalQueued)
	require.Equal(t, uint64(1), metrics.TotalRejected)
	require.Equal(t, uint64(2), metrics.TotalProveCompleted)
}

func TestAdmissionPermitIgnoresRequestCancellationUntilRelease(t *testing.T) {
	controller, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 0},
	}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	permit, err := controller.Acquire(ctx, privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)
	permit.StartProve()
	cancel()

	metrics := controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Equal(t, 1, metrics.InFlight)
	require.Equal(t, 1, metrics.Proving)
	_, err = controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.ErrorIs(t, err, ErrAdmissionQueueFull)

	permit.Release()
	metrics = controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Zero(t, metrics.InFlight)
	require.Zero(t, metrics.Proving)
	require.Equal(t, uint64(1), metrics.TotalProveCompleted)
}

func TestAdmissionMetricsMeasureProveLifetime(t *testing.T) {
	var clockMu sync.Mutex
	now := time.Unix(1_900_000_000, 0)
	controller, err := NewAdmissionController(AdmissionConfig{
		Circuits: map[string]CircuitAdmissionConfig{
			privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 0},
		},
		Now: func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return now
		},
	})
	require.NoError(t, err)
	permit, err := controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)
	permit.StartProve()
	clockMu.Lock()
	now = now.Add(2500 * time.Millisecond)
	clockMu.Unlock()
	permit.Release()

	metrics := controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Equal(t, uint64((2500 * time.Millisecond).Nanoseconds()), metrics.ProveNanoseconds)
}

func TestAdmissionPermitReleaseIsIdempotentAndLateStartIsIgnored(t *testing.T) {
	controller, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 0},
	}})
	require.NoError(t, err)
	permit, err := controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)
	permit.Release()
	permit.Release()
	permit.StartProve()

	metrics := controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Zero(t, metrics.InFlight)
	require.Zero(t, metrics.Proving)
	require.Equal(t, uint64(1), metrics.TotalReleased)
	require.Zero(t, metrics.TotalProveCompleted)
	next, err := controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)
	next.Release()
}

func TestAdmissionCanceledWhileQueuedDoesNotLeakCapacity(t *testing.T) {
	controller, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 1},
	}})
	require.NoError(t, err)
	first, err := controller.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	var queuedErr error
	go func() {
		defer wg.Done()
		_, queuedErr = controller.Acquire(ctx, privacyprovertransport.TransferProofCircuitID)
	}()
	require.Eventually(t, func() bool {
		return controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID].Queued == 1
	}, time.Second, time.Millisecond)
	cancel()
	wg.Wait()
	require.ErrorIs(t, queuedErr, context.Canceled)

	first.Release()
	metrics := controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Zero(t, metrics.InFlight)
	require.Zero(t, metrics.Queued)
	require.Equal(t, uint64(1), metrics.TotalCanceled)
}

func TestAdmissionRejectsAlreadyCanceledRequestWithoutTakingPermit(t *testing.T) {
	controller, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 1},
	}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = controller.Acquire(ctx, privacyprovertransport.TransferProofCircuitID)
	require.ErrorIs(t, err, context.Canceled)
	metrics := controller.Snapshot().Circuits[privacyprovertransport.TransferProofCircuitID]
	require.Zero(t, metrics.InFlight)
	require.Equal(t, uint64(1), metrics.TotalCanceled)
}

func TestAdmissionConfigurationRejectsUnboundedValues(t *testing.T) {
	_, err := NewAdmissionController(AdmissionConfig{})
	require.Error(t, err)
	_, err = NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		"spend": {MaxInFlight: 0, MaxQueued: 1},
	}})
	require.Error(t, err)
	_, err = NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		"spend": {MaxInFlight: 1, MaxQueued: -1},
	}})
	require.Error(t, err)
}

func TestDefaultAdmissionConfigSeparatesDepositAndBatchCapacity(t *testing.T) {
	config := DefaultAdmissionConfig()
	require.Contains(t, config.Circuits, privacyprovertransport.DepositProofCircuitID)
	require.Equal(t, DefaultMaxInFlightPerCircuit, config.Circuits[privacyprovertransport.DepositProofCircuitID].MaxInFlight)
	require.Equal(t, DefaultMaxQueuedPerCircuit, config.Circuits[privacyprovertransport.DepositProofCircuitID].MaxQueued)
	require.Contains(t, config.Circuits, privacyprovertransport.BatchTransferProofCircuitID)
	require.Equal(t, DefaultMaxInFlightPerCircuit, config.Circuits[privacyprovertransport.BatchTransferProofCircuitID].MaxInFlight)
	require.Equal(t, DefaultMaxQueuedPerCircuit, config.Circuits[privacyprovertransport.BatchTransferProofCircuitID].MaxQueued)
}
