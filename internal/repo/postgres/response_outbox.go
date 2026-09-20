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

var ErrResponseOutboxConflict = errors.New("response outbox operation conflicts")

// ResponseOutbox records publish intent only after a response is committed.
// Delivery workers belong to a later adapter and must reauthorize immediately
// before their own external call.
type ResponseOutbox struct{ db *gorm.DB }

func NewResponseOutbox(db *gorm.DB) *ResponseOutbox { return &ResponseOutbox{db: db} }

type ResponseOutboxMessage struct {
	OperationID string
	RunID       string
	Scope       runtime.Metadata
	Topic       string
	Payload     json.RawMessage
}

type outboxRow struct {
	ID          string          `gorm:"column:id;primaryKey"`
	OperationID string          `gorm:"column:operation_id"`
	Topic       string          `gorm:"column:topic"`
	Payload     json.RawMessage `gorm:"column:payload;type:jsonb"`
	Status      string          `gorm:"column:status"`
	CreatedAt   time.Time       `gorm:"column:created_at"`
	UpdatedAt   time.Time       `gorm:"column:updated_at"`
}

func (outboxRow) TableName() string { return "outbox_messages" }

// EnqueueCommittedResponse writes an idempotent publish intent only if the
// exact scoped Run already owns a committed response. It never publishes.
func (o *ResponseOutbox) EnqueueCommittedResponse(ctx context.Context, message ResponseOutboxMessage) error {
	if o == nil || o.db == nil {
		return errors.New("response outbox is nil")
	}
	if err := validResponseOutboxMessage(message); err != nil {
		return err
	}
	return o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var committed int64
		if err := tx.Raw(`SELECT count(*) FROM agent_committed_responses response JOIN agent_runs run ON run.id=response.run_id WHERE response.run_id=? AND run.tenant_id=? AND run.user_id=? AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?`, message.RunID, message.Scope.TenantID, message.Scope.UserID, optionalString(message.Scope.PatientID), optionalString(message.Scope.SessionID)).Scan(&committed).Error; err != nil {
			return err
		}
		if committed != 1 {
			return runtime.ErrTaskDAGNotFound
		}
		row := outboxRow{ID: uuid.NewString(), OperationID: message.OperationID, Topic: message.Topic, Payload: append(json.RawMessage(nil), message.Payload...), Status: "pending", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if err := tx.Create(&row).Error; err != nil {
			if isPostgresUniqueViolation(err) {
				return ErrResponseOutboxConflict
			}
			return err
		}
		return nil
	})
}

func validResponseOutboxMessage(message ResponseOutboxMessage) error {
	if message.OperationID == "" || message.Topic == "" {
		return fmt.Errorf("invalid response outbox operation")
	}
	if err := validateCommittedScope(message.Scope, message.RunID); err != nil {
		return err
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(message.Payload, &payload) != nil || payload == nil {
		return fmt.Errorf("response outbox payload must be a JSON object")
	}
	return nil
}
