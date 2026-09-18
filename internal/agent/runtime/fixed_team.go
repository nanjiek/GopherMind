package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrFixedTeam = errors.New("runtime fixed multi-agent team is invalid")

const (
	TeamAgentIntake   = "intake-agent"
	TeamAgentTriage   = "triage-agent"
	TeamAgentEvidence = "evidence-agent"
	TeamAgentSafety   = "safety-agent"
	TeamAgentResponse = "response-agent"
)

// TeamAgent is a pure, bounded worker implementation. It receives only the
// Lead-prepared input for its own fixed task; it has no Task Board, Mailbox, or
// unrestricted graph handle. Side-effecting implementations must instead use
// the existing authorized executor boundary.
type TeamAgent func(context.Context, json.RawMessage) (json.RawMessage, error)

type FixedTeam struct {
	Intake   TeamAgent
	Triage   TeamAgent
	Evidence TeamAgent
	Safety   TeamAgent
	Response TeamAgent
}

type FixedTeamSpec struct {
	RunID    string
	Scope    Metadata
	Deadline time.Time
}

// FixedTeamCoordinator is the closed P4 Lead/Orchestrator. It creates the
// fixed member DAG, sends durable ready notices, has each owner claim and
// complete exactly its Task, and returns only the Response worker's output.
// It never permits a worker to add Agents or edges dynamically.
type FixedTeamCoordinator struct {
	Tasks interface {
		TaskDAGStore
		TaskBoard
	}
	Mailbox MailboxStore
	Team    FixedTeam
	Lease   time.Duration
}

