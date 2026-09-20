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
	"gophermind/internal/core/service"
)

var ErrCommittedResponseConflict = errors.New("committed response already exists")

type CommittedResponseStore struct{ db *gorm.DB }

var _ service.ReviewedResponseCommitter = (*CommittedResponseStore)(nil)
var _ service.CommittedResponseReader = (*CommittedResponseStore)(nil)

func NewCommittedResponseStore(db *gorm.DB) *CommittedResponseStore {
	return &CommittedResponseStore{db: db}
}

type committedResponseRow struct {
	RunID        string          `gorm:"column:run_id;primaryKey"`
	GenerationID string          `gorm:"column:generation_id"`
	Data         json.RawMessage `gorm:"column:response_data;type:jsonb"`
	EventSeq     int64           `gorm:"column:event_seq"`
	Revision     int64           `gorm:"column:revision"`
	CommittedAt  time.Time       `gorm:"column:committed_at"`
}

func (committedResponseRow) TableName() string { return "agent_committed_responses" }

// CommitReviewedResponse is idempotency-protected by run ID. The caller must
// already be the ResponseCommitBarrier, which reauthorizes immediately before
// this durable effect; this store independently rechecks scope and JSON shape.
func (s *CommittedResponseStore) CommitReviewedResponse(ctx context.Context, response service.CommittedTeamResponse) error {
	if s == nil || s.db == nil {
		return errors.New("committed response store is nil")
	}
	if err := validateCommittedResponse(response); err != nil {
		return err
	}
	var count int64
	if err := checkpointScopeQuery(s.db.WithContext(ctx).Model(&workflowRunRow{}), response.Scope, response.RunID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return runtime.ErrTaskDAGNotFound
	}
	row := committedResponseRow{RunID: response.RunID, GenerationID: response.RunID, Data: append(json.RawMessage(nil), response.Data...), EventSeq: 1, Revision: 1, CommittedAt: time.Now().UTC()}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isPostgresUniqueViolation(err) {
			return ErrCommittedResponseConflict
		}
		return err
	}
	return nil
}

// LoadCommittedResponse returns the sole committed replay event for a run.
// Later streaming work may split this event into persisted chunks, but replay
// never calls a Team worker or an external Tool again.
func (s *CommittedResponseStore) LoadCommittedResponse(ctx context.Context, scope runtime.Metadata, runID string) (service.CommittedTeamResponse, int64, error) {
	if s == nil || s.db == nil {
		return service.CommittedTeamResponse{}, 0, errors.New("committed response store is nil")
	}
	if err := validateCommittedScope(scope, runID); err != nil {
		return service.CommittedTeamResponse{}, 0, err
	}
	var row committedResponseRow
	result := s.db.WithContext(ctx).Raw(`SELECT response.* FROM agent_committed_responses AS response JOIN agent_runs AS run ON run.id = response.run_id WHERE response.run_id = ? AND run.tenant_id = ? AND run.user_id = ? AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?`, runID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID)).Scan(&row)
	if result.Error != nil {
		return service.CommittedTeamResponse{}, 0, result.Error
	}
	if result.RowsAffected != 1 {
		return service.CommittedTeamResponse{}, 0, runtime.ErrTaskDAGNotFound
	}
	return service.CommittedTeamResponse{RunID: row.RunID, Scope: scope, Data: append(json.RawMessage(nil), row.Data...)}, row.EventSeq, nil
}

// PostgresSafetyReviewVerifier proves that the fixed Team's durable Safety
// task succeeded with the explicit approved schema; a caller cannot forge it.
type PostgresSafetyReviewVerifier struct{ Tasks runtime.TaskDAGStore }

func (v PostgresSafetyReviewVerifier) VerifyReviewedResponse(ctx context.Context, response service.ReviewedTeamResponse) error {
	if v.Tasks == nil {
		return errors.New("safety task store is nil")
	}
	dag, err := v.Tasks.Load(ctx, response.Scope, response.RunID)
	if err != nil {
		return err
	}
	for _, task := range dag.Tasks {
		if task.Type != "safety" || task.Status != runtime.TaskSucceeded {
			continue
		}
		var proof struct {
			Approved bool `json:"approved"`
		}
		if json.Unmarshal(task.Output, &proof) == nil && proof.Approved {
			return nil
		}
	}
	return fmt.Errorf("%w: durable approved safety task is required", service.ErrResponseCommitBlocked)
}

func validateCommittedResponse(response service.CommittedTeamResponse) error {
	return validateCommittedScope(response.Scope, response.RunID)
}
func validateCommittedScope(scope runtime.Metadata, runID string) error {
	if scope.TenantID == "" || scope.UserID == "" {
		return fmt.Errorf("invalid committed response scope")
	}
	if _, err := uuid.Parse(runID); err != nil {
		return fmt.Errorf("invalid committed response run ID: %w", err)
	}
	return nil
}
