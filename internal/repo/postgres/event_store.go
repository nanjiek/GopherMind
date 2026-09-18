package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"gophermind/internal/session/eventlog"
)

type EventStore struct {
	db *gorm.DB
}

var _ eventlog.Store = (*EventStore)(nil)

func NewEventStore(db *gorm.DB) *EventStore {
	return &EventStore{db: db}
}

type EventStreamModel struct {
	SessionID string `gorm:"type:uuid;primaryKey"`
	TenantID  string
	UserID    string
	PatientID *string
	NextSeq   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (EventStreamModel) TableName() string { return "event_streams" }

type ClinicalEventModel struct {
	EventID        string `gorm:"type:uuid;primaryKey"`
	EventType      string
	SchemaVersion  int
	TenantID       string
	UserID         string
	PatientID      *string
	SessionID      string `gorm:"type:uuid"`
	StreamSeq      int64
	RequestID      string
	IdempotencyKey string
	CorrelationID  string
	CausationID    *string
	ActorType      string
	ActorID        string
	Payload        json.RawMessage `gorm:"type:jsonb"`
	OccurredAt     time.Time
	RecordedAt     time.Time
}

func (ClinicalEventModel) TableName() string { return "clinical_events" }

func (s *EventStore) Append(ctx context.Context, request eventlog.AppendRequest) (eventlog.AppendResult, error) {
	if replay, ok, err := s.findReplay(ctx, request.Scope, request.SessionID, request.IdempotencyKey); err != nil || ok {
		return eventlog.AppendResult{Event: replay, Revision: replay.StreamSeq, Idempotent: ok}, err
	}

	var appended eventlog.Event
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		appended, err = appendEventTx(tx, request)
		return err
	})
	if err == nil {
		return eventlog.AppendResult{Event: appended, Revision: appended.StreamSeq}, nil
	}
	if !isPostgresUniqueViolation(err) {
		return eventlog.AppendResult{}, err
	}
	replay, ok, replayErr := s.findReplay(ctx, request.Scope, request.SessionID, request.IdempotencyKey)
	if replayErr != nil {
		return eventlog.AppendResult{}, replayErr
	}
	if !ok {
		return eventlog.AppendResult{}, err
	}
	return eventlog.AppendResult{Event: replay, Revision: replay.StreamSeq, Idempotent: true}, nil
}

func (s *EventStore) ReadStream(ctx context.Context, scope eventlog.Scope, sessionID string, afterSeq int64, limit int) ([]eventlog.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if err := verifyEventStreamScope(ctx, s.db, scope, sessionID); err != nil {
		return nil, err
	}
	var rows []ClinicalEventModel
	if err := s.db.WithContext(ctx).
		Where("session_id = ? AND stream_seq > ?", sessionID, afterSeq).
		Order("stream_seq ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]eventlog.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapClinicalEvent(row))
	}
	return out, nil
}

func (s *EventStore) CurrentSequence(ctx context.Context, scope eventlog.Scope, sessionID string) (int64, error) {
	var stream EventStreamModel
	result := s.db.WithContext(ctx).
		Where("session_id = ? AND tenant_id = ? AND user_id = ?", sessionID, scope.Tenant(), scope.UserID).
		First(&stream)
	if result.Error != nil {
		return 0, result.Error
	}
	if !samePatient(stream.PatientID, scope.PatientID) {
		return 0, eventlog.ErrScopeMismatch
	}
	return stream.NextSeq - 1, nil
}

func (s *EventStore) findReplay(ctx context.Context, scope eventlog.Scope, sessionID string, idempotencyKey string) (eventlog.Event, bool, error) {
	if idempotencyKey == "" {
		return eventlog.Event{}, false, nil
	}
	var row ClinicalEventModel
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND idempotency_key = ?", scope.Tenant(), idempotencyKey).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return eventlog.Event{}, false, nil
	}
	if err != nil {
		return eventlog.Event{}, false, err
	}
	if row.UserID != scope.UserID || (sessionID != "" && row.SessionID != sessionID) || !samePatient(row.PatientID, scope.PatientID) {
		return eventlog.Event{}, false, eventlog.ErrScopeMismatch
	}
	return mapClinicalEvent(row), true, nil
}

