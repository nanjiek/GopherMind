package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gophermind/internal/session/compaction"
)

var ErrCompactionConflict = errors.New("session compaction revision conflicts")

// CompactionStore is the PostgreSQL authority for versioned summaries. Redis
// is intentionally not involved: a cache loss must not lose the watermark.
type CompactionStore struct{ db *gorm.DB }

func NewCompactionStore(db *gorm.DB) *CompactionStore { return &CompactionStore{db: db} }

type sessionCompactionRow struct {
	TenantID             string          `gorm:"column:tenant_id;primaryKey"`
	UserID               string          `gorm:"column:user_id;primaryKey"`
	SessionID            string          `gorm:"column:session_id;primaryKey"`
	SourceSeq            int64           `gorm:"column:source_seq"`
	Summary              json.RawMessage `gorm:"column:summary;type:jsonb"`
	InputTokens          int             `gorm:"column:input_tokens"`
	ReservedOutputTokens int             `gorm:"column:reserved_output_tokens"`
	Revision             int64           `gorm:"column:revision"`
	CreatedAt            time.Time       `gorm:"column:created_at"`
	UpdatedAt            time.Time       `gorm:"column:updated_at"`
}

func (sessionCompactionRow) TableName() string { return "session_compactions" }

func (s *CompactionStore) Load(ctx context.Context, scope compaction.Scope) (compaction.Record, bool, error) {
	if s == nil || s.db == nil {
		return compaction.Record{}, false, errors.New("compaction store is nil")
	}
	if scope.TenantID == "" || scope.UserID == "" || scope.SessionID == "" {
		return compaction.Record{}, false, compaction.ErrInvalidRecord
	}
	var row sessionCompactionRow
	result := s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND session_id = ?", scope.TenantID, scope.UserID, scope.SessionID).First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return compaction.Record{}, false, nil
	}
	if result.Error != nil {
		return compaction.Record{}, false, result.Error
	}
	return compactionRecord(row), true, nil
}

// Publish commits a summary only if it was made from a still-valid stream
// watermark and the caller owns the immediately prior summary revision. New
// events are allowed after SourceSeq; readers retain them after this summary.
func (s *CompactionStore) Publish(ctx context.Context, record compaction.Record, expectedRevision int64) (compaction.Record, error) {
	if s == nil || s.db == nil {
		return compaction.Record{}, errors.New("compaction store is nil")
	}
	if err := compaction.ValidateRecord(record); err != nil {
		return compaction.Record{}, err
	}
	if expectedRevision < 0 || record.Revision != expectedRevision+1 {
		return compaction.Record{}, ErrCompactionConflict
	}
	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current struct{ SourceSeq int64 }
		result := tx.Raw(`SELECT next_seq - 1 AS source_seq FROM event_streams WHERE session_id = ? AND tenant_id = ? AND user_id = ? FOR UPDATE`, record.Scope.SessionID, record.Scope.TenantID, record.Scope.UserID).Scan(&current)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || record.SourceSeq > current.SourceSeq {
			return fmt.Errorf("%w: source watermark is not committed", ErrCompactionConflict)
		}
		var existing sessionCompactionRow
		find := tx.Where("tenant_id = ? AND user_id = ? AND session_id = ?", record.Scope.TenantID, record.Scope.UserID, record.Scope.SessionID).First(&existing)
		if errors.Is(find.Error, gorm.ErrRecordNotFound) {
			if expectedRevision != 0 {
				return ErrCompactionConflict
			}
			return tx.Create(&sessionCompactionRow{TenantID: record.Scope.TenantID, UserID: record.Scope.UserID, SessionID: record.Scope.SessionID, SourceSeq: record.SourceSeq, Summary: append(json.RawMessage(nil), record.Summary...), InputTokens: record.InputTokens, ReservedOutputTokens: record.ReservedOutputTokens, Revision: record.Revision, CreatedAt: now, UpdatedAt: now}).Error
		}
		if find.Error != nil || existing.Revision != expectedRevision || record.SourceSeq <= existing.SourceSeq {
			return ErrCompactionConflict
		}
		result = tx.Model(&sessionCompactionRow{}).Where("tenant_id = ? AND user_id = ? AND session_id = ? AND revision = ?", record.Scope.TenantID, record.Scope.UserID, record.Scope.SessionID, expectedRevision).Updates(map[string]any{"source_seq": record.SourceSeq, "summary": append(json.RawMessage(nil), record.Summary...), "input_tokens": record.InputTokens, "reserved_output_tokens": record.ReservedOutputTokens, "revision": record.Revision, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCompactionConflict
		}
		return nil
	})
	if err != nil {
		return compaction.Record{}, err
	}
	return record, nil
}

func compactionRecord(row sessionCompactionRow) compaction.Record {
	return compaction.Record{Scope: compaction.Scope{TenantID: row.TenantID, UserID: row.UserID, SessionID: row.SessionID}, SourceSeq: row.SourceSeq, Summary: append(json.RawMessage(nil), row.Summary...), InputTokens: row.InputTokens, ReservedOutputTokens: row.ReservedOutputTokens, Revision: row.Revision}
}
