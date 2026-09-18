package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	DefaultTenantID = "default"

	SessionCreatedV1        = "conversation.session.created.v1"
	MessageAppendedV1       = "conversation.message.appended.v1"
	GenerationStartedV1     = "generation.started.v1"
	GenerationCompletedV1   = "generation.completed.v1"
	GenerationFailedV1      = "generation.failed.v1"
	DocumentStatusChangedV1 = "document.status.changed.v1"
	MemoryRecordChangedV1   = "memory.record.changed.v1"
)

var (
	ErrScopeMismatch     = errors.New("event stream scope mismatch")
	ErrUnsupportedSchema = errors.New("unsupported event schema version")
	ErrInvalidAppend     = errors.New("invalid event append request")
)

type Scope struct {
	TenantID  string
	UserID    string
	PatientID *string
}

func (s Scope) Tenant() string {
	if s.TenantID == "" {
		return DefaultTenantID
	}
	return s.TenantID
}

type Event struct {
	EventID        string
	EventType      string
	SchemaVersion  int
	TenantID       string
	UserID         string
	PatientID      *string
	SessionID      string
	StreamSeq      int64
	RequestID      string
	IdempotencyKey string
	CorrelationID  string
	CausationID    *string
	ActorType      string
	ActorID        string
	Payload        json.RawMessage
	OccurredAt     time.Time
	RecordedAt     time.Time
}

type AppendRequest struct {
	Scope          Scope
	SessionID      string
	EventType      string
	SchemaVersion  int
	RequestID      string
	IdempotencyKey string
	CorrelationID  string
	CausationID    *string
	ActorType      string
	ActorID        string
	Payload        any
	OccurredAt     time.Time
}

type AppendResult struct {
	Event      Event
	Revision   int64
	Idempotent bool
}

type Store interface {
	Append(ctx context.Context, request AppendRequest) (AppendResult, error)
	ReadStream(ctx context.Context, scope Scope, sessionID string, afterSeq int64, limit int) ([]Event, error)
}
