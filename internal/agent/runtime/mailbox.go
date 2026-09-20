package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidMailboxMessage = errors.New("runtime mailbox message is invalid")
	ErrMailboxConflict       = errors.New("runtime mailbox message conflicts")
	ErrMailboxNotFound       = errors.New("runtime mailbox message is not found")
	ErrMailboxLease          = errors.New("runtime mailbox delivery lease conflicts")
)

// MaxMailboxPayloadBytes bounds the structured handoff retained in PostgreSQL.
const MaxMailboxPayloadBytes = 1 << 20

type MailboxStatus string

const (
	MailboxQueued    MailboxStatus = "queued"
	MailboxClaimed   MailboxStatus = "claimed"
	MailboxDelivered MailboxStatus = "delivered"
)

func (status MailboxStatus) valid() bool {
	switch status {
	case MailboxQueued, MailboxClaimed, MailboxDelivered:
		return true
	default:
		return false
	}
}

// AgentMessage is a durable, scope-bound handoff. Delivery acknowledgement is
// intentionally separate from Task completion: a message can be delivered
// without declaring its referenced Task successful.
type AgentMessage struct {
	MessageID      string
	RunID          string
	Scope          Metadata
	SenderAgentID  string
	TargetAgentID  string
	TaskID         string
	IdempotencyKey string
	Payload        json.RawMessage
	Status         MailboxStatus
	Revision       int64
}

// MailboxDelivery is a fenced, expiring claim for a queued message.
type MailboxDelivery struct {
	Message      AgentMessage
	WorkerID     string
	FencingToken int64
	ExpiresAt    time.Time
}

// MailboxStore provides at-least-once durable delivery. Enqueue is atomic with
// message identity; Claim and Acknowledge use revision plus fencing checks.
// It does not call a broker, Agent, Tool, or other external side effect.
type MailboxStore interface {
	Enqueue(context.Context, AgentMessage) (AgentMessage, error)
	Claim(context.Context, Metadata, string, string, string, time.Duration) (MailboxDelivery, error)
	Acknowledge(context.Context, Metadata, string, int64, int64) (AgentMessage, error)
	RecoverExpired(context.Context, Metadata, string, time.Time) (int, error)
}

// ValidateMailboxMessage rejects untrusted identity, malformed handoffs, and
// non-initial lifecycle data before it crosses the persistence boundary.
func ValidateMailboxMessage(message AgentMessage) error {
	if message.MessageID == "" || message.RunID == "" || message.Scope.TenantID == "" || message.Scope.UserID == "" || message.SenderAgentID == "" || message.TargetAgentID == "" || message.IdempotencyKey == "" || message.Revision != 1 {
		return fmt.Errorf("%w: identity, trusted scope, agents, idempotency key, and revision 1 are required", ErrInvalidMailboxMessage)
	}
	if message.Status != "" && message.Status != MailboxQueued {
		return fmt.Errorf("%w: first message status must be queued", ErrInvalidMailboxMessage)
	}
	if len(message.Payload) == 0 || len(message.Payload) > MaxMailboxPayloadBytes || !json.Valid(message.Payload) {
		return fmt.Errorf("%w: payload must be bounded valid JSON", ErrInvalidMailboxMessage)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(message.Payload, &object); err != nil || object == nil {
		return fmt.Errorf("%w: payload must be a JSON object", ErrInvalidMailboxMessage)
	}
	return nil
}

func cloneMailboxMessage(message AgentMessage) AgentMessage {
	message.Payload = append(json.RawMessage(nil), message.Payload...)
	return message
}
