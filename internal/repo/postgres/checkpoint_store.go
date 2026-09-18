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

// WorkflowCheckpointStore persists the one canonical checkpoint revision for a
// Run and an immutable history of accepted revisions. It is deliberately only
// a checkpoint authority: Task DAG, Mailbox, lease, and queue delivery remain
// separate P4 contracts.
type WorkflowCheckpointStore struct {
	db *gorm.DB
}

var _ runtime.CheckpointStore = (*WorkflowCheckpointStore)(nil)

func NewWorkflowCheckpointStore(db *gorm.DB) *WorkflowCheckpointStore {
	return &WorkflowCheckpointStore{db: db}
}

type workflowRunRow struct {
	RunID           string          `gorm:"column:id;primaryKey"`
	TenantID        string          `gorm:"column:tenant_id"`
	UserID          string          `gorm:"column:user_id"`
	PatientID       *string         `gorm:"column:patient_id"`
	SessionID       *string         `gorm:"column:session_id"`
	RequestID       string          `gorm:"column:request_id"`
	WorkflowID      string          `gorm:"column:workflow_id"`
	WorkflowVersion string          `gorm:"column:workflow_version"`
	Status          string          `gorm:"column:status"`
	CurrentNode     string          `gorm:"column:current_node"`
	Revision        int64           `gorm:"column:revision"`
	FailureKind     string          `gorm:"column:failure_kind"`
	ErrorCode       string          `gorm:"column:error_code"`
	State           json.RawMessage `gorm:"column:checkpoint_state;type:jsonb"`
	CreatedAt       time.Time       `gorm:"column:created_at"`
	UpdatedAt       time.Time       `gorm:"column:updated_at"`
}

func (workflowRunRow) TableName() string { return "agent_runs" }

type workflowCheckpointRow struct {
	RunID       string          `gorm:"column:run_id;primaryKey"`
	Revision    int64           `gorm:"column:revision;primaryKey"`
	Status      string          `gorm:"column:status"`
	CurrentNode string          `gorm:"column:current_node"`
	FailureKind string          `gorm:"column:failure_kind"`
	ErrorCode   string          `gorm:"column:error_code"`
	State       json.RawMessage `gorm:"column:state;type:jsonb"`
	CreatedAt   time.Time       `gorm:"column:created_at"`
}

func (workflowCheckpointRow) TableName() string { return "agent_run_checkpoints" }

// Create stores the first checkpoint (revision 1). A duplicate Run ID is a
// conflict, never an implicit resume, so callers cannot overwrite a different
// request's execution state.
func (s *WorkflowCheckpointStore) Create(ctx context.Context, checkpoint runtime.Checkpoint) (runtime.Checkpoint, error) {
	if s == nil || s.db == nil {
		return runtime.Checkpoint{}, errors.New("workflow checkpoint store is nil")
	}
	if err := runtime.ValidateCheckpoint(checkpoint); err != nil {
		return runtime.Checkpoint{}, err
	}
	if checkpoint.Revision != 1 {
		return runtime.Checkpoint{}, fmt.Errorf("%w: first revision must be 1", runtime.ErrInvalidCheckpoint)
	}
	if err := validateCheckpointIDs(checkpoint.Scope, checkpoint.RunID); err != nil {
		return runtime.Checkpoint{}, err
	}
	now := time.Now().UTC()
	checkpoint = clonePersistentCheckpoint(checkpoint)
	checkpoint.CreatedAt = now
	checkpoint.UpdatedAt = now
	row := checkpointRunRow(checkpoint)
	history := checkpointHistoryRow(checkpoint)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Create(&history).Error
	}); err != nil {
		if isPostgresUniqueViolation(err) {
			return runtime.Checkpoint{}, fmt.Errorf("%w: run already exists", runtime.ErrCheckpointConflict)
		}
		return runtime.Checkpoint{}, err
	}
	return checkpoint, nil
}

// Save accepts exactly the revision following expectedRevision. The canonical
// agent_runs row is updated with a compare-and-swap predicate and the accepted
// value is appended to immutable history in the same transaction.
func (s *WorkflowCheckpointStore) Save(ctx context.Context, expectedRevision int64, checkpoint runtime.Checkpoint) (runtime.Checkpoint, error) {
	if s == nil || s.db == nil {
		return runtime.Checkpoint{}, errors.New("workflow checkpoint store is nil")
	}
	if err := runtime.ValidateCheckpoint(checkpoint); err != nil {
		return runtime.Checkpoint{}, err
	}
	if expectedRevision < 1 || checkpoint.Revision != expectedRevision+1 {
		return runtime.Checkpoint{}, fmt.Errorf("%w: expected revision %d requires next revision", runtime.ErrInvalidCheckpoint, expectedRevision)
	}
	if err := validateCheckpointIDs(checkpoint.Scope, checkpoint.RunID); err != nil {
		return runtime.Checkpoint{}, err
	}
	checkpoint = clonePersistentCheckpoint(checkpoint)
	checkpoint.UpdatedAt = time.Now().UTC()
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = checkpoint.UpdatedAt
	}
	updates := map[string]any{
		"request_id":       checkpoint.Scope.RequestID,
		"workflow_id":      checkpoint.WorkflowID,
		"workflow_version": checkpoint.WorkflowVersion,
		"status":           string(checkpoint.Status),
		"current_node":     checkpoint.CurrentNode,
		"revision":         checkpoint.Revision,
		"failure_kind":     string(checkpoint.FailureKind),
		"error_code":       checkpoint.ErrorCode,
		"checkpoint_state": checkpoint.State,
		"updated_at":       checkpoint.UpdatedAt,
	}
	history := checkpointHistoryRow(checkpoint)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := checkpointScopeQuery(tx.Model(&workflowRunRow{}), checkpoint.Scope, checkpoint.RunID).
			Where("revision = ?", expectedRevision).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtime.ErrCheckpointConflict
		}
		return tx.Create(&history).Error
	})
	if err != nil {
		if errors.Is(err, runtime.ErrCheckpointConflict) || isPostgresUniqueViolation(err) {
			return runtime.Checkpoint{}, fmt.Errorf("%w: stale or duplicate revision", runtime.ErrCheckpointConflict)
		}
		return runtime.Checkpoint{}, err
	}
	return checkpoint, nil
}

