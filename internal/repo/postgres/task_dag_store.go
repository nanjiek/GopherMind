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

// TaskDAGStore persists the Task Board and its static dependency graph.
type TaskDAGStore struct {
	db *gorm.DB
}

var _ interface {
	runtime.TaskDAGStore
	runtime.TaskBoard
} = (*TaskDAGStore)(nil)

func NewTaskDAGStore(db *gorm.DB) *TaskDAGStore { return &TaskDAGStore{db: db} }

type agentTaskRow struct {
	ID             string     `gorm:"column:id;primaryKey"`
	RunID          string     `gorm:"column:run_id"`
	Type           string     `gorm:"column:task_type"`
	OwnerAgentID   string     `gorm:"column:owner_agent_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	Status         string     `gorm:"column:status"`
	Revision       int64      `gorm:"column:revision"`
	LeaseOwner     string     `gorm:"column:lease_owner"`
	LeaseEpoch     int64      `gorm:"column:lease_epoch"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at"`
	Attempts       int        `gorm:"column:attempts"`
	ErrorCode      string     `gorm:"column:error_code"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
	Deadline       time.Time  `gorm:"column:deadline_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
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
				IdempotencyKey: task.IdempotencyKey, Status: string(task.Status), Revision: task.Revision, Deadline: task.Deadline,
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

// Claim acquires one ready Task or takes over an expired lease. It checks the
// caller's expected revision and all dependencies, then increments both task
// revision and the monotonic fencing token in one transaction.
func (s *TaskDAGStore) Claim(ctx context.Context, scope runtime.Metadata, taskID string, expectedRevision int64, workerID string, leaseDuration time.Duration) (runtime.TaskLease, error) {
	if s == nil || s.db == nil {
		return runtime.TaskLease{}, errors.New("task DAG store is nil")
	}
	if err := validateTaskClaim(scope, taskID, expectedRevision, workerID, leaseDuration); err != nil {
		return runtime.TaskLease{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(leaseDuration)
	var lease runtime.TaskLease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadScopedTaskForUpdate(tx, scope, taskID)
		if err != nil {
			return err
		}
		if row.Revision != expectedRevision {
			return runtime.ErrTaskDAGConflict
		}
		if row.Status != string(runtime.TaskReady) && !(row.Status == string(runtime.TaskRunning) && row.LeaseExpiresAt != nil && !row.LeaseExpiresAt.After(now)) {
			return runtime.ErrTaskNotReady
		}
		var unfinished int64
		if err := tx.Raw(`SELECT count(*) FROM agent_task_dependencies d
			JOIN agent_tasks dependency ON dependency.id = d.blocked_by_task_id
			WHERE d.task_id = ? AND dependency.status <> ?`, taskID, runtime.TaskSucceeded).Scan(&unfinished).Error; err != nil {
			return err
		}
		if unfinished != 0 {
			return runtime.ErrTaskNotReady
		}
		nextRevision := row.Revision + 1
		nextEpoch := row.LeaseEpoch + 1
		result := tx.Model(&agentTaskRow{}).Where("id = ? AND revision = ?", taskID, expectedRevision).Updates(map[string]any{
			"status": string(runtime.TaskRunning), "revision": nextRevision, "lease_owner": workerID,
			"lease_epoch": nextEpoch, "lease_expires_at": expiresAt, "attempts": row.Attempts + 1, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtime.ErrTaskDAGConflict
		}
		lease = runtime.TaskLease{Task: taskFromRow(row), WorkerID: workerID, FencingToken: nextEpoch, ExpiresAt: expiresAt}
		lease.Task.Status = runtime.TaskRunning
		lease.Task.Revision = nextRevision
		return nil
	})
	if err != nil {
		return runtime.TaskLease{}, err
	}
	return lease, nil
}

// Complete accepts only the current, unexpired fenced lease. Success releases
// direct dependents whose entire dependency set has succeeded; a failed or
// cancelled Task releases nothing.
func (s *TaskDAGStore) Complete(ctx context.Context, scope runtime.Metadata, taskID string, expectedRevision, fencingToken int64, status runtime.TaskStatus, errorCode string) (runtime.AgentTask, error) {
	if s == nil || s.db == nil {
		return runtime.AgentTask{}, errors.New("task DAG store is nil")
	}
	if err := validateTaskCompletion(scope, taskID, expectedRevision, fencingToken, status, errorCode); err != nil {
		return runtime.AgentTask{}, err
	}
	now := time.Now().UTC()
	var completed runtime.AgentTask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadScopedTaskForUpdate(tx, scope, taskID)
		if err != nil {
			return err
		}
		if row.Revision != expectedRevision {
			return runtime.ErrTaskDAGConflict
		}
		if row.Status != string(runtime.TaskRunning) || row.LeaseEpoch != fencingToken || row.LeaseExpiresAt == nil || !row.LeaseExpiresAt.After(now) {
			return runtime.ErrTaskLease
		}
		nextRevision := row.Revision + 1
		result := tx.Model(&agentTaskRow{}).Where("id = ? AND revision = ? AND lease_epoch = ?", taskID, expectedRevision, fencingToken).Updates(map[string]any{
			"status": string(status), "revision": nextRevision, "error_code": errorCode, "completed_at": now,
			"lease_owner": "", "lease_expires_at": nil, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtime.ErrTaskDAGConflict
		}
		if status == runtime.TaskSucceeded {
			if err := releaseSatisfiedDependents(tx, row.RunID, now); err != nil {
				return err
			}
		}
		completed = taskFromRow(row)
		completed.Status = status
		completed.Revision = nextRevision
		return nil
	})
	if err != nil {
		return runtime.AgentTask{}, err
	}
	return completed, nil
}

// RecoverExpired returns abandoned Task leases to ready state. It is safe to
// invoke after process restart because it uses durable expiry and only moves
// currently running, expired records.
func (s *TaskDAGStore) RecoverExpired(ctx context.Context, scope runtime.Metadata, runID string, now time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("task DAG store is nil")
	}
	if err := validateTaskDAGScopeIDs(scope, runID); err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := s.db.WithContext(ctx).Exec(`UPDATE agent_tasks AS task SET
		status = ?, revision = task.revision + 1, lease_owner = '', lease_expires_at = NULL, updated_at = ?
		FROM agent_runs AS run
		WHERE task.run_id = run.id AND task.run_id = ? AND run.tenant_id = ? AND run.user_id = ?
		AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?
		AND task.status = ? AND task.lease_expires_at <= ?`, runtime.TaskReady, now, runID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID), runtime.TaskRunning, now)
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