func (c *FixedTeamCoordinator) Start(ctx context.Context, spec FixedTeamSpec, request json.RawMessage) (ResponseOutput, error) {
	if err := c.valid(spec); err != nil {
		return ResponseOutput{}, err
	}
	if !validTaskData(request) {
		return ResponseOutput{}, fmt.Errorf("%w: request must be a JSON object", ErrFixedTeam)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dag, err := c.plan(spec, request)
	if err != nil {
		return ResponseOutput{}, err
	}
	if _, err := c.Tasks.Create(ctx, dag); err != nil {
		return ResponseOutput{}, err
	}
	return c.Resume(ctx, spec.Scope, spec.RunID)
}

// Resume reuses the durable graph and committed task outputs. It can be
// called after a process restart; it never recreates a DAG or re-executes a
// succeeded Task.
func (c *FixedTeamCoordinator) Resume(ctx context.Context, scope Metadata, runID string) (ResponseOutput, error) {
	if c == nil || c.Tasks == nil || c.Mailbox == nil || !c.teamValid() {
		return ResponseOutput{}, fmt.Errorf("%w: task board, mailbox, and five fixed Agents are required", ErrFixedTeam)
	}
	if scope.TenantID == "" || scope.UserID == "" || runID == "" {
		return ResponseOutput{}, fmt.Errorf("%w: trusted scope and run ID are required", ErrFixedTeam)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for turns := 0; turns < 32; turns++ {
		dag, err := c.Tasks.Load(ctx, scope, runID)
		if err != nil {
			return ResponseOutput{}, err
		}
		if response, done, err := fixedTeamResponse(dag); err != nil || done {
			return response, err
		}
		if err := c.enqueueReady(ctx, dag); err != nil {
			return ResponseOutput{}, err
		}
		progressed, err := c.workOneRound(ctx, dag)
		if err != nil {
			return ResponseOutput{}, err
		}
		if !progressed {
			return ResponseOutput{}, fmt.Errorf("%w: no durable ready task or delivery", ErrFixedTeam)
		}
	}
	return ResponseOutput{}, fmt.Errorf("%w: fixed team exceeded bounded turns", ErrFixedTeam)
}

func (c *FixedTeamCoordinator) plan(spec FixedTeamSpec, request json.RawMessage) (TaskDAG, error) {
	intakeID, triageID, evidenceID, safetyID, responseID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	return NewTaskDAG(TaskDAG{RunID: spec.RunID, Scope: spec.Scope, Tasks: []AgentTask{
		{TaskID: intakeID, Type: "intake", OwnerAgentID: TeamAgentIntake, IdempotencyKey: spec.RunID + ":intake", Revision: 1, Deadline: spec.Deadline, Input: cloneJSON(request)},
		{TaskID: triageID, Type: "triage", OwnerAgentID: TeamAgentTriage, IdempotencyKey: spec.RunID + ":triage", Revision: 1, Deadline: spec.Deadline, Input: cloneJSON(request)},
		{TaskID: evidenceID, Type: "evidence", OwnerAgentID: TeamAgentEvidence, IdempotencyKey: spec.RunID + ":evidence", Revision: 1, Deadline: spec.Deadline, BlockedBy: []string{intakeID, triageID}},
		{TaskID: safetyID, Type: "safety", OwnerAgentID: TeamAgentSafety, IdempotencyKey: spec.RunID + ":safety", Revision: 1, Deadline: spec.Deadline, BlockedBy: []string{evidenceID}},
		{TaskID: responseID, Type: "response", OwnerAgentID: TeamAgentResponse, IdempotencyKey: spec.RunID + ":response", Revision: 1, Deadline: spec.Deadline, BlockedBy: []string{safetyID}},
	}})
}

func (c *FixedTeamCoordinator) enqueueReady(ctx context.Context, dag TaskDAG) error {
	for _, task := range dag.Tasks {
		if task.Status != TaskReady {
			continue
		}
		_, err := c.Mailbox.Enqueue(ctx, AgentMessage{MessageID: uuid.NewString(), RunID: dag.RunID, Scope: dag.Scope,
			SenderAgentID: "lead-orchestrator", TargetAgentID: task.OwnerAgentID, TaskID: task.TaskID,
			IdempotencyKey: dag.RunID + ":ready:" + task.TaskID, Revision: 1, Payload: json.RawMessage(`{"kind":"task_ready"}`)})
		if err != nil && !errors.Is(err, ErrMailboxConflict) {
			return err
		}
	}
	return nil
}

func (c *FixedTeamCoordinator) workOneRound(ctx context.Context, dag TaskDAG) (bool, error) {
	progressed := false
	for _, agentID := range []string{TeamAgentIntake, TeamAgentTriage, TeamAgentEvidence, TeamAgentSafety, TeamAgentResponse} {
		delivery, err := c.Mailbox.Claim(ctx, dag.Scope, dag.RunID, agentID, "fixed-team-worker:"+agentID, c.leaseDuration())
		if errors.Is(err, ErrMailboxNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		progressed = true
		task, ok := fixedTeamTask(dag, delivery.Message.TaskID)
		if !ok || task.Status != TaskReady || task.OwnerAgentID != agentID {
			if _, err := c.Mailbox.Acknowledge(ctx, dag.Scope, delivery.Message.MessageID, delivery.Message.Revision, delivery.FencingToken); err != nil {
				return false, err
			}
			continue
		}
		lease, err := c.Tasks.Claim(ctx, dag.Scope, task.TaskID, task.Revision, delivery.WorkerID, c.leaseDuration())
		if errors.Is(err, ErrTaskNotReady) || errors.Is(err, ErrTaskDAGConflict) {
			if _, ackErr := c.Mailbox.Acknowledge(ctx, dag.Scope, delivery.Message.MessageID, delivery.Message.Revision, delivery.FencingToken); ackErr != nil {
				return false, ackErr
			}
			continue
		}
		if err != nil {
			return false, err
		}
		input, err := fixedTeamInput(dag, task)
		if err != nil {
			return false, c.completeFailed(ctx, dag.Scope, delivery, lease, "private_input_invalid", err)
		}
		output, err := c.agent(agentID)(ctx, input)
		if err != nil || !validTaskData(output) {
			return false, c.completeFailed(ctx, dag.Scope, delivery, lease, "agent_output_invalid", err)
		}
		if _, err := c.Tasks.Complete(ctx, dag.Scope, task.TaskID, lease.Task.Revision, lease.FencingToken, TaskCompletion{Status: TaskSucceeded, Output: cloneJSON(output)}); err != nil {
			return false, err
		}
		if _, err := c.Mailbox.Acknowledge(ctx, dag.Scope, delivery.Message.MessageID, delivery.Message.Revision, delivery.FencingToken); err != nil {
			return false, err
		}
	}
	return progressed, nil
}

func (c *FixedTeamCoordinator) completeFailed(ctx context.Context, scope Metadata, delivery MailboxDelivery, lease TaskLease, code string, cause error) error {
	if _, err := c.Tasks.Complete(ctx, scope, lease.Task.TaskID, lease.Task.Revision, lease.FencingToken, TaskCompletion{Status: TaskFailed, ErrorCode: code}); err != nil {
		return err
	}
	if _, err := c.Mailbox.Acknowledge(ctx, scope, delivery.Message.MessageID, delivery.Message.Revision, delivery.FencingToken); err != nil {
		return err
	}
	if cause != nil {
		return cause
	}
	return fmt.Errorf("%w: %s", ErrFixedTeam, code)
}

func fixedTeamInput(dag TaskDAG, task AgentTask) (json.RawMessage, error) {
	if task.Type == "intake" || task.Type == "triage" {
		if !validTaskData(task.Input) {
			return nil, fmt.Errorf("%w: root task input missing", ErrFixedTeam)
		}
		return cloneJSON(task.Input), nil
	}
	result := make(map[string]json.RawMessage, len(task.BlockedBy))
	for _, dependencyID := range task.BlockedBy {
		dependency, ok := fixedTeamTask(dag, dependencyID)
		if !ok || dependency.Status != TaskSucceeded || !validTaskData(dependency.Output) {
			return nil, fmt.Errorf("%w: dependency result unavailable", ErrFixedTeam)
		}
		result[dependency.Type] = cloneJSON(dependency.Output)
	}
	return json.Marshal(result)
}

func fixedTeamResponse(dag TaskDAG) (ResponseOutput, bool, error) {
	for _, task := range dag.Tasks {
		if task.Status == TaskFailed || task.Status == TaskCancelled {
			return ResponseOutput{}, true, fmt.Errorf("%w: task %s ended %s", ErrFixedTeam, task.Type, task.Status)
		}
		if task.Type == "response" && task.Status == TaskSucceeded {
			if !validTaskData(task.Output) {
				return ResponseOutput{}, true, fmt.Errorf("%w: response result missing", ErrFixedTeam)
			}
			return ResponseOutput{Data: cloneJSON(task.Output)}, true, nil
		}
	}
	return ResponseOutput{}, false, nil
}

func fixedTeamTask(dag TaskDAG, taskID string) (AgentTask, bool) {
	for _, task := range dag.Tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return AgentTask{}, false
}

func (c *FixedTeamCoordinator) valid(spec FixedTeamSpec) error {
	if c == nil || c.Tasks == nil || c.Mailbox == nil || !c.teamValid() || spec.RunID == "" || spec.Scope.TenantID == "" || spec.Scope.UserID == "" || spec.Deadline.IsZero() {
		return fmt.Errorf("%w: stores, five Agents, run ID, trusted scope, and deadline are required", ErrFixedTeam)
	}
	return nil
}

func (c *FixedTeamCoordinator) teamValid() bool {
	return c.Team.Intake != nil && c.Team.Triage != nil && c.Team.Evidence != nil && c.Team.Safety != nil && c.Team.Response != nil
}

func (c *FixedTeamCoordinator) leaseDuration() time.Duration {
	if c.Lease <= 0 {
		return time.Minute
	}
	return c.Lease
}

func (c *FixedTeamCoordinator) agent(agentID string) TeamAgent {
	switch agentID {
	case TeamAgentIntake:
		return c.Team.Intake
	case TeamAgentTriage:
		return c.Team.Triage
	case TeamAgentEvidence:
		return c.Team.Evidence
	case TeamAgentSafety:
		return c.Team.Safety
	default:
		return c.Team.Response
	}
}
