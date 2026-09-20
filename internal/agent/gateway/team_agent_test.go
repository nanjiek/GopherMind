package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/agent/runtime"
)

type recordedTeamExecutor struct {
	calls int
	last  Invocation
}

func (r *recordedTeamExecutor) Execute(_ context.Context, invocation Invocation) (Result, error) {
	r.calls++
	r.last = invocation
	return Result{Output: json.RawMessage(`{"answer":"ok"}`)}, nil
}

func TestAuthorizedTeamAgentUsesOnlyRuntimeScope(t *testing.T) {
	executor := &recordedTeamExecutor{}
	agent := AuthorizedTeamAgent(executor, runtime.TeamAgentResponse, "model-response", "produce reviewed response")
	scope := runtime.NewScope(context.Background(), runtime.Metadata{TenantID: "tenant", UserID: "user", RunID: "run"})
	defer scope.Close(context.Background())

	output, err := agent(scope.Context(), json.RawMessage(`{"scope":"forged","answer":"input"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"answer":"ok"}`, string(output))
	require.Equal(t, 1, executor.calls)
	require.Equal(t, "tenant", executor.last.Scope.TenantID)
	require.Equal(t, runtime.TeamAgentResponse, executor.last.AgentID)
	require.Equal(t, "model-response", executor.last.Executable)
}

func TestAuthorizedTeamAgentRejectsUnscopedExecution(t *testing.T) {
	_, err := AuthorizedTeamAgent(&recordedTeamExecutor{}, runtime.TeamAgentResponse, "model-response", "produce reviewed response")(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
}
