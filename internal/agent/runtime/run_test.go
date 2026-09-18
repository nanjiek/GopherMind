package runtime

import (
	"errors"
	"testing"
)

func TestRunTransitionsThroughSuccessfulResult(t *testing.T) {
	run := newTestRun(t, 3)
	for _, status := range []RunStatus{RunLoadingContext, RunRouting, RunRunning} {
		if err := run.Transition(status); err != nil {
			t.Fatalf("Transition(%s) error = %v", status, err)
		}
	}
	if err := run.SubmitAction(testAction("action-1", 1, ActionReturnResult)); err != nil {
		t.Fatalf("SubmitAction() error = %v", err)
	}
	if got := run.Snapshot().Status; got != RunValidating {
		t.Fatalf("status after return_result = %s, want %s", got, RunValidating)
	}
	if err := run.Complete(); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got := run.Snapshot().Status; got != RunCompleted {
		t.Fatalf("completed status = %s, want %s", got, RunCompleted)
	}
	if err := run.Transition(RunRunning); !errors.Is(err, ErrTerminalRun) {
		t.Fatalf("transition from terminal error = %v, want ErrTerminalRun", err)
	}
}

func TestRunActionObservationResumesWaitingRun(t *testing.T) {
	run := newRunningRun(t, 2)
	if err := run.SubmitAction(testAction("action-1", 1, ActionCallTool)); err != nil {
		t.Fatalf("SubmitAction() error = %v", err)
	}
	if got := run.Snapshot().Status; got != RunWaitingTool {
		t.Fatalf("status after call_tool = %s, want %s", got, RunWaitingTool)
	}
	observation := Observation{ObservationID: "observation-1", RunID: "run-1", ActionID: "action-1", Step: 1, Output: []byte(`{"answer":"ok"}`)}
	if err := run.Observe(observation); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	snapshot := run.Snapshot()
	if snapshot.Status != RunRunning || snapshot.StepCount != 1 || len(snapshot.Actions) != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot after observation = %#v", snapshot)
	}
}

func TestRunActionsEnterTheirDefinedWaitStates(t *testing.T) {
	testCases := []struct {
		actionType ActionType
		wantStatus RunStatus
		observable bool
	}{
		{ActionAskUser, RunWaitingUser, true},
		{ActionCallSkill, RunWaitingTool, true},
		{ActionCallTool, RunWaitingTool, true},
		{ActionDelegateTask, RunWaitingAgent, true},
		{ActionEscalateHuman, RunWaitingHuman, true},
		{ActionReturnResult, RunValidating, false},
	}
	for _, testCase := range testCases {
		t.Run(string(testCase.actionType), func(t *testing.T) {
			run := newRunningRun(t, 1)
			if err := run.SubmitAction(testAction("action-1", 1, testCase.actionType)); err != nil {
				t.Fatalf("SubmitAction() error = %v", err)
			}
			if got := run.Snapshot().Status; got != testCase.wantStatus {
				t.Fatalf("status = %s, want %s", got, testCase.wantStatus)
			}
			if testCase.observable {
				if err := run.Observe(Observation{ObservationID: "observation-1", RunID: "run-1", ActionID: "action-1", Step: 1, Output: []byte(`{}`)}); err != nil {
					t.Fatalf("Observe() error = %v", err)
				}
				if got := run.Snapshot().Status; got != RunRunning {
					t.Fatalf("status after observation = %s, want %s", got, RunRunning)
				}
				return
			}
			if err := run.Complete(); err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
		})
	}
}

func TestRunRejectsInvalidTransitionAndAction(t *testing.T) {
	run := newTestRun(t, 1)
	if err := run.Transition(RunRunning); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v, want ErrInvalidTransition", err)
	}
	if err := run.SubmitAction(testAction("action-1", 1, ActionCallTool)); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("action before running error = %v, want ErrInvalidAction", err)
	}
	run = newRunningRun(t, 1)
	if err := run.Transition(RunWaitingTool); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("manual waiting transition error = %v, want ErrInvalidTransition", err)
	}
}

func TestRunRejectsObservationForWrongAction(t *testing.T) {
	run := newRunningRun(t, 2)
	if err := run.SubmitAction(testAction("action-1", 1, ActionAskUser)); err != nil {
		t.Fatalf("SubmitAction() error = %v", err)
	}
	err := run.Observe(Observation{ObservationID: "observation-1", RunID: "run-1", ActionID: "wrong", Step: 1, Output: []byte(`{}`)})
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Observe() error = %v, want ErrInvalidObservation", err)
	}
}

