package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/agent/gateway"
	"gophermind/internal/agent/runtime"
)

func TestTeamQueryApplicationCommitsOnlyReviewedNonHumanResult(t *testing.T) {
	policy, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{{ID: "general", Phrases: []string{"health"}, Route: gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA}}})
	require.NoError(t, err)
	team := &recordedFixedTeam{output: runtime.ResponseOutput{Data: json.RawMessage(`{"answer":"reviewed"}`)}}
	router := &fixedDecisionSource{decision: gateway.Decision{Workflow: gateway.WorkflowSingleAgent, Model: &gateway.ModelRoute{ModelType: "fast", Thinking: gateway.ThinkingLow}}}
	verifier, authorizer, committer := &recordedReviewVerifier{}, &recordedCommitAuthorizer{}, &recordedResponseCommitter{}
	app := &TeamQueryApplication{Policy: policy, Team: NewRoutedTeamQueryService(router, team), Barrier: newResponseCommitBarrier(verifier, authorizer, committer)}

	out, err := app.Execute(context.Background(), trustedPolicyInput("health question"))
	require.NoError(t, err)
	require.False(t, out.RequiresHuman)
	require.Equal(t, 1, team.calls)
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, 1, committer.calls)
}

func TestTeamQueryApplicationNeverCommitsHumanHandoff(t *testing.T) {
	policy, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{{ID: "red", Phrases: []string{"chest pain"}, Route: gateway.Request{Risk: gateway.RiskL3, Task: gateway.TaskRedFlag}}})
	require.NoError(t, err)
	team := &recordedFixedTeam{output: runtime.ResponseOutput{Data: json.RawMessage(`{"requires_human":true}`)}}
	router := &fixedDecisionSource{decision: gateway.Decision{Workflow: gateway.WorkflowManualEscalation, RequiresHuman: true}}
	verifier, authorizer, committer := &recordedReviewVerifier{}, &recordedCommitAuthorizer{}, &recordedResponseCommitter{}
	app := &TeamQueryApplication{Policy: policy, Team: NewRoutedTeamQueryService(router, team), Barrier: newResponseCommitBarrier(verifier, authorizer, committer)}

	out, err := app.Execute(context.Background(), trustedPolicyInput("chest pain"))
	require.NoError(t, err)
	require.True(t, out.RequiresHuman)
	require.Zero(t, verifier.calls)
	require.Zero(t, authorizer.calls)
	require.Zero(t, committer.calls)
}
