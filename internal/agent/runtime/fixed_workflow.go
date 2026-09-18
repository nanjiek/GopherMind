package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrInvalidFixedWorkflow = errors.New("runtime fixed workflow is invalid")
	ErrInvalidWorkflowData  = errors.New("runtime fixed workflow data is invalid")
)

// WorkflowNodeID is one of the fixed P4 Step 1 workflow nodes. The graph is
// intentionally linear and closed: callers cannot insert, remove, or reorder
// nodes at runtime.
type WorkflowNodeID string

const (
	WorkflowNodeIntake      WorkflowNodeID = "intake"
	WorkflowNodeRiskRouting WorkflowNodeID = "risk_routing"
	WorkflowNodeEvidence    WorkflowNodeID = "evidence"
	WorkflowNodeSafety      WorkflowNodeID = "safety"
	WorkflowNodeResponse    WorkflowNodeID = "response"
)

// FixedWorkflowInput holds the trusted routing request and structured request
// payload. Risk is supplied by an upstream trusted policy or deterministic
// triage rule; a workflow node must not let a model select it.
type FixedWorkflowInput struct {
	Route   RiskRoutingInput
	Payload json.RawMessage
}

// RiskRoutingInput is the trusted, bounded routing input used by the fixed
// graph. gateway.FixedWorkflowRiskRoutingNode maps it to the existing Gateway
// route contract without introducing a runtime -> gateway dependency.
type RiskRoutingInput struct {
	Risk            string
	Task            string
	WorkflowVersion string
}

// IntakeOutput is the structured data accepted by the risk-routing node.
type IntakeOutput struct {
	Data json.RawMessage
}

// RiskRoutingOutput is the Gateway decision accepted by the evidence node.
type RiskRoutingOutput struct {
	WorkflowID      string
	WorkflowVersion string
	ModelType       string
	Thinking        string
	RequiresHuman   bool
}

// EvidenceOutput is the structured evidence data accepted by the safety node.
type EvidenceOutput struct {
	Data json.RawMessage
}

// SafetyOutput is the structured safety result accepted by the response node.
type SafetyOutput struct {
	Data json.RawMessage
}

// ResponseOutput is the structured, final workflow result. Publishing it is a
// separate boundary and is deliberately not performed by FixedWorkflow.
type ResponseOutput struct {
	Data json.RawMessage
}

// FixedWorkflowResult retains each typed node result for its caller. This is
// in-memory execution state only; it is not a Task DAG, mailbox, or checkpoint.
type FixedWorkflowResult struct {
	Intake      IntakeOutput
	RiskRouting RiskRoutingOutput
	Evidence    EvidenceOutput
	Safety      SafetyOutput
	Response    ResponseOutput
}

// IntakeNode, RiskRoutingNode, EvidenceNode, SafetyNode, and ResponseNode are
// deliberately distinct function types. Their signatures make the only legal
// data handoff Intake -> Risk routing -> Evidence -> Safety -> Response.
type IntakeNode func(context.Context, FixedWorkflowInput) (IntakeOutput, error)
type RiskRoutingNode func(context.Context, RiskRoutingInput, IntakeOutput) (RiskRoutingOutput, error)
type EvidenceNode func(context.Context, IntakeOutput, RiskRoutingOutput) (EvidenceOutput, error)
type SafetyNode func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput) (SafetyOutput, error)
type ResponseNode func(context.Context, IntakeOutput, RiskRoutingOutput, EvidenceOutput, SafetyOutput) (ResponseOutput, error)

// FixedWorkflow is the closed, in-process P4 Step 1 graph. It has no dynamic
// delegation, durable state, queue semantics, connection recovery, or storage.
type FixedWorkflow struct {
	intake      IntakeNode
	riskRouting RiskRoutingNode
	evidence    EvidenceNode
	safety      SafetyNode
	response    ResponseNode
}

// NewFixedWorkflow requires every fixed node up front. There is intentionally
// no registration API because the topology is a P4 Step 1 contract.
func NewFixedWorkflow(intake IntakeNode, riskRouting RiskRoutingNode, evidence EvidenceNode, safety SafetyNode, response ResponseNode) (*FixedWorkflow, error) {
	if intake == nil || riskRouting == nil || evidence == nil || safety == nil || response == nil {
		return nil, fmt.Errorf("%w: every fixed node is required", ErrInvalidFixedWorkflow)
	}
	return &FixedWorkflow{
		intake:      intake,
		riskRouting: riskRouting,
		evidence:    evidence,
		safety:      safety,
		response:    response,
	}, nil
}

