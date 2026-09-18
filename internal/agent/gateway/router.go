// Package gateway contains the in-process Agent Gateway control-plane
// contracts. It deliberately does not execute models, tools, or MCP calls.
package gateway

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidRouteRequest = errors.New("gateway route request is invalid")
	ErrInvalidModelRoute   = errors.New("gateway model route is invalid")
)

// RiskLevel is the trusted risk classification supplied by an upstream policy
// or deterministic triage rule. The model cannot choose its own risk level.
type RiskLevel string

const (
	RiskL0 RiskLevel = "l0"
	RiskL1 RiskLevel = "l1"
	RiskL2 RiskLevel = "l2"
	RiskL3 RiskLevel = "l3"
)

// TaskKind identifies a bounded Gateway routing use case.
type TaskKind string

const (
	TaskClassification TaskKind = "classification"
	TaskSummary        TaskKind = "summary"
	TaskGeneralQA      TaskKind = "general_qa"
	TaskComplexReview  TaskKind = "complex_review"
	TaskMedication     TaskKind = "medication_safety"
	TaskFinalReview    TaskKind = "final_safety_review"
	TaskRedFlag        TaskKind = "red_flag_triage"
)

// WorkflowID is a versioned, constrained execution route rather than a model
// selected free-form workflow.
type WorkflowID string

const (
	WorkflowSingleAgent      WorkflowID = "single-agent-query"
	WorkflowClinicalReview   WorkflowID = "clinical-review"
	WorkflowManualEscalation WorkflowID = "manual-escalation"
)

// ThinkingLevel describes a configured reasoning budget without depending on
// a particular model-provider API.
type ThinkingLevel string

const (
	ThinkingOff    ThinkingLevel = "off"
	ThinkingLow    ThinkingLevel = "low"
	ThinkingMedium ThinkingLevel = "medium"
	ThinkingHigh   ThinkingLevel = "high"
)

// ModelRoute maps a logical route to an existing configured model type, such
// as "qwen" or "auto". It does not embed provider names or prices in code.
type ModelRoute struct {
	ModelType string
	Thinking  ThinkingLevel
}

// Request is the trusted input to model and workflow routing.
type Request struct {
	Risk            RiskLevel
	Task            TaskKind
	WorkflowVersion string
}

// Decision is the immutable routing result consumed by a later executor.
type Decision struct {
	Risk            RiskLevel
	Task            TaskKind
	Workflow        WorkflowID
	WorkflowVersion string
	Model           *ModelRoute
	RequiresHuman   bool
}

// Router selects from configured low- and high-risk model routes.
type Router struct {
	fast     ModelRoute
	advanced ModelRoute
}

// NewRouter validates the model-type aliases supplied by application config.
func NewRouter(fast ModelRoute, advanced ModelRoute) (*Router, error) {
	if err := validateModelRoute(fast); err != nil {
		return nil, err
	}
	if err := validateModelRoute(advanced); err != nil {
		return nil, err
	}
	return &Router{fast: fast, advanced: advanced}, nil
}

// Route deterministically selects a constrained workflow and model route.
// Red-flag requests never receive a model route: a later human-escalation
// executor must handle them instead of treating an LLM as the triage authority.
func (r *Router) Route(request Request) (Decision, error) {
	if !request.Risk.valid() || !request.Task.valid() {
		return Decision{}, ErrInvalidRouteRequest
	}
	version := request.WorkflowVersion
	if version == "" {
		version = "v1"
	}
	decision := Decision{Risk: request.Risk, Task: request.Task, WorkflowVersion: version}
	if request.Risk == RiskL3 || request.Task == TaskRedFlag {
		decision.Workflow = WorkflowManualEscalation
		decision.RequiresHuman = true
		return decision, nil
	}
	if request.Risk == RiskL2 || request.Task == TaskComplexReview || request.Task == TaskMedication || request.Task == TaskFinalReview {
		decision.Workflow = WorkflowClinicalReview
		decision.Model = cloneRoute(r.advanced)
		return decision, nil
	}
	decision.Workflow = WorkflowSingleAgent
	decision.Model = cloneRoute(r.fast)
	return decision, nil
}

func validateModelRoute(route ModelRoute) error {
	if route.ModelType == "" || !route.Thinking.valid() {
		return fmt.Errorf("%w: model type and known thinking level are required", ErrInvalidModelRoute)
	}
	return nil
}

func (risk RiskLevel) valid() bool {
	return risk == RiskL0 || risk == RiskL1 || risk == RiskL2 || risk == RiskL3
}

func (task TaskKind) valid() bool {
	switch task {
	case TaskClassification, TaskSummary, TaskGeneralQA, TaskComplexReview, TaskMedication, TaskFinalReview, TaskRedFlag:
		return true
	default:
		return false
	}
}

func (thinking ThinkingLevel) valid() bool {
	return thinking == ThinkingOff || thinking == ThinkingLow || thinking == ThinkingMedium || thinking == ThinkingHigh
}

func cloneRoute(route ModelRoute) *ModelRoute {
	copy := route
	return &copy
}
