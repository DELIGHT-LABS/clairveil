package payroll

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type BroadcastResult struct {
	TxHash          string
	TxBytesHash     string
	SignDocHash     string
	AccountSequence uint64
	Height          int64
	Code            uint32
	RawLog          string
}

type MessageBroadcaster interface {
	BroadcastMessages(ctx context.Context, msgs ...sdk.Msg) (*BroadcastResult, error)
}

type BroadcastWorker struct {
	Reservation      privacyreservation.Service
	Broadcaster      MessageBroadcaster
	NullifierChecker BroadcastNullifierChecker
	LeaseOwner       string
	LeaseTTL         time.Duration
}

func (w BroadcastWorker) SubmitProofResult(ctx context.Context, result ProofResult) (*BroadcastResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if w.Broadcaster == nil {
		return nil, fmt.Errorf("a message broadcaster is required")
	}
	if result.Message == nil {
		return nil, fmt.Errorf("proof result has no transfer message")
	}

	refs, operationIDs, acquiredLeases, err := preflightSubmissionState(ctx, w.Reservation, []ProofResult{result}, w.LeaseOwner, w.LeaseTTL)
	if err != nil {
		return nil, err
	}
	if err := ensureNullifiersUnspent(ctx, resolveBroadcastNullifierChecker(w.NullifierChecker, w.Broadcaster), []ProofResult{result}); err != nil {
		return nil, errors.Join(err, clearAcquiredSubmissionLeases(ctx, w.Reservation, acquiredLeases))
	}

	broadcast, err := broadcastWithSubmissionLeaseHeartbeat(ctx, w.Reservation, refs, w.LeaseTTL, func(broadcastCtx context.Context) (*BroadcastResult, error) {
		return w.Broadcaster.BroadcastMessages(broadcastCtx, result.Message)
	})
	if err != nil {
		if broadcast != nil {
			err = errors.Join(err, markProofResultsBroadcastUnknown(ctx, w.Reservation, refs, operationIDs, broadcast, err))
		}
		return broadcast, err
	}
	if broadcast == nil {
		return nil, fmt.Errorf("message broadcaster returned nil result")
	}
	if broadcast.Code != 0 {
		txErr := broadcastCodeError(broadcast)
		return broadcast, errors.Join(txErr, markProofResultsBroadcastUnknown(ctx, w.Reservation, refs, operationIDs, broadcast, txErr))
	}

	if err := markProofResultsSubmitted(ctx, w.Reservation, refs, operationIDs, broadcast); err != nil {
		return broadcast, err
	}

	return broadcast, nil
}