// Execute runs exactly the fixed graph in order. It records the node about to
// execute on the existing in-memory Run but makes no lifecycle transition and
// performs no external effect. Future side-effecting node implementations must
// use their executor boundary, which re-authorizes Capability immediately
// before the effect.
func (w *FixedWorkflow) Execute(ctx context.Context, run *Run, input FixedWorkflowInput) (FixedWorkflowResult, error) {
	if w == nil || run == nil {
		return FixedWorkflowResult{}, fmt.Errorf("%w: workflow and run are required", ErrInvalidFixedWorkflow)
	}
	if run.Snapshot().Status != RunRunning {
		return FixedWorkflowResult{}, fmt.Errorf("%w: run must be %s", ErrInvalidFixedWorkflow, RunRunning)
	}
	if err := validateWorkflowData("input", input.Payload); err != nil {
		return FixedWorkflowResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result := FixedWorkflowResult{}
	if err := run.SetCurrentNode(string(WorkflowNodeIntake)); err != nil {
		return FixedWorkflowResult{}, err
	}
	intake, err := w.intake(ctx, cloneFixedWorkflowInput(input))
	if err != nil {
		return FixedWorkflowResult{}, fmt.Errorf("%s: %w", WorkflowNodeIntake, err)
	}
	if err := validateWorkflowData("intake output", intake.Data); err != nil {
		return FixedWorkflowResult{}, err
	}
	result.Intake = cloneIntakeOutput(intake)

	if err := run.SetCurrentNode(string(WorkflowNodeRiskRouting)); err != nil {
		return FixedWorkflowResult{}, err
	}
	route, err := w.riskRouting(ctx, input.Route, cloneIntakeOutput(intake))
	if err != nil {
		return FixedWorkflowResult{}, fmt.Errorf("%s: %w", WorkflowNodeRiskRouting, err)
	}
	if err := validateRiskRoutingOutput(route); err != nil {
		return FixedWorkflowResult{}, err
	}
	result.RiskRouting = route

	if err := run.SetCurrentNode(string(WorkflowNodeEvidence)); err != nil {
		return FixedWorkflowResult{}, err
	}
	evidence, err := w.evidence(ctx, cloneIntakeOutput(intake), route)
	if err != nil {
		return FixedWorkflowResult{}, fmt.Errorf("%s: %w", WorkflowNodeEvidence, err)
	}
	if err := validateWorkflowData("evidence output", evidence.Data); err != nil {
		return FixedWorkflowResult{}, err
	}
	result.Evidence = cloneEvidenceOutput(evidence)

	if err := run.SetCurrentNode(string(WorkflowNodeSafety)); err != nil {
		return FixedWorkflowResult{}, err
	}
	safety, err := w.safety(ctx, cloneIntakeOutput(intake), route, cloneEvidenceOutput(evidence))
	if err != nil {
		return FixedWorkflowResult{}, fmt.Errorf("%s: %w", WorkflowNodeSafety, err)
	}
	if err := validateWorkflowData("safety output", safety.Data); err != nil {
		return FixedWorkflowResult{}, err
	}
	result.Safety = cloneSafetyOutput(safety)

	if err := run.SetCurrentNode(string(WorkflowNodeResponse)); err != nil {
		return FixedWorkflowResult{}, err
	}
	response, err := w.response(ctx, cloneIntakeOutput(intake), route, cloneEvidenceOutput(evidence), cloneSafetyOutput(safety))
	if err != nil {
		return FixedWorkflowResult{}, fmt.Errorf("%s: %w", WorkflowNodeResponse, err)
	}
	if err := validateWorkflowData("response output", response.Data); err != nil {
		return FixedWorkflowResult{}, err
	}
	result.Response = cloneResponseOutput(response)
	return result, nil
}

func validateWorkflowData(name string, data json.RawMessage) error {
	if len(data) == 0 || !json.Valid(data) {
		return fmt.Errorf("%w: %s must be valid JSON", ErrInvalidWorkflowData, name)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return fmt.Errorf("%w: %s must be a JSON object", ErrInvalidWorkflowData, name)
	}
	return nil
}

func validateRiskRoutingOutput(output RiskRoutingOutput) error {
	if output.WorkflowID == "" || output.WorkflowVersion == "" {
		return fmt.Errorf("%w: routing output needs workflow ID and version", ErrInvalidWorkflowData)
	}
	if output.RequiresHuman {
		if output.ModelType != "" || output.Thinking != "" {
			return fmt.Errorf("%w: human route cannot select a model", ErrInvalidWorkflowData)
		}
		return nil
	}
	if output.ModelType == "" || output.Thinking == "" {
		return fmt.Errorf("%w: non-human route needs model type and thinking level", ErrInvalidWorkflowData)
	}
	return nil
}

func cloneFixedWorkflowInput(input FixedWorkflowInput) FixedWorkflowInput {
	input.Payload = cloneJSON(input.Payload)
	return input
}

func cloneIntakeOutput(output IntakeOutput) IntakeOutput {
	output.Data = cloneJSON(output.Data)
	return output
}

func cloneEvidenceOutput(output EvidenceOutput) EvidenceOutput {
	output.Data = cloneJSON(output.Data)
	return output
}

func cloneSafetyOutput(output SafetyOutput) SafetyOutput {
	output.Data = cloneJSON(output.Data)
	return output
}

func cloneResponseOutput(output ResponseOutput) ResponseOutput {
	output.Data = cloneJSON(output.Data)
	return output
}
