package gateway

import (
	"context"
	"errors"
	"testing"

	"gophermind/internal/agent/runtime"
)

func TestFixedWorkflowRiskRoutingNodeUsesExistingRouter(t *testing.T) {
	router, err := NewRouter(
		ModelRoute{ModelType: "fast", Thinking: ThinkingOff},
		ModelRoute{ModelType: "advanced", Thinking: ThinkingHigh},
	)
	if err != nil {
		t.Fatal(err)
	}
	node, err := FixedWorkflowRiskRoutingNode(router)
	if err != nil {
		t.Fatal(err)
	}
	route, err := node(context.Background(), runtime.RiskRoutingInput{Risk: "l2", Task: "general_qa"}, runtime.IntakeOutput{Data: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if route.WorkflowID != string(WorkflowClinicalReview) || route.WorkflowVersion != "v1" || route.ModelType != "advanced" || route.Thinking != string(ThinkingHigh) {
		t.Fatalf("route = %#v", route)
	}
	if _, err := FixedWorkflowRiskRoutingNode(nil); !errors.Is(err, runtime.ErrInvalidFixedWorkflow) {
		t.Fatalf("FixedWorkflowRiskRoutingNode(nil) error = %v", err)
	}
}
