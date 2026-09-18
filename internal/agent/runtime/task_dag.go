package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidTaskDAG  = errors.New("runtime task DAG is invalid")
	ErrTaskDAGCycle    = errors.New("runtime task DAG contains a dependency cycle")
	ErrTaskDAGConflict = errors.New("runtime task DAG conflicts")
	ErrTaskDAGNotFound = errors.New("runtime task DAG is not found")
	ErrTaskNotReady    = errors.New("runtime task is not ready")
	ErrTaskLease       = errors.New("runtime task lease conflicts")
)

// TaskStatus is the persisted Task Board lifecycle. Only a successful Task
// releases dependents; Mailbox delivery is deliberately not a Task transition.
type TaskStatus string

const (
	TaskReady     TaskStatus = "ready"
	TaskBlocked   TaskStatus = "blocked"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

func (status TaskStatus) valid() bool {
	switch status {
	case TaskReady, TaskBlocked, TaskRunning, TaskSucceeded, TaskFailed, TaskCancelled:
		return true
	default:
		return false
	}
}

// AgentTask is the durable identity and static dependency declaration for one
// unit of future Agent work. BlockedBy contains task IDs in the same DAG; it
// does not send a message or start an Agent.
type AgentTask struct {
	TaskID         string
	Type           string
	OwnerAgentID   string
	IdempotencyKey string
	BlockedBy      []string
	Status         TaskStatus
	Revision       int64
	Deadline       time.Time
	Input          json.RawMessage
	Output         json.RawMessage
}

// TaskDAG is the trusted, scope-bound static task graph for one existing Run.
// Scope and RunID belong to the graph rather than individual tasks so a batch
// cannot mix trusted identity boundaries.
type TaskDAG struct {
	RunID string
	Scope Metadata
	Tasks []AgentTask
}

// TaskDAGStore is the durable boundary for a fully declared DAG.
type TaskDAGStore interface {
	Create(context.Context, TaskDAG) (TaskDAG, error)
	Load(context.Context, Metadata, string) (TaskDAG, error)
}

// TaskBoard advances Task state through revision CAS and a fenced lease. It
// never invokes an Agent or publishes a message; callers must perform effects
// through their separately authorized executor boundary.
type TaskBoard interface {
	Claim(context.Context, Metadata, string, int64, string, time.Duration) (TaskLease, error)
	Complete(context.Context, Metadata, string, int64, int64, TaskCompletion) (AgentTask, error)
	RecoverExpired(context.Context, Metadata, string, time.Time) (int, error)
	ExpireBlocked(context.Context, Metadata, string, time.Time) (int, error)
}

// TaskCompletion is the bounded committed outcome of one leased Task. A
// successful Task must include a structured output; non-successful tasks carry
// an error code and never publish an output as a result.
type TaskCompletion struct {
	Status    TaskStatus
	ErrorCode string
	Output    json.RawMessage
}

// TaskLease proves a particular worker owns one unexpired Task attempt. The
// fencing token is monotonic for that Task and must accompany completion.
type TaskLease struct {
	Task         AgentTask
	WorkerID     string
	FencingToken int64
	ExpiresAt    time.Time
}

// NewTaskDAG validates static task identity and dependencies, derives each
// initial state, and returns a defensively copied graph. A root is ready; any
// task with dependencies is blocked until a later task-state contract exists.
func NewTaskDAG(dag TaskDAG) (TaskDAG, error) {
	result, err := validateTaskDAG(dag, true)
	if err != nil {
		return TaskDAG{}, err
	}
	for index := range result.Tasks {
		expected := TaskReady
		if len(result.Tasks[index].BlockedBy) != 0 {
			expected = TaskBlocked
		}
		if result.Tasks[index].Status != "" && result.Tasks[index].Status != expected {
			return TaskDAG{}, fmt.Errorf("%w: task %q status must be dependency-derived", ErrInvalidTaskDAG, result.Tasks[index].TaskID)
		}
		result.Tasks[index].Status = expected
	}
	return result, nil
}

// ValidateStoredTaskDAG validates a graph loaded from the durable Task Board.
// Unlike NewTaskDAG, it accepts legal post-creation lifecycle states.
func ValidateStoredTaskDAG(dag TaskDAG) (TaskDAG, error) {
	return validateTaskDAG(dag, false)
}

func validateTaskDAG(dag TaskDAG, initial bool) (TaskDAG, error) {
	if dag.RunID == "" || dag.Scope.TenantID == "" || dag.Scope.UserID == "" || len(dag.Tasks) == 0 {
		return TaskDAG{}, fmt.Errorf("%w: run ID, trusted tenant/user scope, and tasks are required", ErrInvalidTaskDAG)
	}

	taskByID := make(map[string]AgentTask, len(dag.Tasks))
	idempotencyKeys := make(map[string]struct{}, len(dag.Tasks))
	for _, task := range dag.Tasks {
		if task.TaskID == "" || task.Type == "" || task.OwnerAgentID == "" || task.IdempotencyKey == "" || task.Revision < 1 || task.Deadline.IsZero() {
			return TaskDAG{}, fmt.Errorf("%w: task ID, type, owner, idempotency key, deadline, and positive revision are required", ErrInvalidTaskDAG)
		}
		if initial && task.Revision != 1 {
			return TaskDAG{}, fmt.Errorf("%w: first task revision must be 1", ErrInvalidTaskDAG)
		}
		if !initial && !task.Status.valid() {
			return TaskDAG{}, fmt.Errorf("%w: task %q has unknown status", ErrInvalidTaskDAG, task.TaskID)
		}
		if len(task.Input) != 0 && !validTaskData(task.Input) {
			return TaskDAG{}, fmt.Errorf("%w: task %q input must be a JSON object", ErrInvalidTaskDAG, task.TaskID)
		}
		if len(task.Output) != 0 && !validTaskData(task.Output) {
			return TaskDAG{}, fmt.Errorf("%w: task %q output must be a JSON object", ErrInvalidTaskDAG, task.TaskID)
		}
		if _, exists := taskByID[task.TaskID]; exists {
			return TaskDAG{}, fmt.Errorf("%w: duplicate task ID %q", ErrInvalidTaskDAG, task.TaskID)
		}
		if _, exists := idempotencyKeys[task.IdempotencyKey]; exists {
			return TaskDAG{}, fmt.Errorf("%w: duplicate idempotency key %q", ErrInvalidTaskDAG, task.IdempotencyKey)
		}
		taskByID[task.TaskID] = task
		idempotencyKeys[task.IdempotencyKey] = struct{}{}
	}

	for _, task := range dag.Tasks {
		dependencies := make(map[string]struct{}, len(task.BlockedBy))
		for _, dependencyID := range task.BlockedBy {
			if dependencyID == "" || dependencyID == task.TaskID {
				return TaskDAG{}, fmt.Errorf("%w: task %q has an invalid dependency", ErrInvalidTaskDAG, task.TaskID)
			}
			if _, exists := taskByID[dependencyID]; !exists {
				return TaskDAG{}, fmt.Errorf("%w: task %q depends on unknown task %q", ErrInvalidTaskDAG, task.TaskID, dependencyID)
			}
			if _, exists := dependencies[dependencyID]; exists {
				return TaskDAG{}, fmt.Errorf("%w: task %q repeats dependency %q", ErrInvalidTaskDAG, task.TaskID, dependencyID)
			}
			dependencies[dependencyID] = struct{}{}
		}
	}
	if err := validateTaskDAGIsAcyclic(dag.Tasks); err != nil {
		return TaskDAG{}, err
	}

	result := cloneTaskDAG(dag)
	for index := range result.Tasks {
		sort.Strings(result.Tasks[index].BlockedBy)
	}
	return result, nil
}

func validateTaskDAGIsAcyclic(tasks []AgentTask) error {
	visiting := make(map[string]bool, len(tasks))
	visited := make(map[string]bool, len(tasks))
	byID := make(map[string]AgentTask, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
	}
	var visit func(string) error
	visit = func(taskID string) error {
		if visiting[taskID] {
			return fmt.Errorf("%w: task %q", ErrTaskDAGCycle, taskID)
		}
		if visited[taskID] {
			return nil
		}
		visiting[taskID] = true
		for _, dependencyID := range byID[taskID].BlockedBy {
			if err := visit(dependencyID); err != nil {
				return err
			}
		}
		visiting[taskID] = false
		visited[taskID] = true
		return nil
	}
	for _, task := range tasks {
		if err := visit(task.TaskID); err != nil {
			return err
		}
	}
	return nil
}

func cloneTaskDAG(dag TaskDAG) TaskDAG {
	result := dag
	result.Tasks = make([]AgentTask, len(dag.Tasks))
	for index, task := range dag.Tasks {
		result.Tasks[index] = task
		result.Tasks[index].BlockedBy = append([]string(nil), task.BlockedBy...)
		result.Tasks[index].Input = cloneJSON(task.Input)
		result.Tasks[index].Output = cloneJSON(task.Output)
	}
	return result
}

func validTaskData(value json.RawMessage) bool {
	if !json.Valid(value) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}
