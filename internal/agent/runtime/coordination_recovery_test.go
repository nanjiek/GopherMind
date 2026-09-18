package runtime

import (
	"context"
	"testing"
	"time"
)

func TestCoordinationRecoveryReclaimsBothDurableLeases(t *testing.T) {
	tasks := &recoveringTaskBoard{count: 2}
	mailbox := &recoveringMailbox{count: 3}
	result, err := (CoordinationRecovery{Tasks: tasks, Mailbox: mailbox}).Recover(context.Background(), Metadata{TenantID: "tenant-a", UserID: "user-a"}, "run-a", time.Now())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if result.RecoveredTasks != 2 || result.RecoveredMessages != 3 || result.ExpiredDependencies != 4 {
		t.Fatalf("Recover() = %#v", result)
	}
}

type recoveringTaskBoard struct{ count int }

func (s *recoveringTaskBoard) Claim(context.Context, Metadata, string, int64, string, time.Duration) (TaskLease, error) {
	return TaskLease{}, nil
}
func (s *recoveringTaskBoard) Complete(context.Context, Metadata, string, int64, int64, TaskCompletion) (AgentTask, error) {
	return AgentTask{}, nil
}
func (s *recoveringTaskBoard) RecoverExpired(context.Context, Metadata, string, time.Time) (int, error) {
	return s.count, nil
}
func (s *recoveringTaskBoard) ExpireBlocked(context.Context, Metadata, string, time.Time) (int, error) {
	return 4, nil
}

type recoveringMailbox struct{ count int }

func (s *recoveringMailbox) Enqueue(context.Context, AgentMessage) (AgentMessage, error) {
	return AgentMessage{}, nil
}
func (s *recoveringMailbox) Claim(context.Context, Metadata, string, string, string, time.Duration) (MailboxDelivery, error) {
	return MailboxDelivery{}, nil
}
func (s *recoveringMailbox) Acknowledge(context.Context, Metadata, string, int64, int64) (AgentMessage, error) {
	return AgentMessage{}, nil
}
func (s *recoveringMailbox) RecoverExpired(context.Context, Metadata, string, time.Time) (int, error) {
	return s.count, nil
}