// Load reads the canonical current checkpoint only when its trusted tenant,
// user, patient, and session scope match exactly.
func (s *WorkflowCheckpointStore) Load(ctx context.Context, scope runtime.Metadata, runID string) (runtime.Checkpoint, error) {
	if s == nil || s.db == nil {
		return runtime.Checkpoint{}, errors.New("workflow checkpoint store is nil")
	}
	if err := validateCheckpointIDs(scope, runID); err != nil {
		return runtime.Checkpoint{}, err
	}
	var row workflowRunRow
	err := checkpointScopeQuery(s.db.WithContext(ctx), scope, runID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return runtime.Checkpoint{}, runtime.ErrCheckpointNotFound
	}
	if err != nil {
		return runtime.Checkpoint{}, err
	}
	checkpoint := runtime.Checkpoint{
		RunID:           row.RunID,
		Scope:           runtime.Metadata{TenantID: row.TenantID, UserID: row.UserID, PatientID: cloneOptional(row.PatientID), SessionID: cloneOptional(row.SessionID), RequestID: row.RequestID, RunID: row.RunID},
		WorkflowID:      row.WorkflowID,
		WorkflowVersion: row.WorkflowVersion,
		Status:          runtime.RunStatus(row.Status),
		CurrentNode:     row.CurrentNode,
		Revision:        row.Revision,
		State:           append(json.RawMessage(nil), row.State...),
		FailureKind:     runtime.FailureKind(row.FailureKind),
		ErrorCode:       row.ErrorCode,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	if err := runtime.ValidateCheckpoint(checkpoint); err != nil {
		return runtime.Checkpoint{}, fmt.Errorf("stored checkpoint is invalid: %w", err)
	}
	return checkpoint, nil
}

func validateCheckpointIDs(scope runtime.Metadata, runID string) error {
	if scope.TenantID == "" || scope.UserID == "" || runID == "" {
		return fmt.Errorf("%w: trusted tenant/user scope and run ID are required", runtime.ErrInvalidCheckpoint)
	}
	if _, err := uuid.Parse(runID); err != nil {
		return fmt.Errorf("%w: run ID must be a UUID: %v", runtime.ErrInvalidCheckpoint, err)
	}
	if scope.SessionID != "" {
		if _, err := uuid.Parse(scope.SessionID); err != nil {
			return fmt.Errorf("%w: session ID must be a UUID: %v", runtime.ErrInvalidCheckpoint, err)
		}
	}
	return nil
}

func checkpointScopeQuery(db *gorm.DB, scope runtime.Metadata, runID string) *gorm.DB {
	return db.Where(`id = ? AND tenant_id = ? AND user_id = ?
		AND patient_id IS NOT DISTINCT FROM ? AND session_id IS NOT DISTINCT FROM ?`, runID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID))
}

func checkpointRunRow(checkpoint runtime.Checkpoint) workflowRunRow {
	return workflowRunRow{
		RunID: checkpoint.RunID, TenantID: checkpoint.Scope.TenantID, UserID: checkpoint.Scope.UserID,
		PatientID: optionalString(checkpoint.Scope.PatientID), SessionID: optionalString(checkpoint.Scope.SessionID), RequestID: checkpoint.Scope.RequestID,
		WorkflowID: checkpoint.WorkflowID, WorkflowVersion: checkpoint.WorkflowVersion, Status: string(checkpoint.Status), CurrentNode: checkpoint.CurrentNode,
		Revision: checkpoint.Revision, FailureKind: string(checkpoint.FailureKind), ErrorCode: checkpoint.ErrorCode, State: append(json.RawMessage(nil), checkpoint.State...),
		CreatedAt: checkpoint.CreatedAt, UpdatedAt: checkpoint.UpdatedAt,
	}
}

func checkpointHistoryRow(checkpoint runtime.Checkpoint) workflowCheckpointRow {
	return workflowCheckpointRow{RunID: checkpoint.RunID, Revision: checkpoint.Revision, Status: string(checkpoint.Status), CurrentNode: checkpoint.CurrentNode,
		FailureKind: string(checkpoint.FailureKind), ErrorCode: checkpoint.ErrorCode, State: append(json.RawMessage(nil), checkpoint.State...), CreatedAt: checkpoint.UpdatedAt}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func cloneOptional(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func clonePersistentCheckpoint(checkpoint runtime.Checkpoint) runtime.Checkpoint {
	checkpoint.State = append(json.RawMessage(nil), checkpoint.State...)
	return checkpoint
}
