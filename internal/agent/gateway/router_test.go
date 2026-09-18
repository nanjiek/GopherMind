package gateway

import (
	"errors"
	"testing"
)

func TestRouterRoutesRiskAndTaskDeterministically(t *testing.T) {
	router := newTestRouter(t)
	tests := []struct {
		name          string
		request       Request
		workflow      WorkflowID
		model         string
		requiresHuman bool
	}{
		{"low risk qa", Request{Risk: RiskL0, Task: TaskGeneralQA}, WorkflowSingleAgent, "qwen", false},
		{"complex", Request{Risk: RiskL1, Task: TaskComplexReview}, WorkflowClinicalReview, "qwen-pro", false},
		{"high risk", Request{Risk: RiskL2, Task: TaskGeneralQA}, WorkflowClinicalReview, "qwen-pro", false},
		{"red flag", Request{Risk: RiskL3, Task: TaskRedFlag}, WorkflowManualEscalation, "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := router.Route(test.request)
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if decision.Workflow != test.workflow || decision.RequiresHuman != test.requiresHuman {
				t.Fatalf("decision = %#v", decision)
			}
			if test.model == "" {
				if decision.Model != nil {
					t.Fatalf("model = %#v, want nil", decision.Model)
				}
				return
			}
			if decision.Model == nil || decision.Model.ModelType != test.model {
				t.Fatalf("model = %#v, want %q", decision.Model, test.model)
			}
		})
	}
}

func TestRouterDefaultsWorkflowVersionAndRejectsInvalidInput(t *testing.T) {
	router := newTestRouter(t)
	decision, err := router.Route(Request{Risk: RiskL1, Task: TaskSummary})
	if err != nil || decision.WorkflowVersion != "v1" {
		t.Fatalf("Route() = %#v, %v", decision, err)
	}
	_, err = router.Route(Request{Risk: "unknown", Task: TaskSummary})
	if !errors.Is(err, ErrInvalidRouteRequest) {
		t.Fatalf("Route() error = %v, want ErrInvalidRouteRequest", err)
	}
}

func TestNewRouterRejectsUnconfiguredRoute(t *testing.T) {
	_, err := NewRouter(ModelRoute{}, ModelRoute{ModelType: "qwen-pro", Thinking: ThinkingHigh})
	if !errors.Is(err, ErrInvalidModelRoute) {
		t.Fatalf("NewRouter() error = %v, want ErrInvalidModelRoute", err)
	}
}

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	router, err := NewRouter(
		ModelRoute{ModelType: "qwen", Thinking: ThinkingOff},
		ModelRoute{ModelType: "qwen-pro", Thinking: ThinkingHigh},
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}
