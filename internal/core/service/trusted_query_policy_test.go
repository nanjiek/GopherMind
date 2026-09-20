package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gophermind/internal/agent/gateway"
)

func TestTrustedQueryPolicyBuildsOnlyConfiguredRouteAndServerScope(t *testing.T) {
	policy, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{
		{ID: "general", Priority: 10, Phrases: []string{"health"}, Route: gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA}},
		{ID: "red-flag", Priority: 100, Phrases: []string{"chest pain"}, Route: gateway.Request{Risk: gateway.RiskL3, Task: gateway.TaskRedFlag}},
	})
	require.NoError(t, err)

	out, err := policy.Build(trustedPolicyInput("chest pain health question"))
	require.NoError(t, err)
	require.Equal(t, gateway.RiskL3, out.Route.Risk)
	require.Equal(t, gateway.TaskRedFlag, out.Route.Task)
	require.Equal(t, "tenant-a", out.Scope.TenantID)
	require.Equal(t, "user-a", out.Scope.UserID)
	require.Equal(t, "session-a", out.Scope.SessionID)
	require.JSONEq(t, `{"question":"chest pain health question","document_id":"doc-a"}`, string(out.Payload))
}

func TestTrustedQueryPolicyFailsClosedWithoutMatchOrOnEqualPriorityConflict(t *testing.T) {
	noMatch, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{
		{ID: "general", Priority: 1, Phrases: []string{"health"}, Route: gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA}},
	})
	require.NoError(t, err)
	_, err = noMatch.Build(trustedPolicyInput("unclassified question"))
	require.ErrorIs(t, err, ErrNoTrustedQueryRoute)

	conflict, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{
		{ID: "a", Priority: 1, Phrases: []string{"pain"}, Route: gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA}},
		{ID: "b", Priority: 1, Phrases: []string{"pain"}, Route: gateway.Request{Risk: gateway.RiskL2, Task: gateway.TaskComplexReview}},
	})
	require.NoError(t, err)
	_, err = conflict.Build(trustedPolicyInput("pain"))
	require.ErrorIs(t, err, ErrAmbiguousTrustedQueryRoute)
}

func TestTrustedQueryPolicyRejectsInvalidRulesAndUntrustedInput(t *testing.T) {
	_, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{{
		ID: "bad", Phrases: []string{"health"}, Route: gateway.Request{Risk: "invented", Task: gateway.TaskGeneralQA},
	}})
	require.ErrorIs(t, err, ErrInvalidTrustedQueryPolicy)

	policy, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{{
		ID: "general", Phrases: []string{"health"}, Route: gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA},
	}})
	require.NoError(t, err)
	input := trustedPolicyInput("health")
	input.Identity.TenantID = ""
	_, err = policy.Build(input)
	require.ErrorIs(t, err, ErrInvalidTrustedQueryPolicy)
}

func TestTrustedQueryPolicyPayloadIsStructured(t *testing.T) {
	policy, err := NewTrustedQueryPolicy([]TrustedQueryRouteRule{{
		ID: "general", Phrases: []string{"health"}, Route: gateway.Request{Risk: gateway.RiskL1, Task: gateway.TaskGeneralQA},
	}})
	require.NoError(t, err)
	out, err := policy.Build(trustedPolicyInput("health"))
	require.NoError(t, err)
	var payload map[string]string
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	require.Equal(t, "health", payload["question"])

	_, err = policy.Build(TrustedQueryPolicyInput{})
	require.True(t, errors.Is(err, ErrInvalidTrustedQueryPolicy))
}

func trustedPolicyInput(question string) TrustedQueryPolicyInput {
	return TrustedQueryPolicyInput{
		Identity:   TrustedQueryIdentity{TenantID: "tenant-a", UserID: "user-a", SessionID: "session-a"},
		RunID:      "run-a",
		Deadline:   time.Now().Add(time.Minute),
		Question:   question,
		DocumentID: "doc-a",
	}
}
