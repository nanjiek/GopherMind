package gateway

import (
	"context"
	"fmt"

	"gophermind/internal/agent/runtime"
)

// FixedWorkflowRiskRoutingNode adapts the existing deterministic Router to the
// runtime fixed-workflow contract. It performs no model call or external
// connection; Router remains the sole validator of the bounded risk/task data.
func FixedWorkflowRiskRoutingNode(router interface {
	Route(Request) (Decision, error)
}) (runtime.RiskRoutingNode, error) {
	if router == nil {
		return nil, fmt.Errorf("%w: gateway router is required", runtime.ErrInvalidFixedWorkflow)
	}
	return func(_ context.Context, input runtime.RiskRoutingInput, _ runtime.IntakeOutput) (runtime.RiskRoutingOutput, error) {
		decision, err := router.Route(Request{
			Risk:            RiskLevel(input.Risk),
			Task:            TaskKind(input.Task),
			WorkflowVersion: input.WorkflowVersion,
		})
		if err != nil {
			return runtime.RiskRoutingOutput{}, err
		}
		output := runtime.RiskRoutingOutput{
			WorkflowID:      string(decision.Workflow),
			WorkflowVersion: decision.WorkflowVersion,
			RequiresHuman:   decision.RequiresHuman,
		}
		if decision.Model != nil {
			output.ModelType = decision.Model.ModelType
			output.Thinking = string(decision.Model.Thinking)
		}
		return output, nil
	}, nil
}