// ExpireBlocked turns only deadline-expired dependency waits into a durable
// terminal cancellation. It does not change a running Task or release another
// dependent, so a failed prerequisite cannot silently become success.
func (s *TaskDAGStore) ExpireBlocked(ctx context.Context, scope runtime.Metadata, runID string, now time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("task DAG store is nil")
	}
	if err := validateTaskDAGScopeIDs(scope, runID); err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := s.db.WithContext(ctx).Exec(`UPDATE agent_tasks AS task SET
		status = ?, revision = task.revision + 1, error_code = 'dependency_timeout', completed_at = ?, updated_at = ?
		FROM agent_runs AS run
		WHERE task.run_id = run.id AND task.run_id = ? AND run.tenant_id = ? AND run.user_id = ?
		AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?
		AND task.status = ? AND task.deadline_at <= ?`, runtime.TaskCancelled, now, now, runID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID), runtime.TaskBlocked, now)
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
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
			Status: runtime.TaskStatus(row.Status), Revision: row.Revision, Deadline: row.Deadline, BlockedBy: dependencies[row.ID],
		})
	}
	validated, err := runtime.ValidateStoredTaskDAG(dag)
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
	if err := validateTaskScope(scope); err != nil {
		return err
	}
	if runID == "" {
		return fmt.Errorf("%w: run ID is required", runtime.ErrInvalidTaskDAG)
	}
	if _, err := uuid.Parse(runID); err != nil {
		return fmt.Errorf("%w: run ID must be a UUID: %v", runtime.ErrInvalidTaskDAG, err)
	}
	return nil
}

