package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"gophermind/internal/agent/runtime"
)

// TaskDAGStore persists only a validated, static task graph. It intentionally
// has no claim, completion, retry, lease, fencing, or Mailbox operation.
type TaskDAGStore struct {
	db *gorm.DB
}

var _ runtime.TaskDAGStore = (*TaskDAGStore)(nil)

func NewTaskDAGStore(db *gorm.DB) *TaskDAGStore { return &TaskDAGStore{db: db} }

type agentTaskRow struct {
	ID             string    `gorm:"column:id;primaryKey"`
	RunID          string    `gorm:"column:run_id"`
	Type           string    `gorm:"column:task_type"`
	OwnerAgentID   string    `gorm:"column:owner_agent_id"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	Status         string    `gorm:"column:status"`
	Revision       int64     `gorm:"column:revision"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (agentTaskRow) TableName() string { return "agent_tasks" }

type agentTaskDependencyRow struct {
	TaskID          string `gorm:"column:task_id;primaryKey"`
	BlockedByTaskID string `gorm:"column:blocked_by_task_id;primaryKey"`
}

func (agentTaskDependencyRow) TableName() string { return "agent_task_dependencies" }

// Create validates the entire graph before beginning its transaction. It then
// verifies the Run belongs to the exact trusted scope and writes tasks plus
// edges atomically. A duplicate task or idempotency key is a conflict, not an
// implicit resume.
func (s *TaskDAGStore) Create(ctx context.Context, dag runtime.TaskDAG) (runtime.TaskDAG, error) {
	if s == nil || s.db == nil {
		return runtime.TaskDAG{}, errors.New("task DAG store is nil")
	}
	validated, err := runtime.NewTaskDAG(dag)
	if err != nil {
		return runtime.TaskDAG{}, err
	}
	if err := validateTaskDAGIDs(validated); err != nil {
		return runtime.TaskDAG{}, err
	}
	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var matchingRun int64
		if err := checkpointScopeQuery(tx.Model(&workflowRunRow{}), validated.Scope, validated.RunID).Count(&matchingRun).Error; err != nil {
			return err
		}
		if matchingRun != 1 {
			return runtime.ErrTaskDAGNotFound
		}
		for _, task := range validated.Tasks {
			row := agentTaskRow{
				ID: task.TaskID, RunID: validated.RunID, Type: task.Type, OwnerAgentID: task.OwnerAgentID,
				IdempotencyKey: task.IdempotencyKey, Status: string(task.Status), Revision: task.Revision,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		for _, task := range validated.Tasks {
			for _, dependencyID := range task.BlockedBy {
				if err := tx.Create(&agentTaskDependencyRow{TaskID: task.TaskID, BlockedByTaskID: dependencyID}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, runtime.ErrTaskDAGNotFound) {
			return runtime.TaskDAG{}, err
		}
		if isPostgresUniqueViolation(err) {
			return runtime.TaskDAG{}, fmt.Errorf("%w: task ID or run idempotency key already exists", runtime.ErrTaskDAGConflict)
		}
		return runtime.TaskDAG{}, err
	}
	return validated, nil
}

// Load returns the static graph only when its parent Run is in the exact
// trusted scope. The database remains the authority; no cache or queue is
// consulted.
func (s *TaskDAGStore) Load(ctx context.Context, scope runtime.Metadata, runID string) (runtime.TaskDAG, error) {
	if s == nil || s.db == nil {
		return runtime.TaskDAG{}, errors.New("task DAG store is nil")
	}
	if err := validateTaskDAGScopeIDs(scope, runID); err != nil {
		return runtime.TaskDAG{}, err
	}
	var matchingRun int64
	if err := checkpointScopeQuery(s.db.WithContext(ctx).Model(&workflowRunRow{}), scope, runID).Count(&matchingRun).Error; err != nil {
		return runtime.TaskDAG{}, err
	}
	if matchingRun != 1 {
		return runtime.TaskDAG{}, runtime.ErrTaskDAGNotFound
	}
	var rows []agentTaskRow
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&rows).Error; err != nil {
		return runtime.TaskDAG{}, err
	}
	if len(rows) == 0 {
		return runtime.TaskDAG{}, runtime.ErrTaskDAGNotFound
	}
	dependencies, err := loadTaskDependencies(ctx, s.db, rows)
	if err != nil {
		return runtime.TaskDAG{}, err
	}
	dag := runtime.TaskDAG{RunID: runID, Scope: scope, Tasks: make([]runtime.AgentTask, 0, len(rows))}
	for _, row := range rows {
		dag.Tasks = append(dag.Tasks, runtime.AgentTask{
			TaskID: row.ID, Type: row.Type, OwnerAgentID: row.OwnerAgentID, IdempotencyKey: row.IdempotencyKey,
			Status: runtime.TaskStatus(row.Status), Revision: row.Revision, BlockedBy: dependencies[row.ID],
		})
	}
	validated, err := runtime.NewTaskDAG(dag)
	if err != nil {
		return runtime.TaskDAG{}, fmt.Errorf("stored task DAG is invalid: %w", err)
	}
	return validated, nil
}

func loadTaskDependencies(ctx context.Context, db *gorm.DB, tasks []agentTaskRow) (map[string][]string, error) {
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	var rows []agentTaskDependencyRow
	if err := db.WithContext(ctx).Where("task_id IN ?", taskIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	dependencies := make(map[string][]string, len(tasks))
	for _, row := range rows {
		dependencies[row.TaskID] = append(dependencies[row.TaskID], row.BlockedByTaskID)
	}
	for _, values := range dependencies {
		sort.Strings(values)
	}
	return dependencies, nil
}

func validateTaskDAGIDs(dag runtime.TaskDAG) error {
	if err := validateTaskDAGScopeIDs(dag.Scope, dag.RunID); err != nil {
		return err
	}
	for _, task := range dag.Tasks {
		if _, err := uuid.Parse(task.TaskID); err != nil {
			return fmt.Errorf("%w: task ID must be a UUID: %v", runtime.ErrInvalidTaskDAG, err)
		}
	}
	return nil
}

func validateTaskDAGScopeIDs(scope runtime.Metadata, runID string) error {
	if scope.TenantID == "" || scope.UserID == "" || runID == "" {
		return fmt.Errorf("%w: trusted tenant/user scope and run ID are required", runtime.ErrInvalidTaskDAG)
	}
	if _, err := uuid.Parse(runID); err != nil {
		return fmt.Errorf("%w: run ID must be a UUID: %v", runtime.ErrInvalidTaskDAG, err)
	}
	if scope.SessionID != "" {
		if _, err := uuid.Parse(scope.SessionID); err != nil {
			return fmt.Errorf("%w: session ID must be a UUID: %v", runtime.ErrInvalidTaskDAG, err)
		}
	}
	return nil
}
