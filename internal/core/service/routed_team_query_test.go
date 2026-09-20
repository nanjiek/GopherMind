package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
)

type recordedFixedTeam struct {
	calls   int
	spec    runtime.FixedTeamSpec
	payload json.RawMessage
	output  runtime.ResponseOutput
	err     error
}

func (f *recordedFixedTeam) Start(_ context.Context, spec runtime.FixedTeamSpec, payload json.RawMessage) (runtime.ResponseOutput, error) {
	f.calls++
	f.spec = spec
	f.payload = append(json.RawMessage(nil), payload...)
	return f.output, f.err
}

type fixedDecisionSource struct {
	decision gateway.Decision
	err      error
	calls    int
}

func (f *fixedDecisionSource) Route(gateway.Request) (gateway.Decision, error) {
	f.calls++
	return f.decision, f.err
}

func TestRoutedTeamQueryServiceStartsOnlyRouterSelectedPath(t *testing.T) {
	team := &recordedFixedTeam{output: runtime.ResponseOutput{Data: json.RawMessage(`{"answer":"safe"}`)}}
	router := &fixedDecisionSource{decision: gateway.Decision{
		Workflow: gateway.WorkflowClinicalReview,
		Model:    &gateway.ModelRoute{ModelType: "advanced", Thinking: gateway.ThinkingHigh},
	}}
	svc := NewRoutedTeamQueryService(router, team)
	payload := json.RawMessage(`{"question":"q"}`)

	out, err := svc.Start(context.Background(), validRoutedTeamInput(payload))
	require.NoError(t, err)
	require.Equal(t, gateway.WorkflowClinicalReview, out.Workflow)
	require.Equal(t, runtime.FixedTeamPathStandard, out.Path)
	require.False(t, out.RequiresHuman)
	require.JSONEq(t, `{"answer":"safe"}`, string(out.Data))
	require.Equal(t, 1, router.calls)
	require.Equal(t, 1, team.calls)
	require.Equal(t, runtime.FixedTeamPathStandard, team.spec.Path)
	require.Equal(t, "tenant-a", team.spec.Scope.TenantID)
	require.JSONEq(t, `{"question":"q"}`, string(team.payload))

	payload[2] = 'X'
	require.JSONEq(t, `{"question":"q"}`, string(team.payload))
}

func TestRoutedTeamQueryServiceMarksHumanHandoffWithoutPublishingAnswer(t *testing.T) {
	team := &recordedFixedTeam{output: runtime.ResponseOutput{Data: json.RawMessage(`{"requires_human":true,"triage":{"level":"l3"}}`)}}
	svc := NewRoutedTeamQueryService(&fixedDecisionSource{decision: gateway.Decision{
		Workflow: gateway.WorkflowManualEscalation, RequiresHuman: true,
	}}, team)

	out, err := svc.Start(context.Background(), validRoutedTeamInput(json.RawMessage(`{"question":"chest pain"}`)))
	require.NoError(t, err)
	require.True(t, out.RequiresHuman)
	require.Equal(t, gateway.WorkflowManualEscalation, out.Workflow)
	require.Equal(t, runtime.FixedTeamPathHuman, out.Path)
	require.JSONEq(t, `{"requires_human":true,"triage":{"level":"l3"}}`, string(out.Data))
	require.Equal(t, 1, team.calls)
}

func TestRoutedTeamQueryServiceRejectsInvalidInputOrForgedDecisionBeforeTeam(t *testing.T) {
	team := &recordedFixedTeam{}
	router := &fixedDecisionSource{decision: gateway.Decision{
		Workflow: gateway.WorkflowManualEscalation,
		Model:    &gateway.ModelRoute{ModelType: "forged", Thinking: gateway.ThinkingLow},
	}}
	svc := NewRoutedTeamQueryService(router, team)

	badPayload := validRoutedTeamInput(json.RawMessage(`[]`))
	_, err := svc.Start(context.Background(), badPayload)
	require.ErrorIs(t, err, ErrInvalidRoutedTeamQuery)
	require.Zero(t, router.calls)
	require.Zero(t, team.calls)

	_, err = svc.Start(context.Background(), validRoutedTeamInput(json.RawMessage(`{"question":"q"}`)))
	require.ErrorIs(t, err, gateway.ErrInvalidFixedTeamDecision)
	require.Equal(t, 1, router.calls)
	require.Zero(t, team.calls)
}

func TestRoutedTeamQueryServiceDoesNotStartTeamWhenRoutingFails(t *testing.T) {
	team := &recordedFixedTeam{}
	router := &fixedDecisionSource{err: errors.New("routing unavailable")}
	svc := NewRoutedTeamQueryService(router, team)

	_, err := svc.Start(context.Background(), validRoutedTeamInput(json.RawMessage(`{"question":"q"}`)))
	require.EqualError(t, err, "routing unavailable")
	require.Zero(t, team.calls)
}

func validRoutedTeamInput(payload json.RawMessage) RoutedTeamQueryInput {
	return RoutedTeamQueryInput{
		RunID:    "run-a",
		Scope:    runtime.Metadata{TenantID: "tenant-a", UserID: "user-a", SessionID: "session-a"},
		Deadline: time.Now().Add(time.Minute),
		Route:    gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA},
		Payload:  payload,
	}
}