func validateTaskClaim(scope runtime.Metadata, taskID string, expectedRevision int64, workerID string, leaseDuration time.Duration) error {
	if err := validateTaskScope(scope); err != nil {
		return err
	}
	if _, err := uuid.Parse(taskID); err != nil {
		return fmt.Errorf("%w: task ID must be a UUID: %v", runtime.ErrInvalidTaskDAG, err)
	}
	if expectedRevision < 1 || workerID == "" || leaseDuration <= 0 {
		return fmt.Errorf("%w: positive expected revision, worker ID, and lease duration are required", runtime.ErrInvalidTaskDAG)
	}
	return nil
}

func validateTaskScope(scope runtime.Metadata) error {
	if scope.TenantID == "" || scope.UserID == "" {
		return fmt.Errorf("%w: trusted tenant/user scope is required", runtime.ErrInvalidTaskDAG)
	}
	if scope.SessionID != "" {
		if _, err := uuid.Parse(scope.SessionID); err != nil {
			return fmt.Errorf("%w: session ID must be a UUID: %v", runtime.ErrInvalidTaskDAG, err)
		}
	}
	return nil
}

func validateTaskCompletion(scope runtime.Metadata, taskID string, expectedRevision, fencingToken int64, status runtime.TaskStatus, errorCode string) error {
	if err := validateTaskClaim(scope, taskID, expectedRevision, "completion", time.Nanosecond); err != nil {
		return err
	}
	if fencingToken < 1 || (status != runtime.TaskSucceeded && status != runtime.TaskFailed && status != runtime.TaskCancelled) {
		return fmt.Errorf("%w: positive fencing token and terminal task status are required", runtime.ErrInvalidTaskDAG)
	}
	if status == runtime.TaskSucceeded && errorCode != "" {
		return fmt.Errorf("%w: successful task cannot have an error code", runtime.ErrInvalidTaskDAG)
	}
	if status != runtime.TaskSucceeded && errorCode == "" {
		return fmt.Errorf("%w: non-successful task requires an error code", runtime.ErrInvalidTaskDAG)
	}
	return nil
}

func loadScopedTaskForUpdate(tx *gorm.DB, scope runtime.Metadata, taskID string) (agentTaskRow, error) {
	var row agentTaskRow
	result := tx.Raw(`SELECT task.* FROM agent_tasks AS task
		JOIN agent_runs AS run ON run.id = task.run_id
		WHERE task.id = ? AND run.tenant_id = ? AND run.user_id = ?
		AND run.patient_id IS NOT DISTINCT FROM ? AND run.session_id IS NOT DISTINCT FROM ?
		FOR UPDATE`, taskID, scope.TenantID, scope.UserID, optionalString(scope.PatientID), optionalString(scope.SessionID)).Scan(&row)
	if result.Error != nil {
		return agentTaskRow{}, result.Error
	}
	if result.RowsAffected != 1 {
		return agentTaskRow{}, runtime.ErrTaskDAGNotFound
	}
	return row, nil
}

func taskFromRow(row agentTaskRow) runtime.AgentTask {
	return runtime.AgentTask{TaskID: row.ID, Type: row.Type, OwnerAgentID: row.OwnerAgentID, IdempotencyKey: row.IdempotencyKey, Status: runtime.TaskStatus(row.Status), Revision: row.Revision, Deadline: row.Deadline}
}

func releaseSatisfiedDependents(tx *gorm.DB, runID string, now time.Time) error {
	return tx.Exec(`UPDATE agent_tasks AS task SET status = ?, revision = task.revision + 1, updated_at = ?
		WHERE task.run_id = ? AND task.status = ?
		AND EXISTS (SELECT 1 FROM agent_task_dependencies own WHERE own.task_id = task.id)
		AND NOT EXISTS (
			SELECT 1 FROM agent_task_dependencies d
			JOIN agent_tasks dependency ON dependency.id = d.blocked_by_task_id
			WHERE d.task_id = task.id AND dependency.status <> ?
		)`, runtime.TaskReady, now, runID, runtime.TaskBlocked, runtime.TaskSucceeded).Error
}
