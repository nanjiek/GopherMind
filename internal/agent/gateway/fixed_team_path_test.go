package gateway

import (
	"errors"
	"testing"

	"gophermind/internal/agent/runtime"
)

func TestFixedTeamPathMapsOnlyTrustedWorkflowDecisions(t *testing.T) {
	model := &ModelRoute{ModelType: "configured", Thinking: ThinkingLow}
	cases := []struct {
		name     string
		decision Decision
		want     runtime.FixedTeamPath
	}{
		{"simple", Decision{Workflow: WorkflowSingleAgent, Model: model}, runtime.FixedTeamPathSimple},
		{"standard", Decision{Workflow: WorkflowClinicalReview, Model: model}, runtime.FixedTeamPathStandard},
		{"human", Decision{Workflow: WorkflowManualEscalation, RequiresHuman: true}, runtime.FixedTeamPathHuman},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := FixedTeamPath(test.decision)
			if err != nil || got != test.want {
				t.Fatalf("FixedTeamPath() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestFixedTeamPathRejectsForgedOrContradictoryDecisions(t *testing.T) {
	model := &ModelRoute{ModelType: "configured", Thinking: ThinkingLow}
	cases := []Decision{
		{Workflow: WorkflowManualEscalation, Model: model},
		{Workflow: WorkflowSingleAgent, RequiresHuman: true},
		{Workflow: WorkflowClinicalReview},
		{Workflow: "invented", Model: model},
	}
	for _, decision := range cases {
		if _, err := FixedTeamPath(decision); !errors.Is(err, ErrInvalidFixedTeamDecision) {
			t.Fatalf("FixedTeamPath(%#v) error = %v", decision, err)
		}
	}
}
