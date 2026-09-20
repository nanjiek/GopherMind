package gateway

import (
	"errors"
	"fmt"

	"gophermind/internal/agent/runtime"
)

var ErrInvalidFixedTeamDecision = errors.New("gateway fixed team decision is invalid")

// FixedTeamPath maps a trusted P3 routing Decision to the only P4 Team
// topology it may start. The mapping is closed: callers cannot select a Team
// path directly, and a model never supplies this Decision.
func FixedTeamPath(decision Decision) (runtime.FixedTeamPath, error) {
	if decision.RequiresHuman {
		if decision.Workflow != WorkflowManualEscalation || decision.Model != nil {
			return "", fmt.Errorf("%w: human escalation requires the manual workflow without a model", ErrInvalidFixedTeamDecision)
		}
		return runtime.FixedTeamPathHuman, nil
	}
	if decision.Model == nil {
		return "", fmt.Errorf("%w: non-human workflow requires a model route", ErrInvalidFixedTeamDecision)
	}
	switch decision.Workflow {
	case WorkflowSingleAgent:
		return runtime.FixedTeamPathSimple, nil
	case WorkflowClinicalReview:
		return runtime.FixedTeamPathStandard, nil
	case WorkflowManualEscalation:
		return "", fmt.Errorf("%w: manual workflow must require human escalation", ErrInvalidFixedTeamDecision)
	default:
		return "", fmt.Errorf("%w: unknown workflow %q", ErrInvalidFixedTeamDecision, decision.Workflow)
	}
}
