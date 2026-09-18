package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CoordinationRecovery resumes durable coordination after a process restart.
// It requeues only expired Task and Mailbox leases; it does not execute an
// Agent, rerun a completed Task, or acknowledge a message on a worker's
// behalf. Static workflow checkpoint recovery remains a separate runner.
type CoordinationRecovery struct {
	Tasks   TaskBoard
	Mailbox MailboxStore
}

type CoordinationRecoveryResult struct {
	RecoveredTasks      int
	RecoveredMessages   int
	ExpiredDependencies int
}

func (r CoordinationRecovery) Recover(ctx context.Context, scope Metadata, runID string, now time.Time) (CoordinationRecoveryResult, error) {
	if r.Tasks == nil || r.Mailbox == nil {
		return CoordinationRecoveryResult{}, errors.New("task board and mailbox are required for coordination recovery")
	}
	if runID == "" || scope.TenantID == "" || scope.UserID == "" {
		return CoordinationRecoveryResult{}, fmt.Errorf("%w: run ID and trusted tenant/user scope are required", ErrInvalidTaskDAG)
	}
	tasks, err := r.Tasks.RecoverExpired(ctx, scope, runID, now)
	if err != nil {
		return CoordinationRecoveryResult{}, err
	}
	messages, err := r.Mailbox.RecoverExpired(ctx, scope, runID, now)
	if err != nil {
		return CoordinationRecoveryResult{}, err
	}
	expired, err := r.Tasks.ExpireBlocked(ctx, scope, runID, now)
	if err != nil {
		return CoordinationRecoveryResult{}, err
	}
	return CoordinationRecoveryResult{RecoveredTasks: tasks, RecoveredMessages: messages, ExpiredDependencies: expired}, nil
}
