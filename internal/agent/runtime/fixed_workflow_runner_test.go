package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFixedWorkflowRunnerCompletesLifecycle(t *testing.T) {
	runner := &FixedWorkflowRunner{Workflow: passingFixedWorkflow(t)}
	run, result, err := runner.Run(context.Background(), fixedWorkflowRunSpec("complete"), FixedWorkflowInput{Payload: []byte(`{"question":"q"}`)})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if string(result.Response.Data) != `{"answer":"ok"}` {
		t.Fatalf("response = %s", result.Response.Data)
	}
	snapshot := run.Snapshot()
	if snapshot.Status != RunCompleted || snapshot.CurrentNode != string(WorkflowNodeResponse) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if len(snapshot.Actions) != 1 {
		t.Fatalf("actions = %#v", snapshot.Actions)
	}
	action := snapshot.Actions[0]
	if action.Type != ActionReturnResult || action.ActionID != "complete:response" || action.Step != 1 || string(action.Arguments) != `{"answer":"ok"}` {
		t.Fatalf("action = %#v", action)
	}
}

func TestFixedWorkflowRunnerRecordsNodeFailure(t *testing.T) {
	nodeErr := errors.New("evidence unavailable")
	workflow := newFixedWorkflow(t, passIntake, passRisk,
		func(context.Context, IntakeOutput, RiskRoutingOutput) (EvidenceOutput, error) {
			return EvidenceOutput{}, nodeErr
		},
		passSafety, passResponse,
	)
	runner := &FixedWorkflowRunner{Workflow: workflow}
	run, _, err := runner.Run(context.Background(), fixedWorkflowRunSpec("node-failure"), FixedWorkflowInput{Payload: []byte(`{}`)})
	if !errors.Is(err, nodeErr) {
		t.Fatalf("Run() error = %v, want wrapped node error", err)
	}
	snapshot := run.Snapshot()
	if snapshot.Status != RunFailed || snapshot.CurrentNode != string(WorkflowNodeEvidence) || snapshot.ErrorCode != fixedWorkflowNodeError || snapshot.FailureKind != FailurePermanent || len(snapshot.Actions) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestFixedWorkflowRunnerClassifiesContextCancellation(t *testing.T) {
	workflow := newFixedWorkflow(t,
		func(ctx context.Context, _ FixedWorkflowInput) (IntakeOutput, error) {
			return IntakeOutput{}, ctx.Err()
		},
		passRisk, passEvidence, passSafety, passResponse,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, _, err := (&FixedWorkflowRunner{Workflow: workflow}).Run(ctx, fixedWorkflowRunSpec("cancelled"), FixedWorkflowInput{Payload: []byte(`{}`)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	snapshot := run.Snapshot()
	if snapshot.Status != RunFailed || snapshot.ErrorCode != fixedWorkflowNodeError || snapshot.FailureKind != FailureContext {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestFixedWorkflowRunnerClassifiesDeadline(t *testing.T) {
	workflow := newFixedWorkflow(t,
		func(ctx context.Context, _ FixedWorkflowInput) (IntakeOutput, error) {
			return IntakeOutput{}, ctx.Err()
		},
		passRisk, passEvidence, passSafety, passResponse,
	)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	run, _, err := (&FixedWorkflowRunner{Workflow: workflow}).Run(ctx, fixedWorkflowRunSpec("timed-out"), FixedWorkflowInput{Payload: []byte(`{}`)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v", err)
	}
	snapshot := run.Snapshot()
	if snapshot.Status != RunFailed || snapshot.ErrorCode != fixedWorkflowNodeError || snapshot.FailureKind != FailureTimeout {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestFixedWorkflowRunnerUsesControllerHooks(t *testing.T) {
	var stages []HookStage
	pipeline := &Pipeline{}
	if err := pipeline.Register(nil, HookFunc{BeforeFunc: func(_ context.Context, event HookEvent) error {
		stages = append(stages, event.Stage)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runner := &FixedWorkflowRunner{Workflow: passingFixedWorkflow(t), Hooks: pipeline}
	if _, _, err := runner.Run(context.Background(), fixedWorkflowRunSpec("hooks"), FixedWorkflowInput{Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	want := []HookStage{HookBeforeTransition, HookBeforeTransition, HookBeforeTransition, HookBeforeAction, HookBeforeTransition}
	if len(stages) != len(want) {
		t.Fatalf("stages = %#v, want %#v", stages, want)
	}
	for index := range want {
		if stages[index] != want[index] {
			t.Fatalf("stages = %#v, want %#v", stages, want)
		}
	}
}

func TestFixedWorkflowRunnerRejectsMissingWorkflowAndInvalidSpec(t *testing.T) {
	if run, _, err := (*FixedWorkflowRunner)(nil).Run(context.Background(), fixedWorkflowRunSpec("nil"), FixedWorkflowInput{Payload: []byte(`{}`)}); !errors.Is(err, ErrInvalidFixedWorkflow) || run != nil {
		t.Fatalf("nil runner = %v, %v", run, err)
	}
	runner := &FixedWorkflowRunner{Workflow: passingFixedWorkflow(t)}
	badSpec := fixedWorkflowRunSpec("bad")
	badSpec.MaxSteps = 0
	if run, _, err := runner.Run(context.Background(), badSpec, FixedWorkflowInput{Payload: []byte(`{}`)}); !errors.Is(err, ErrInvalidRun) || run != nil {
		t.Fatalf("invalid spec = %v, %v", run, err)
	}
}

func passingFixedWorkflow(t *testing.T) *FixedWorkflow {
	t.Helper()
	return newFixedWorkflow(t, passIntake, passRisk, passEvidence, passSafety,
		func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput, SafetyOutput) (ResponseOutput, error) {
			return ResponseOutput{Data: []byte(`{"answer":"ok"}`)}, nil
		},
	)
}

func fixedWorkflowRunSpec(runID string) RunSpec {
	return RunSpec{RunID: runID, WorkflowID: "fixed-workflow", WorkflowVersion: "v1", MaxSteps: 1}
}
