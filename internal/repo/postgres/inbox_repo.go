package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	// InboxStatusProcessing means message is being processed.
	InboxStatusProcessing = "processing"
	// InboxStatusSucceeded means message processing is done.
	InboxStatusSucceeded = "succeeded"
	// InboxStatusFailed means retryable failure happened.
	InboxStatusFailed = "failed"
	// InboxStatusDead means message is routed to DLQ.
	InboxStatusDead = "dead"

	// processingLease allows takeover of stale processing records.
	processingLease = 2 * time.Minute
)

// InboxRepository persists consumer inbox records for idempotency and state transitions.
type InboxRepository struct {
	db *gorm.DB
}

// NewInboxRepository builds InboxRepository.
func NewInboxRepository(db *gorm.DB) *InboxRepository {
	return &InboxRepository{db: db}
}

// BeginProcess tries to reserve processing for (consumer, messageID).
// It returns true when caller should continue business processing.
func (r *InboxRepository) BeginProcess(ctx context.Context, consumer string, messageID string, retryCount int) (bool, error) {
	item := ConsumerInboxModel{
		Consumer:   consumer,
		MessageID:  messageID,
		Status:     InboxStatusProcessing,
		RetryCount: retryCount,
	}
	err := r.db.WithContext(ctx).Create(&item).Error
	if err == nil {
		return true, nil
	}
	if !isDuplicateKeyError(err) {
		return false, err
	}

	var existing ConsumerInboxModel
	if err := r.db.WithContext(ctx).
		Where("consumer = ? AND message_id = ?", consumer, messageID).
		First(&existing).Error; err != nil {
		return false, err
	}
	if existing.Status == InboxStatusSucceeded || existing.Status == InboxStatusDead {
		return false, nil
	}

	staleProcessing := existing.Status == InboxStatusProcessing && time.Since(existing.UpdatedAt) > processingLease
	takeoverByRetry := retryCount > existing.RetryCount
	if !staleProcessing && !takeoverByRetry {
		return false, nil
	}

	nextRetryCount := retryCount
	if nextRetryCount < existing.RetryCount {
		nextRetryCount = existing.RetryCount
	}
	return true, r.db.WithContext(ctx).
		Model(&ConsumerInboxModel{}).
		Where("consumer = ? AND message_id = ?", consumer, messageID).
		Updates(map[string]any{
			"status":      InboxStatusProcessing,
			"retry_count": nextRetryCount,
			"last_error":  "",
		}).Error
}

// MarkSucceeded marks a message as succeeded.
func (r *InboxRepository) MarkSucceeded(ctx context.Context, consumer string, messageID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&ConsumerInboxModel{}).
		Where("consumer = ? AND message_id = ?", consumer, messageID).
		Updates(map[string]any{
			"status":       InboxStatusSucceeded,
			"processed_at": &now,
			"last_error":   "",
		}).Error
}

// MarkFailed marks a message as failed and waiting for retry.
func (r *InboxRepository) MarkFailed(ctx context.Context, consumer string, messageID string, retryCount int, lastErr string) error {
	return r.db.WithContext(ctx).
		Model(&ConsumerInboxModel{}).
		Where("consumer = ? AND message_id = ?", consumer, messageID).
		Updates(map[string]any{
			"status":      InboxStatusFailed,
			"retry_count": retryCount,
			"last_error":  lastErr,
		}).Error
}

// MarkDead marks a message as dead-lettered.
func (r *InboxRepository) MarkDead(ctx context.Context, consumer string, messageID string, retryCount int, lastErr string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&ConsumerInboxModel{}).
		Where("consumer = ? AND message_id = ?", consumer, messageID).
		Updates(map[string]any{
			"status":       InboxStatusDead,
			"retry_count":  retryCount,
			"last_error":   lastErr,
			"processed_at": &now,
		}).Error
}

func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
