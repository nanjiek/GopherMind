package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"gophermind/internal/agent/runtime"
)

// DurableMailboxStore persists at-least-once handoffs. It is not an external
// queue adapter: acknowledgement records delivery only, never Task success.
type DurableMailboxStore struct{ db *gorm.DB }

var _ runtime.MailboxStore = (*DurableMailboxStore)(nil)

func NewDurableMailboxStore(db *gorm.DB) *DurableMailboxStore { return &DurableMailboxStore{db: db} }

type mailboxMessageRow struct {
	ID             string          `gorm:"column:id;primaryKey"`
	RunID          string          `gorm:"column:run_id"`
	TaskID         *string         `gorm:"column:task_id"`
	SenderAgentID  string          `gorm:"column:sender_agent_id"`
	TargetAgentID  string          `gorm:"column:target_agent_id"`
	IdempotencyKey string          `gorm:"column:idempotency_key"`
	Payload        json.RawMessage `gorm:"column:payload;type:jsonb"`
	Status         string          `gorm:"column:status"`
	Revision       int64           `gorm:"column:revision"`
	LeaseOwner     string          `gorm:"column:lease_owner"`
	LeaseEpoch     int64           `gorm:"column:lease_epoch"`
	LeaseExpiresAt *time.Time      `gorm:"column:lease_expires_at"`
	Attempts       int             `gorm:"column:attempts"`
	DeliveredAt    *time.Time      `gorm:"column:delivered_at"`
	CreatedAt      time.Time       `gorm:"column:created_at"`
	UpdatedAt      time.Time       `gorm:"column:updated_at"`
}

func (mailboxMessageRow) TableName() string { return "agent_mailbox_messages" }

func (s *DurableMailboxStore) Enqueue(ctx context.Context, message runtime.AgentMessage) (runtime.AgentMessage, error) {
	if s == nil || s.db == nil {
		return runtime.AgentMessage{}, errors.New("durable mailbox store is nil")
	}
	if err := runtime.ValidateMailboxMessage(message); err != nil {
		return runtime.AgentMessage{}, err
	}
	if err := validateMailboxIDs(message); err != nil {
		return runtime.AgentMessage{}, err
	}
	message = cloneStoredMessage(message)
	message.Status = runtime.MailboxQueued
	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runCount int64
		if err := checkpointScopeQuery(tx.Model(&workflowRunRow{}), message.Scope, message.RunID).Count(&runCount).Error; err != nil {
			return err
		}
		if runCount != 1 {
			return runtime.ErrMailboxNotFound
		}
		if message.TaskID != "" {
			var taskCount int64
			if err := tx.Model(&agentTaskRow{}).Where("id = ? AND run_id = ?", message.TaskID, message.RunID).Count(&taskCount).Error; err != nil {
				return err
			}
			if taskCount != 1 {
				return runtime.ErrMailboxNotFound
			}
		}
		row := mailboxRow(message, now)
		return tx.Create(&row).Error
	})
	if err != nil {
		if errors.Is(err, runtime.ErrMailboxNotFound) {
			return runtime.AgentMessage{}, err
		}
		if isPostgresUniqueViolation(err) {
			return runtime.AgentMessage{}, fmt.Errorf("%w: message ID or Run idempotency key already exists", runtime.ErrMailboxConflict)
		}
		return runtime.AgentMessage{}, err
	}
	return message, nil
}

