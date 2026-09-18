package runtime

import (
	"context"
	"errors"
	"fmt"
)

const (
	fixedWorkflowLifecycleError  = "fixed_workflow_lifecycle"
	fixedWorkflowNodeError       = "fixed_workflow_node"
	fixedWorkflowCompletionError = "fixed_workflow_completion"
)

// FixedWorkflowRunner is the in-memory execution protocol for the closed P4
// Step 1 graph. It creates one Run, moves it through the existing lifecycle,
// and commits the validated response as its sole return_result action.
//
// It does not persist a checkpoint, publish a response, retry work, recover a
// connection, or execute an external effect. Nodes that later need an external
// effect must use their existing executor boundary and re-authorize Capability
// immediately before the effect.
type FixedWorkflowRunner struct {
	Workflow *FixedWorkflow
	Hooks    *Pipeline
}

// Run executes the full in-memory lifecycle for one fixed workflow request.
// Once a Run has been created, every failure is recorded on it before the
// original error is returned so callers can inspect a stable terminal state.
func (r *FixedWorkflowRunner) Run(ctx context.Context, spec RunSpec, input FixedWorkflowInput) (*Run, FixedWorkflowResult, error) {
	if r == nil || r.Workflow == nil {
		return nil, FixedWorkflowResult{}, fmt.Errorf("%w: runner needs a workflow", ErrInvalidFixedWorkflow)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := NewRun(spec)
	if err != nil {
		return nil, FixedWorkflowResult{}, err
	}
	controller := &Controller{Run: run, Hooks: r.Hooks}
	for _, status := range []RunStatus{RunLoadingContext, RunRouting, RunRunning} {
		if err := controller.Transition(ctx, status); err != nil {
			r.fail(run, fixedWorkflowLifecycleError, err)
			return run, FixedWorkflowResult{}, err
		}
	}

	result, err := r.Workflow.Execute(ctx, run, input)
	if err != nil {
		r.fail(run, fixedWorkflowNodeError, err)
		return run, FixedWorkflowResult{}, err
	}
	if err := controller.SubmitAction(ctx, Action{
		ActionID:       spec.RunID + ":response",
		RunID:          spec.RunID,
		Step:           run.Snapshot().StepCount + 1,
		Type:           ActionReturnResult,
		Arguments:      cloneJSON(result.Response.Data),
		IdempotencyKey: spec.RunID + ":response",
	}); err != nil {
		r.fail(run, fixedWorkflowCompletionError, err)
		return run, FixedWorkflowResult{}, err
	}
	if err := controller.Complete(ctx); err != nil {
		r.fail(run, fixedWorkflowCompletionError, err)
		return run, FixedWorkflowResult{}, err
	}
	return run, result, nil
}

func (r *FixedWorkflowRunner) fail(run *Run, errorCode string, err error) {
	if run == nil {
		return
	}
	_ = run.FailWith(fixedWorkflowFailureKind(err), errorCode)
}

func fixedWorkflowFailureKind(err error) FailureKind {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout
	case errors.Is(err, context.Canceled):
		return FailureContext
	case errors.Is(err, ErrInvalidFixedWorkflow), errors.Is(err, ErrInvalidWorkflowData), errors.Is(err, ErrInvalidAction), errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrMaxStepsExceeded):
		return FailureValidation
	default:
		return FailurePermanent
	}
}
