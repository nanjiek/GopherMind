package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrStaticWorkflowRecovery = errors.New("runtime static workflow recovery is invalid")

// StaticWorkflowState is the complete structured handoff state for the fixed
// graph. It is deliberately valid only for pure, in-process nodes: a node with
// an external effect requires the later Durable Action/Task protocol instead.
type StaticWorkflowState struct {
	Version  int                `json:"version"`
	Input    FixedWorkflowInput `json:"input"`
	Next     WorkflowNodeID     `json:"next"`
	Intake   *IntakeOutput      `json:"intake,omitempty"`
	Route    *RiskRoutingOutput `json:"route,omitempty"`
	Evidence *EvidenceOutput    `json:"evidence,omitempty"`
	Safety   *SafetyOutput      `json:"safety,omitempty"`
	Response *ResponseOutput    `json:"response,omitempty"`
}

const staticWorkflowStateVersion = 1

// StaticWorkflowRecoveryRunner executes and resumes only the closed fixed
// graph. A checkpoint is advanced after each completed node using store CAS.
// It must not be used for side-effecting nodes.
type StaticWorkflowRecoveryRunner struct {
	Workflow    *FixedWorkflow
	Checkpoints CheckpointStore
}

func (r *StaticWorkflowRecoveryRunner) Start(ctx context.Context, spec RunSpec, input FixedWorkflowInput) (*Run, FixedWorkflowResult, error) {
	if err := r.valid(); err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	if err := validateWorkflowData("input", input.Payload); err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	state := StaticWorkflowState{Version: staticWorkflowStateVersion, Input: cloneFixedWorkflowInput(input), Next: WorkflowNodeIntake}
	encoded, err := encodeStaticWorkflowState(state)
	if err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	checkpoint, err := r.Checkpoints.Create(ctx, Checkpoint{RunID: spec.RunID, Scope: spec.Scope, WorkflowID: spec.WorkflowID, WorkflowVersion: spec.WorkflowVersion, Status: RunCreated, Revision: 1, State: encoded})
	if err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	return r.execute(ctx, spec, checkpoint, state)
}

func (r *StaticWorkflowRecoveryRunner) Resume(ctx context.Context, scope Metadata, runID string) (*Run, FixedWorkflowResult, error) {
	if err := r.valid(); err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	checkpoint, err := r.Checkpoints.Load(ctx, scope, runID)
	if err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	if checkpoint.Status.terminal() {
		return nil, FixedWorkflowResult{}, fmt.Errorf("%w: terminal checkpoint", ErrStaticWorkflowRecovery)
	}
	state, err := decodeStaticWorkflowState(checkpoint.State)
	if err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	spec := RunSpec{RunID: checkpoint.RunID, Scope: checkpoint.Scope, WorkflowID: checkpoint.WorkflowID, WorkflowVersion: checkpoint.WorkflowVersion, MaxSteps: 1}
	return r.execute(ctx, spec, checkpoint, state)
}

func (r *StaticWorkflowRecoveryRunner) execute(ctx context.Context, spec RunSpec, checkpoint Checkpoint, state StaticWorkflowState) (*Run, FixedWorkflowResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := NewRun(spec)
	if err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	for _, status := range []RunStatus{RunLoadingContext, RunRouting, RunRunning} {
		if err := run.Transition(status); err != nil {
			return run, FixedWorkflowResult{}, err
		}
	}
	if checkpoint.Status != RunRunning {
		checkpoint, err = r.save(ctx, checkpoint, RunRunning, "", state)
		if err != nil {
			return run, FixedWorkflowResult{}, err
		}
	}
	for state.Next != "" {
		if err := r.runNext(ctx, run, &checkpoint, &state); err != nil {
			_ = r.fail(ctx, checkpoint, state, err)
			return run, fixedWorkflowResult(state), err
		}
	}
	return run, fixedWorkflowResult(state), nil
}