// Claim returns one queued or expired delivery for a target Agent. SKIP LOCKED
// permits several worker processes to pull without double-delivering a live
// lease; an expired delivery remains eligible for retry.
func (s *DurableMailboxStore) Claim(ctx context.Context, scope runtime.Metadata, runID, targetAgentID, workerID string, leaseDuration time.Duration) (runtime.MailboxDelivery, error) {
	if s == nil || s.db == nil {
		return runtime.MailboxDelivery{}, errors.New("durable mailbox store is nil")
	}
	if err := validateMailboxClaim(scope, runID, targetAgentID, workerID, leaseDuration); err != nil {
		return runtime.MailboxDelivery{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(leaseDuration)
	var delivery runtime.MailboxDelivery
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row mailboxMessageRow
		result := tx.Raw(`SELECT message.* FROM agent_mailbox_messages AS message
			JOIN agent_runs AS run ON run.id = message.run_id
			WHERE message.run_id = ? AND message.target_agent_id = ?
			AND run.tenant_id = ? AND run.user_id = ?
			AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?
			AND (message.status = ? OR (message.status = ? AND message.lease_expires_at <= ?))
			ORDER BY message.created_at, message.id FOR UPDATE SKIP LOCKED LIMIT 1`,
			runID, targetAgentID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID), runtime.MailboxQueued, runtime.MailboxClaimed, now).Scan(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtime.ErrMailboxNotFound
		}
		nextRevision, nextEpoch := row.Revision+1, row.LeaseEpoch+1
		result = tx.Model(&mailboxMessageRow{}).Where("id = ? AND revision = ?", row.ID, row.Revision).Updates(map[string]any{
			"status": string(runtime.MailboxClaimed), "revision": nextRevision, "lease_owner": workerID,
			"lease_epoch": nextEpoch, "lease_expires_at": expiresAt, "attempts": row.Attempts + 1, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtime.ErrMailboxConflict
		}
		delivery = runtime.MailboxDelivery{Message: messageFromRow(row, scope), WorkerID: workerID, FencingToken: nextEpoch, ExpiresAt: expiresAt}
		delivery.Message.Status = runtime.MailboxClaimed
		delivery.Message.Revision = nextRevision
		return nil
	})
	if err != nil {
		return runtime.MailboxDelivery{}, err
	}
	return delivery, nil
}

// Acknowledge records a completed delivery only for the current unexpired
// claim. It has no Task Board side effect.
func (s *DurableMailboxStore) Acknowledge(ctx context.Context, scope runtime.Metadata, messageID string, expectedRevision, fencingToken int64) (runtime.AgentMessage, error) {
	if s == nil || s.db == nil {
		return runtime.AgentMessage{}, errors.New("durable mailbox store is nil")
	}
	if err := validateMailboxAcknowledge(scope, messageID, expectedRevision, fencingToken); err != nil {
		return runtime.AgentMessage{}, err
	}
	now := time.Now().UTC()
	var acknowledged runtime.AgentMessage
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadScopedMessageForUpdate(tx, scope, messageID)
		if err != nil {
			return err
		}
		if row.Revision != expectedRevision {
			return runtime.ErrMailboxConflict
		}
		if row.Status != string(runtime.MailboxClaimed) || row.LeaseEpoch != fencingToken || row.LeaseExpiresAt == nil || !row.LeaseExpiresAt.After(now) {
			return runtime.ErrMailboxLease
		}
		nextRevision := row.Revision + 1
		result := tx.Model(&mailboxMessageRow{}).Where("id = ? AND revision = ? AND lease_epoch = ?", messageID, expectedRevision, fencingToken).Updates(map[string]any{
			"status": string(runtime.MailboxDelivered), "revision": nextRevision, "delivered_at": now,
			"lease_owner": "", "lease_expires_at": nil, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtime.ErrMailboxConflict
		}
		acknowledged = messageFromRow(row, scope)
		acknowledged.Status, acknowledged.Revision = runtime.MailboxDelivered, nextRevision
		return nil
	})
	if err != nil {
		return runtime.AgentMessage{}, err
	}
	return acknowledged, nil
}

func (s *DurableMailboxStore) RecoverExpired(ctx context.Context, scope runtime.Metadata, runID string, now time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("durable mailbox store is nil")
	}
	if err := validateTaskDAGScopeIDs(scope, runID); err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := s.db.WithContext(ctx).Exec(`UPDATE agent_mailbox_messages AS message SET
		status = ?, revision = message.revision + 1, lease_owner = '', lease_expires_at = NULL, updated_at = ?
		FROM agent_runs AS run
		WHERE message.run_id = run.id AND message.run_id = ? AND run.tenant_id = ? AND run.user_id = ?
		AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?
		AND message.status = ? AND message.lease_expires_at <= ?`, runtime.MailboxQueued, now, runID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID), runtime.MailboxClaimed, now)
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func validateMailboxIDs(message runtime.AgentMessage) error {
	if err := validateTaskDAGScopeIDs(message.Scope, message.RunID); err != nil {
		return fmt.Errorf("%w: %v", runtime.ErrInvalidMailboxMessage, err)
	}
	if _, err := uuid.Parse(message.MessageID); err != nil {
		return fmt.Errorf("%w: message ID must be a UUID: %v", runtime.ErrInvalidMailboxMessage, err)
	}
	if message.TaskID != "" {
		if _, err := uuid.Parse(message.TaskID); err != nil {
			return fmt.Errorf("%w: task ID must be a UUID: %v", runtime.ErrInvalidMailboxMessage, err)
		}
	}
	return nil
}

