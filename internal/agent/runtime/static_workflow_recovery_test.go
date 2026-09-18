package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestStaticWorkflowRecoveryRunnerCheckpointsEachPureNode(t *testing.T) {
	store := &memoryCheckpointStore{}
	runner := &StaticWorkflowRecoveryRunner{Workflow: passingFixedWorkflow(t), Checkpoints: store}
	spec := fixedWorkflowRunSpec("e19de5b7-c382-4ef7-ae3c-3fa69b33260a")
	spec.Scope = Metadata{TenantID: "tenant", UserID: "user"}
	run, result, err := runner.Start(context.Background(), spec, FixedWorkflowInput{Payload: []byte(`{"question":"q"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if run.Snapshot().Status != RunCompleted || string(result.Response.Data) != `{"answer":"ok"}` {
		t.Fatalf("result = %#v, snapshot = %#v", result, run.Snapshot())
	}
	checkpoint, err := store.Load(context.Background(), spec.Scope, spec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Status != RunCompleted || checkpoint.Revision != 7 || checkpoint.CurrentNode != string(WorkflowNodeResponse) {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
}

func TestStaticWorkflowRecoveryRunnerResumesAtNextCompletedNode(t *testing.T) {
	store := &memoryCheckpointStore{}
	var calls []string
	workflow := newFixedWorkflow(t,
		func(context.Context, FixedWorkflowInput) (IntakeOutput, error) {
			calls = append(calls, "intake")
			return IntakeOutput{Data: []byte(`{}`)}, nil
		},
		func(context.Context, RiskRoutingInput, IntakeOutput) (RiskRoutingOutput, error) {
			calls = append(calls, "route")
			return RiskRoutingOutput{WorkflowID: "fixed", WorkflowVersion: "v1", ModelType: "fast", Thinking: "off"}, nil
		},
		func(context.Context, IntakeOutput, RiskRoutingOutput) (EvidenceOutput, error) {
			calls = append(calls, "evidence")
			return EvidenceOutput{Data: []byte(`{"evidence":true}`)}, nil
		},
		func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput) (SafetyOutput, error) {
			calls = append(calls, "safety")
			return SafetyOutput{Data: []byte(`{"safe":true}`)}, nil
		},
		func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput, SafetyOutput) (ResponseOutput, error) {
			calls = append(calls, "response")
			return ResponseOutput{Data: []byte(`{"answer":"ok"}`)}, nil
		},
	)
	runner := &StaticWorkflowRecoveryRunner{Workflow: workflow, Checkpoints: store}
	spec := fixedWorkflowRunSpec("98cb85cc-998c-4a34-9280-6f8cc67c80cb")
	spec.Scope = Metadata{TenantID: "tenant", UserID: "user"}
	state := StaticWorkflowState{Version: staticWorkflowStateVersion, Input: FixedWorkflowInput{Payload: []byte(`{}`)}, Next: WorkflowNodeEvidence, Intake: ptrIntake(IntakeOutput{Data: []byte(`{}`)}), Route: &RiskRoutingOutput{WorkflowID: "fixed", WorkflowVersion: "v1", ModelType: "fast", Thinking: "off"}}
	encoded, err := encodeStaticWorkflowState(state)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Create(context.Background(), Checkpoint{RunID: spec.RunID, Scope: spec.Scope, WorkflowID: spec.WorkflowID, WorkflowVersion: spec.WorkflowVersion, Status: RunRunning, Revision: 1, State: encoded})
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := runner.Resume(context.Background(), spec.Scope, spec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0] != "evidence" || calls[1] != "safety" || calls[2] != "response" {
		t.Fatalf("calls = %#v", calls)
	}
	if string(result.Response.Data) != `{"answer":"ok"}` {
		t.Fatalf("response = %s", result.Response.Data)
	}
}

func TestStaticWorkflowRecoveryRunnerRecordsFailure(t *testing.T) {
	store := &memoryCheckpointStore{}
	fail := errors.New("evidence failed")
	workflow := newFixedWorkflow(t, passIntake, passRisk, func(context.Context, IntakeOutput, RiskRoutingOutput) (EvidenceOutput, error) {
		return EvidenceOutput{}, fail
	}, passSafety, passResponse)
	runner := &StaticWorkflowRecoveryRunner{Workflow: workflow, Checkpoints: store}
	spec := fixedWorkflowRunSpec("b1a1c8a3-ef16-4e3f-a02d-e7c5f5eb1c1a")
	spec.Scope = Metadata{TenantID: "tenant", UserID: "user"}
	_, _, err := runner.Start(context.Background(), spec, FixedWorkflowInput{Payload: []byte(`{}`)})
	if !errors.Is(err, fail) {
		t.Fatalf("error = %v", err)
	}
	checkpoint, err := store.Load(context.Background(), spec.Scope, spec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Status != RunFailed || checkpoint.ErrorCode != "static_workflow_recovery" {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
}

type memoryCheckpointStore struct{ checkpoint Checkpoint }

func (s *memoryCheckpointStore) Create(_ context.Context, value Checkpoint) (Checkpoint, error) {
	if s.checkpoint.RunID != "" {
		return Checkpoint{}, ErrCheckpointConflict
	}
	if err := ValidateCheckpoint(value); err != nil {
		return Checkpoint{}, err
	}
	s.checkpoint = cloneCheckpoint(value)
	return cloneCheckpoint(value), nil
}
func (s *memoryCheckpointStore) Save(_ context.Context, expected int64, value Checkpoint) (Checkpoint, error) {
	if s.checkpoint.RunID != value.RunID || s.checkpoint.Revision != expected {
		return Checkpoint{}, ErrCheckpointConflict
	}
	if err := ValidateCheckpoint(value); err != nil {
		return Checkpoint{}, err
	}
	s.checkpoint = cloneCheckpoint(value)
	return cloneCheckpoint(value), nil
}
func (s *memoryCheckpointStore) Load(_ context.Context, scope Metadata, runID string) (Checkpoint, error) {
	if s.checkpoint.RunID != runID || s.checkpoint.Scope.TenantID != scope.TenantID || s.checkpoint.Scope.UserID != scope.UserID {
		return Checkpoint{}, ErrCheckpointNotFound
	}
	return cloneCheckpoint(s.checkpoint), nil
}
