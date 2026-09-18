package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFixedWorkflowExecutesClosedGraphInOrder(t *testing.T) {
	var calls []string
	workflow := newFixedWorkflow(t,
		func(_ context.Context, input FixedWorkflowInput) (IntakeOutput, error) {
			calls = append(calls, "intake")
			if string(input.Payload) != `{"question":"q"}` {
				t.Fatalf("intake input = %s", input.Payload)
			}
			return IntakeOutput{Data: []byte(`{"intake":"ok"}`)}, nil
		},
		func(_ context.Context, request RiskRoutingInput, intake IntakeOutput) (RiskRoutingOutput, error) {
			calls = append(calls, "risk")
			if request.Risk != "l1" || string(intake.Data) != `{"intake":"ok"}` {
				t.Fatalf("risk input = %#v, %s", request, intake.Data)
			}
			return RiskRoutingOutput{WorkflowID: "single-agent-query", WorkflowVersion: "v1", ModelType: "fast", Thinking: "off"}, nil
		},
		func(_ context.Context, intake IntakeOutput, route RiskRoutingOutput) (EvidenceOutput, error) {
			calls = append(calls, "evidence")
			if string(intake.Data) != `{"intake":"ok"}` || route.WorkflowID != "single-agent-query" {
				t.Fatalf("evidence inputs = %s, %#v", intake.Data, route)
			}
			return EvidenceOutput{Data: []byte(`{"evidence":"ok"}`)}, nil
		},
		func(_ context.Context, _ IntakeOutput, _ RiskRoutingOutput, evidence EvidenceOutput) (SafetyOutput, error) {
			calls = append(calls, "safety")
			if string(evidence.Data) != `{"evidence":"ok"}` {
				t.Fatalf("safety evidence = %s", evidence.Data)
			}
			return SafetyOutput{Data: []byte(`{"safe":true}`)}, nil
		},
		func(_ context.Context, _ IntakeOutput, _ RiskRoutingOutput, _ EvidenceOutput, safety SafetyOutput) (ResponseOutput, error) {
			calls = append(calls, "response")
			if string(safety.Data) != `{"safe":true}` {
				t.Fatalf("response safety = %s", safety.Data)
			}
			return ResponseOutput{Data: []byte(`{"answer":"ok"}`)}, nil
		},
	)
	run := newRunningFixedWorkflowRun(t)
	result, err := workflow.Execute(context.Background(), run, FixedWorkflowInput{
		Route:   RiskRoutingInput{Risk: "l1", Task: "general_qa"},
		Payload: []byte(`{"question":"q"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := []string{"intake", "risk", "evidence", "safety", "response"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	if string(result.Response.Data) != `{"answer":"ok"}` {
		t.Fatalf("response = %s", result.Response.Data)
	}
	if node := run.Snapshot().CurrentNode; node != string(WorkflowNodeResponse) {
		t.Fatalf("current node = %q, want %q", node, WorkflowNodeResponse)
	}
}

func TestFixedWorkflowStopsAtFailingNode(t *testing.T) {
	stop := errors.New("stop")
	var calls []string
	workflow := newFixedWorkflow(t,
		func(context.Context, FixedWorkflowInput) (IntakeOutput, error) {
			calls = append(calls, "intake")
			return IntakeOutput{Data: []byte(`{}`)}, nil
		},
		func(context.Context, RiskRoutingInput, IntakeOutput) (RiskRoutingOutput, error) {
			calls = append(calls, "risk")
			return RiskRoutingOutput{}, stop
		},
		func(context.Context, IntakeOutput, RiskRoutingOutput) (EvidenceOutput, error) {
			calls = append(calls, "evidence")
			return EvidenceOutput{}, nil
		},
		func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput) (SafetyOutput, error) {
			calls = append(calls, "safety")
			return SafetyOutput{}, nil
		},
		func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput, SafetyOutput) (ResponseOutput, error) {
			calls = append(calls, "response")
			return ResponseOutput{}, nil
		},
	)
	_, err := workflow.Execute(context.Background(), newRunningFixedWorkflowRun(t), FixedWorkflowInput{Payload: []byte(`{}`)})
	if !errors.Is(err, stop) {
		t.Fatalf("Execute() error = %v, want wrapped stop", err)
	}
	if want := []string{"intake", "risk"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestFixedWorkflowRejectsIncompleteAndInvalidExecution(t *testing.T) {
	if _, err := NewFixedWorkflow(nil, nil, nil, nil, nil); !errors.Is(err, ErrInvalidFixedWorkflow) {
		t.Fatalf("NewFixedWorkflow() error = %v", err)
	}
	workflow := newFixedWorkflow(t, passIntake, passRisk, passEvidence, passSafety, passResponse)
	if _, err := workflow.Execute(context.Background(), nil, FixedWorkflowInput{Payload: []byte(`{}`)}); !errors.Is(err, ErrInvalidFixedWorkflow) {
		t.Fatalf("Execute(nil run) error = %v", err)
	}
	idle, err := NewRun(RunSpec{RunID: "idle", WorkflowID: "fixed", WorkflowVersion: "v1", MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Execute(context.Background(), idle, FixedWorkflowInput{Payload: []byte(`{}`)}); !errors.Is(err, ErrInvalidFixedWorkflow) {
		t.Fatalf("Execute(idle run) error = %v", err)
	}
	if _, err := workflow.Execute(context.Background(), newRunningFixedWorkflowRun(t), FixedWorkflowInput{Payload: []byte(`[]`)}); !errors.Is(err, ErrInvalidWorkflowData) {
		t.Fatalf("Execute(array input) error = %v", err)
	}
}

func TestFixedWorkflowRejectsInvalidNodeData(t *testing.T) {
	workflow := newFixedWorkflow(t,
		func(context.Context, FixedWorkflowInput) (IntakeOutput, error) {
			return IntakeOutput{Data: []byte(`not json`)}, nil
		},
		passRisk, passEvidence, passSafety, passResponse,
	)
	_, err := workflow.Execute(context.Background(), newRunningFixedWorkflowRun(t), FixedWorkflowInput{Payload: []byte(`{}`)})
	if !errors.Is(err, ErrInvalidWorkflowData) {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestFixedWorkflowCopiesStructuredDataAtNodeBoundaries(t *testing.T) {
	intakeData := []byte(`{"intake":"original"}`)
	responseData := []byte(`{"answer":"original"}`)
	workflow := newFixedWorkflow(t,
		func(context.Context, FixedWorkflowInput) (IntakeOutput, error) {
			return IntakeOutput{Data: intakeData}, nil
		},
		func(context.Context, RiskRoutingInput, IntakeOutput) (RiskRoutingOutput, error) {
			return RiskRoutingOutput{WorkflowID: "single", WorkflowVersion: "v1", ModelType: "fast", Thinking: "off"}, nil
		},
		func(_ context.Context, intake IntakeOutput, _ RiskRoutingOutput) (EvidenceOutput, error) {
			if string(intake.Data) != `{"intake":"original"}` {
				t.Fatalf("intake handoff = %s", intake.Data)
			}
			return EvidenceOutput{Data: []byte(`{}`)}, nil
		},
		passSafety,
		func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput, SafetyOutput) (ResponseOutput, error) {
			return ResponseOutput{Data: responseData}, nil
		},
	)
	result, err := workflow.Execute(context.Background(), newRunningFixedWorkflowRun(t), FixedWorkflowInput{Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	copy(intakeData, []byte(`{"intake":"changed"} `))
	copy(responseData, []byte(`{"answer":"changed"} `))
	if string(result.Intake.Data) != `{"intake":"original"}` || string(result.Response.Data) != `{"answer":"original"}` {
		t.Fatalf("result retained mutable node data: %#v", result)
	}
}

func newFixedWorkflow(t *testing.T, intake IntakeNode, risk RiskRoutingNode, evidence EvidenceNode, safety SafetyNode, response ResponseNode) *FixedWorkflow {
	t.Helper()
	workflow, err := NewFixedWorkflow(intake, risk, evidence, safety, response)
	if err != nil {
		t.Fatal(err)
	}
	return workflow
}

func newRunningFixedWorkflowRun(t *testing.T) *Run {
	t.Helper()
	run, err := NewRun(RunSpec{RunID: "fixed-run", WorkflowID: "fixed", WorkflowVersion: "v1", MaxSteps: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []RunStatus{RunLoadingContext, RunRouting, RunRunning} {
		if err := run.Transition(status); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

func passIntake(context.Context, FixedWorkflowInput) (IntakeOutput, error) {
	return IntakeOutput{Data: []byte(`{}`)}, nil
}
func passRisk(context.Context, RiskRoutingInput, IntakeOutput) (RiskRoutingOutput, error) {
	return RiskRoutingOutput{WorkflowID: "single", WorkflowVersion: "v1", ModelType: "fast", Thinking: "off"}, nil
}
func passEvidence(context.Context, IntakeOutput, RiskRoutingOutput) (EvidenceOutput, error) {
	return EvidenceOutput{Data: []byte(`{}`)}, nil
}
func passSafety(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput) (SafetyOutput, error) {
	return SafetyOutput{Data: []byte(`{}`)}, nil
}
func passResponse(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput, SafetyOutput) (ResponseOutput, error) {
	return ResponseOutput{Data: []byte(`{}`)}, nil
}