func ensureEventStreamTx(tx *gorm.DB, scope eventlog.Scope, sessionID string) error {
	return tx.Create(&EventStreamModel{
		SessionID: sessionID,
		TenantID:  scope.Tenant(),
		UserID:    scope.UserID,
		PatientID: scope.PatientID,
		NextSeq:   1,
	}).Error
}

func appendEventTx(tx *gorm.DB, request eventlog.AppendRequest) (eventlog.Event, error) {
	if request.SessionID == "" || request.Scope.UserID == "" || request.EventType == "" || request.IdempotencyKey == "" {
		return eventlog.Event{}, eventlog.ErrInvalidAppend
	}
	if request.SchemaVersion != 1 {
		return eventlog.Event{}, eventlog.ErrUnsupportedSchema
	}
	payload, err := json.Marshal(request.Payload)
	if err != nil {
		return eventlog.Event{}, fmt.Errorf("marshal event payload: %w", err)
	}
	if request.OccurredAt.IsZero() {
		request.OccurredAt = time.Now().UTC()
	}
	if request.ActorType == "" {
		request.ActorType = "system"
	}
	if request.ActorID == "" {
		request.ActorID = request.Scope.UserID
	}

	var sequence struct{ StreamSeq int64 }
	result := tx.Raw(`
		UPDATE event_streams
		SET next_seq = next_seq + 1, updated_at = now()
		WHERE session_id = ? AND tenant_id = ? AND user_id = ?
		  AND patient_id IS NOT DISTINCT FROM ?
		RETURNING next_seq - 1 AS stream_seq`, request.SessionID, request.Scope.Tenant(), request.Scope.UserID, request.Scope.PatientID).Scan(&sequence)
	if result.Error != nil {
		return eventlog.Event{}, result.Error
	}
	if result.RowsAffected == 0 {
		return eventlog.Event{}, gorm.ErrRecordNotFound
	}

	row := ClinicalEventModel{
		EventID:        uuid.NewString(),
		EventType:      request.EventType,
		SchemaVersion:  request.SchemaVersion,
		TenantID:       request.Scope.Tenant(),
		UserID:         request.Scope.UserID,
		PatientID:      request.Scope.PatientID,
		SessionID:      request.SessionID,
		StreamSeq:      sequence.StreamSeq,
		RequestID:      request.RequestID,
		IdempotencyKey: request.IdempotencyKey,
		CorrelationID:  request.CorrelationID,
		CausationID:    request.CausationID,
		ActorType:      request.ActorType,
		ActorID:        request.ActorID,
		Payload:        payload,
		OccurredAt:     request.OccurredAt,
		RecordedAt:     time.Now().UTC(),
	}
	if err := tx.Create(&row).Error; err != nil {
		return eventlog.Event{}, err
	}
	return mapClinicalEvent(row), nil
}

func verifyEventStreamScope(ctx context.Context, db *gorm.DB, scope eventlog.Scope, sessionID string) error {
	var stream EventStreamModel
	if err := db.WithContext(ctx).Where("session_id = ?", sessionID).First(&stream).Error; err != nil {
		return err
	}
	if stream.TenantID != scope.Tenant() || stream.UserID != scope.UserID || !samePatient(stream.PatientID, scope.PatientID) {
		return eventlog.ErrScopeMismatch
	}
	return nil
}

func mapClinicalEvent(row ClinicalEventModel) eventlog.Event {
	return eventlog.Event{
		EventID:        row.EventID,
		EventType:      row.EventType,
		SchemaVersion:  row.SchemaVersion,
		TenantID:       row.TenantID,
		UserID:         row.UserID,
		PatientID:      row.PatientID,
		SessionID:      row.SessionID,
		StreamSeq:      row.StreamSeq,
		RequestID:      row.RequestID,
		IdempotencyKey: row.IdempotencyKey,
		CorrelationID:  row.CorrelationID,
		CausationID:    row.CausationID,
		ActorType:      row.ActorType,
		ActorID:        row.ActorID,
		Payload:        row.Payload,
		OccurredAt:     row.OccurredAt,
		RecordedAt:     row.RecordedAt,
	}
}

func samePatient(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func isPostgresUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
