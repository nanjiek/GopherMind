package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

var (
	ErrInvalidTaskDAG  = errors.New("runtime task DAG is invalid")
	ErrTaskDAGCycle    = errors.New("runtime task DAG contains a dependency cycle")
	ErrTaskDAGConflict = errors.New("runtime task DAG conflicts")
	ErrTaskDAGNotFound = errors.New("runtime task DAG is not found")
)

// TaskStatus is deliberately limited to the initial dependency-derived state
// in this contract. Claiming, completing, retrying, and timing out a task are
// later Task Board and lease contracts.
type TaskStatus string

const (
	TaskReady   TaskStatus = "ready"
	TaskBlocked TaskStatus = "blocked"
)

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
}

// TaskDAG is the trusted, scope-bound static task graph for one existing Run.
// Scope and RunID belong to the graph rather than individual tasks so a batch
// cannot mix trusted identity boundaries.
type TaskDAG struct {
	RunID string
	Scope Metadata
	Tasks []AgentTask
}

// TaskDAGStore is the durable boundary for a fully declared DAG. It does not
// dispatch work, deliver Mailbox messages, or mutate task state.
type TaskDAGStore interface {
	Create(context.Context, TaskDAG) (TaskDAG, error)
	Load(context.Context, Metadata, string) (TaskDAG, error)
}

// NewTaskDAG validates static task identity and dependencies, derives each
// initial state, and returns a defensively copied graph. A root is ready; any
// task with dependencies is blocked until a later task-state contract exists.
func NewTaskDAG(dag TaskDAG) (TaskDAG, error) {
	if dag.RunID == "" || dag.Scope.TenantID == "" || dag.Scope.UserID == "" || len(dag.Tasks) == 0 {
		return TaskDAG{}, fmt.Errorf("%w: run ID, trusted tenant/user scope, and tasks are required", ErrInvalidTaskDAG)
	}

	taskByID := make(map[string]AgentTask, len(dag.Tasks))
	idempotencyKeys := make(map[string]struct{}, len(dag.Tasks))
	for _, task := range dag.Tasks {
		if task.TaskID == "" || task.Type == "" || task.OwnerAgentID == "" || task.IdempotencyKey == "" || task.Revision != 1 {
			return TaskDAG{}, fmt.Errorf("%w: task ID, type, owner, idempotency key, and revision 1 are required", ErrInvalidTaskDAG)
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
		expected := TaskReady
		if len(result.Tasks[index].BlockedBy) != 0 {
			expected = TaskBlocked
		}
		if result.Tasks[index].Status != "" && result.Tasks[index].Status != expected {
			return TaskDAG{}, fmt.Errorf("%w: task %q status must be dependency-derived", ErrInvalidTaskDAG, result.Tasks[index].TaskID)
		}
		result.Tasks[index].Status = expected
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
	}
	return result
}