func (r *StaticWorkflowRecoveryRunner) runNext(ctx context.Context, run *Run, checkpoint *Checkpoint, state *StaticWorkflowState) error {
	node := state.Next
	if node == WorkflowNodeIntake {
		if err := run.SetCurrentNode(string(node)); err != nil {
			return err
		}
		value, err := r.Workflow.intake(ctx, cloneFixedWorkflowInput(state.Input))
		if err != nil {
			return fmt.Errorf("%s: %w", node, err)
		}
		if err := validateWorkflowData("intake output", value.Data); err != nil {
			return err
		}
		state.Intake = ptrIntake(cloneIntakeOutput(value))
		state.Next = WorkflowNodeRiskRouting
	} else if node == WorkflowNodeRiskRouting {
		if state.Intake == nil {
			return fmt.Errorf("%w: risk routing lacks intake", ErrStaticWorkflowRecovery)
		}
		if err := run.SetCurrentNode(string(node)); err != nil {
			return err
		}
		value, err := r.Workflow.riskRouting(ctx, state.Input.Route, cloneIntakeOutput(*state.Intake))
		if err != nil {
			return fmt.Errorf("%s: %w", node, err)
		}
		if err := validateRiskRoutingOutput(value); err != nil {
			return err
		}
		state.Route = &value
		state.Next = WorkflowNodeEvidence
	} else if node == WorkflowNodeEvidence {
		if state.Intake == nil || state.Route == nil {
			return fmt.Errorf("%w: evidence lacks prior state", ErrStaticWorkflowRecovery)
		}
		if err := run.SetCurrentNode(string(node)); err != nil {
			return err
		}
		value, err := r.Workflow.evidence(ctx, cloneIntakeOutput(*state.Intake), *state.Route)
		if err != nil {
			return fmt.Errorf("%s: %w", node, err)
		}
		if err := validateWorkflowData("evidence output", value.Data); err != nil {
			return err
		}
		state.Evidence = ptrEvidence(cloneEvidenceOutput(value))
		state.Next = WorkflowNodeSafety
	} else if node == WorkflowNodeSafety {
		if state.Intake == nil || state.Route == nil || state.Evidence == nil {
			return fmt.Errorf("%w: safety lacks prior state", ErrStaticWorkflowRecovery)
		}
		if err := run.SetCurrentNode(string(node)); err != nil {
			return err
		}
		value, err := r.Workflow.safety(ctx, cloneIntakeOutput(*state.Intake), *state.Route, cloneEvidenceOutput(*state.Evidence))
		if err != nil {
			return fmt.Errorf("%s: %w", node, err)
		}
		if err := validateWorkflowData("safety output", value.Data); err != nil {
			return err
		}
		state.Safety = ptrSafety(cloneSafetyOutput(value))
		state.Next = WorkflowNodeResponse
	} else if node == WorkflowNodeResponse {
		if state.Intake == nil || state.Route == nil || state.Evidence == nil || state.Safety == nil {
			return fmt.Errorf("%w: response lacks prior state", ErrStaticWorkflowRecovery)
		}
		if err := run.SetCurrentNode(string(node)); err != nil {
			return err
		}
		value, err := r.Workflow.response(ctx, cloneIntakeOutput(*state.Intake), *state.Route, cloneEvidenceOutput(*state.Evidence), cloneSafetyOutput(*state.Safety))
		if err != nil {
			return fmt.Errorf("%s: %w", node, err)
		}
		if err := validateWorkflowData("response output", value.Data); err != nil {
			return err
		}
		state.Response = ptrResponse(cloneResponseOutput(value))
		if err := run.SubmitAction(Action{ActionID: run.spec.RunID + ":response", RunID: run.spec.RunID, Step: 1, Type: ActionReturnResult, Arguments: cloneJSON(value.Data)}); err != nil {
			return err
		}
		if err := run.Complete(); err != nil {
			return err
		}
		state.Next = ""
	} else {
		return fmt.Errorf("%w: unknown next node %q", ErrStaticWorkflowRecovery, node)
	}
	status := RunRunning
	current := string(state.Next)
	if state.Next == "" {
		status = RunCompleted
		current = string(WorkflowNodeResponse)
	}
	next, err := r.save(ctx, *checkpoint, status, current, *state)
	if err != nil {
		return err
	}
	*checkpoint = next
	return nil
}

func (r *StaticWorkflowRecoveryRunner) save(ctx context.Context, previous Checkpoint, status RunStatus, current string, state StaticWorkflowState) (Checkpoint, error) {
	encoded, err := encodeStaticWorkflowState(state)
	if err != nil {
		return Checkpoint{}, err
	}
	next := previous
	next.Revision++
	next.Status = status
	next.CurrentNode = current
	next.State = encoded
	next.FailureKind = ""
	next.ErrorCode = ""
	return r.Checkpoints.Save(ctx, previous.Revision, next)
}

func (r *StaticWorkflowRecoveryRunner) fail(ctx context.Context, previous Checkpoint, state StaticWorkflowState, failure error) error {
	encoded, err := encodeStaticWorkflowState(state)
	if err != nil {
		return err
	}
	next := previous
	next.Revision++
	next.Status = RunFailed
	next.FailureKind = fixedWorkflowFailureKind(failure)
	next.ErrorCode = "static_workflow_recovery"
	next.State = encoded
	_, err = r.Checkpoints.Save(ctx, previous.Revision, next)
	return err
}

func (r *StaticWorkflowRecoveryRunner) valid() error {
	if r == nil || r.Workflow == nil || r.Checkpoints == nil {
		return fmt.Errorf("%w: workflow and checkpoint store are required", ErrStaticWorkflowRecovery)
	}
	return nil
}

func encodeStaticWorkflowState(state StaticWorkflowState) (json.RawMessage, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if err := validateWorkflowData("checkpoint state", encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}
func decodeStaticWorkflowState(encoded json.RawMessage) (StaticWorkflowState, error) {
	var state StaticWorkflowState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return state, fmt.Errorf("%w: decode state: %v", ErrStaticWorkflowRecovery, err)
	}
	if state.Version != staticWorkflowStateVersion || state.Next == "" {
		return state, fmt.Errorf("%w: unsupported or terminal state", ErrStaticWorkflowRecovery)
	}
	if err := validateWorkflowData("input", state.Input.Payload); err != nil {
		return state, err
	}
	return state, nil
}
func fixedWorkflowResult(state StaticWorkflowState) FixedWorkflowResult {
	var result FixedWorkflowResult
	if state.Intake != nil {
		result.Intake = cloneIntakeOutput(*state.Intake)
	}
	if state.Route != nil {
		result.RiskRouting = *state.Route
	}
	if state.Evidence != nil {
		result.Evidence = cloneEvidenceOutput(*state.Evidence)
	}
	if state.Safety != nil {
		result.Safety = cloneSafetyOutput(*state.Safety)
	}
	if state.Response != nil {
		result.Response = cloneResponseOutput(*state.Response)
	}
	return result
}
func ptrIntake(v IntakeOutput) *IntakeOutput       { return &v }
func ptrEvidence(v EvidenceOutput) *EvidenceOutput { return &v }
func ptrSafety(v SafetyOutput) *SafetyOutput       { return &v }
func ptrResponse(v ResponseOutput) *ResponseOutput { return &v }
