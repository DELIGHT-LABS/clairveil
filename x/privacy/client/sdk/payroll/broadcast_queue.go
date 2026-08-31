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

type PreparedMessageBroadcast struct {
	Identity BroadcastResult
	Submit   func(context.Context) (*BroadcastResult, error)
}

// PreparedMessageBroadcaster performs account lookup, gas estimation, signing,
// and tx encoding before the durable attempt marker. Submit must contain only
// the external broadcast call.
type PreparedMessageBroadcaster interface {
	PrepareBroadcastMessages(ctx context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error)
}

var ErrPreparedBroadcastUnsupported = errors.New("prepared broadcast boundary is unsupported")

type BroadcastWorker struct {
	Reservation      privacyreservation.Service
	Broadcaster      MessageBroadcaster
	Assembler        TransferMessageAssembler
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
	if err := validateProofResultArtifact(result, w.Assembler); err != nil {
		return nil, fmt.Errorf("proof result is invalid: %w", err)
	}

	refs, operationIDs, err := preflightSubmissionState(ctx, w.Reservation, []ProofResult{result}, w.LeaseOwner, w.LeaseTTL)
	if err != nil {
		return nil, err
	}

	return broadcastWithSubmissionLeaseHeartbeat(ctx, w.Reservation, refs, w.LeaseTTL, func(broadcastCtx context.Context, commit submissionLeaseCommit, refresh submissionLeaseRefresh) (*BroadcastResult, error) {
		if err := ensureNullifiersUnspent(broadcastCtx, resolveBroadcastNullifierChecker(w.NullifierChecker, w.Broadcaster), []ProofResult{result}); err != nil {
			return nil, err
		}
		if err := refresh(); err != nil {
			return nil, err
		}
		preparedBroadcast, err := prepareMessageBroadcast(broadcastCtx, w.Broadcaster, result.Message)
		if err != nil {
			return nil, err
		}
		attemptReservations, attemptOperations, err := w.Reservation.MarkBroadcastAttempting(
			broadcastCtx,
			refs,
			operationIDs,
			broadcastAttemptStart(preparedBroadcast.Identity),
		)
		if err != nil {
			return nil, err
		}
		broadcast, err := preparedBroadcast.Submit(broadcastCtx)
		if broadcast == nil && err == nil {
			cause := fmt.Errorf("message broadcaster returned nil result")
			err := &ManualReviewBroadcastError{Cause: cause}
			return nil, errors.Join(err, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(cause, heartbeatErr))
			}))
		}
		broadcast, identityErr := mergeBroadcastResultWithStoredIdentity(broadcast, attemptReservations, attemptOperations)
		if identityErr != nil {
			ambiguityErr := &ManualReviewBroadcastError{Cause: identityErr}
			return broadcast, errors.Join(err, ambiguityErr, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(identityErr, heartbeatErr))
			}))
		}
		if err != nil {
			if broadcast != nil {
				err = errors.Join(err, commit(func(commitCtx context.Context, heartbeatErr error) error {
					return markProofResultsBroadcastUnknown(commitCtx, w.Reservation, refs, operationIDs, broadcast, errors.Join(err, heartbeatErr))
				}))
			} else {
				manualReviewErr := &ManualReviewBroadcastError{Cause: err}
				err = errors.Join(manualReviewErr, commit(func(commitCtx context.Context, heartbeatErr error) error {
					return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(err, heartbeatErr))
				}))
			}
			return broadcast, err
		}
		if broadcast == nil {
			cause := fmt.Errorf("message broadcaster returned nil result")
			err := &ManualReviewBroadcastError{Cause: cause}
			return nil, errors.Join(err, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(cause, heartbeatErr))
			}))
		}
		if broadcast.Code != 0 {
			txErr := broadcastCodeError(broadcast)
			return broadcast, errors.Join(txErr, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastUnknown(commitCtx, w.Reservation, refs, operationIDs, broadcast, errors.Join(txErr, heartbeatErr))
			}))
		}
		if err := commit(func(commitCtx context.Context, heartbeatErr error) error {
			return markProofResultsSubmitted(commitCtx, w.Reservation, refs, operationIDs, broadcast, heartbeatErr)
		}); err != nil {
			return broadcast, err
		}
		return broadcast, nil
	})
}