func validateMailboxClaim(scope runtime.Metadata, runID, targetAgentID, workerID string, leaseDuration time.Duration) error {
	if err := validateTaskDAGScopeIDs(scope, runID); err != nil {
		return fmt.Errorf("%w: %v", runtime.ErrInvalidMailboxMessage, err)
	}
	if targetAgentID == "" || workerID == "" || leaseDuration <= 0 {
		return fmt.Errorf("%w: target Agent, worker, and positive lease duration are required", runtime.ErrInvalidMailboxMessage)
	}
	return nil
}

func validateMailboxAcknowledge(scope runtime.Metadata, messageID string, expectedRevision, fencingToken int64) error {
	if scope.TenantID == "" || scope.UserID == "" || expectedRevision < 1 || fencingToken < 1 {
		return fmt.Errorf("%w: trusted scope, revision, and fencing token are required", runtime.ErrInvalidMailboxMessage)
	}
	if _, err := uuid.Parse(messageID); err != nil {
		return fmt.Errorf("%w: message ID must be a UUID: %v", runtime.ErrInvalidMailboxMessage, err)
	}
	return nil
}

func loadScopedMessageForUpdate(tx *gorm.DB, scope runtime.Metadata, messageID string) (mailboxMessageRow, error) {
	var row mailboxMessageRow
	result := tx.Raw(`SELECT message.* FROM agent_mailbox_messages AS message
		JOIN agent_runs AS run ON run.id = message.run_id
		WHERE message.id = ? AND run.tenant_id = ? AND run.user_id = ?
		AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?
		FOR UPDATE`, messageID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID)).Scan(&row)
	if result.Error != nil {
		return mailboxMessageRow{}, result.Error
	}
	if result.RowsAffected != 1 {
		return mailboxMessageRow{}, runtime.ErrMailboxNotFound
	}
	return row, nil
}

func mailboxRow(message runtime.AgentMessage, now time.Time) mailboxMessageRow {
	return mailboxMessageRow{ID: message.MessageID, RunID: message.RunID, TaskID: optionalString(message.TaskID), SenderAgentID: message.SenderAgentID, TargetAgentID: message.TargetAgentID,
		IdempotencyKey: message.IdempotencyKey, Payload: append(json.RawMessage(nil), message.Payload...), Status: string(message.Status), Revision: message.Revision, CreatedAt: now, UpdatedAt: now}
}

func messageFromRow(row mailboxMessageRow, scope runtime.Metadata) runtime.AgentMessage {
	return runtime.AgentMessage{MessageID: row.ID, RunID: row.RunID, Scope: scope, SenderAgentID: row.SenderAgentID, TargetAgentID: row.TargetAgentID,
		TaskID: cloneOptional(row.TaskID), IdempotencyKey: row.IdempotencyKey, Payload: append(json.RawMessage(nil), row.Payload...), Status: runtime.MailboxStatus(row.Status), Revision: row.Revision}
}

func cloneStoredMessage(message runtime.AgentMessage) runtime.AgentMessage {
	return runtime.AgentMessage{MessageID: message.MessageID, RunID: message.RunID, Scope: message.Scope, SenderAgentID: message.SenderAgentID, TargetAgentID: message.TargetAgentID,
		TaskID: message.TaskID, IdempotencyKey: message.IdempotencyKey, Payload: append(json.RawMessage(nil), message.Payload...), Status: message.Status, Revision: message.Revision}
}