func TestRunEnforcesMaximumSteps(t *testing.T) {
	run := newRunningRun(t, 1)
	if err := run.SubmitAction(testAction("action-1", 1, ActionCallTool)); err != nil {
		t.Fatalf("first SubmitAction() error = %v", err)
	}
	if err := run.Observe(Observation{ObservationID: "observation-1", RunID: "run-1", ActionID: "action-1", Step: 1, Output: []byte(`{}`)}); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if err := run.SubmitAction(testAction("action-2", 2, ActionCallTool)); !errors.Is(err, ErrMaxStepsExceeded) {
		t.Fatalf("second SubmitAction() error = %v, want ErrMaxStepsExceeded", err)
	}
}

func TestRunFailureTerminatesPendingWork(t *testing.T) {
	run := newRunningRun(t, 2)
	if err := run.SubmitAction(testAction("action-1", 1, ActionDelegateTask)); err != nil {
		t.Fatalf("SubmitAction() error = %v", err)
	}
	if err := run.Fail("dependency_unavailable"); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if got := run.Snapshot(); got.Status != RunFailed || got.ErrorCode != "dependency_unavailable" {
		t.Fatalf("failed snapshot = %#v", got)
	}
	if err := run.Observe(Observation{ObservationID: "observation-1", RunID: "run-1", ActionID: "action-1", Step: 1}); !errors.Is(err, ErrTerminalRun) {
		t.Fatalf("Observe() after failure error = %v, want ErrTerminalRun", err)
	}
}

func TestRunCASRejectsStaleRevision(t *testing.T) {
	run := newTestRun(t, 2)
	initial := run.Snapshot().Revision
	if err := run.TransitionCAS(initial, RunLoadingContext); err != nil {
		t.Fatalf("TransitionCAS() error = %v", err)
	}
	if err := run.TransitionCAS(initial, RunRouting); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale TransitionCAS() error = %v, want ErrRevisionConflict", err)
	}
	if got := run.Snapshot().Revision; got != initial+1 {
		t.Fatalf("revision = %d, want %d", got, initial+1)
	}
}

func TestRunTerminatesRepeatedNoProgress(t *testing.T) {
	run, err := NewRun(RunSpec{RunID: "run-1", WorkflowID: "triage", WorkflowVersion: "v1", MaxSteps: 3, MaxNoProgress: 1})
	if err != nil {
		t.Fatalf("NewRun() error = %v", err)
	}
	for _, status := range []RunStatus{RunLoadingContext, RunRouting, RunRunning} {
		if err := run.Transition(status); err != nil {
			t.Fatalf("Transition(%s) error = %v", status, err)
		}
	}
	if err := run.SubmitAction(testAction("action-1", 1, ActionCallTool)); err != nil {
		t.Fatalf("first SubmitAction() error = %v", err)
	}
	if err := run.Observe(Observation{ObservationID: "observation-1", RunID: "run-1", ActionID: "action-1", Step: 1, Output: []byte(`{}`)}); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if err := run.SubmitAction(testAction("action-2", 2, ActionCallTool)); !errors.Is(err, ErrNoProgress) {
		t.Fatalf("repeated SubmitAction() error = %v, want ErrNoProgress", err)
	}
	if got := run.Snapshot(); got.Status != RunFailed || got.FailureKind != FailureNoProgress || got.ErrorCode != string(FailureNoProgress) {
		t.Fatalf("no-progress snapshot = %#v", got)
	}
}

func TestRunFailWithStoresClassification(t *testing.T) {
	run := newRunningRun(t, 1)
	if err := run.FailWith(FailureDependency, "rag_unavailable"); err != nil {
		t.Fatalf("FailWith() error = %v", err)
	}
	if got := run.Snapshot(); got.FailureKind != FailureDependency || got.ErrorCode != "rag_unavailable" {
		t.Fatalf("failure snapshot = %#v", got)
	}
}

func newRunningRun(t *testing.T, maxSteps int) *Run {
	t.Helper()
	run := newTestRun(t, maxSteps)
	for _, status := range []RunStatus{RunLoadingContext, RunRouting, RunRunning} {
		if err := run.Transition(status); err != nil {
			t.Fatalf("Transition(%s) error = %v", status, err)
		}
	}
	return run
}

func newTestRun(t *testing.T, maxSteps int) *Run {
	t.Helper()
	run, err := NewRun(RunSpec{RunID: "run-1", WorkflowID: "triage", WorkflowVersion: "v1", MaxSteps: maxSteps})
	if err != nil {
		t.Fatalf("NewRun() error = %v", err)
	}
	return run
}

func testAction(id string, step int, actionType ActionType) Action {
	return Action{ActionID: id, RunID: "run-1", Step: step, Type: actionType, Arguments: []byte(`{}`)}
}
