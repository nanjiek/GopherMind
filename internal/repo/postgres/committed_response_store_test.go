package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gophermind/internal/agent/runtime"
	"gophermind/internal/core/service"
)

type safetyDAGLoader struct {
	dag runtime.TaskDAG
	err error
}

func (s safetyDAGLoader) Create(context.Context, runtime.TaskDAG) (runtime.TaskDAG, error) {
	return runtime.TaskDAG{}, nil
}
func (s safetyDAGLoader) Load(context.Context, runtime.Metadata, string) (runtime.TaskDAG, error) {
	return s.dag, s.err
}

func TestPostgresSafetyReviewVerifierRequiresDurableApprovedSafetyTask(t *testing.T) {
	runID := uuid.NewString()
	scope := runtime.Metadata{TenantID: "tenant", UserID: "user"}
	verifier := PostgresSafetyReviewVerifier{Tasks: safetyDAGLoader{dag: runtime.TaskDAG{RunID: runID, Scope: scope, Tasks: []runtime.AgentTask{{Type: "safety", Status: runtime.TaskSucceeded, Output: json.RawMessage(`{"approved":true}`)}}}}}
	require.NoError(t, verifier.VerifyReviewedResponse(context.Background(), service.ReviewedTeamResponse{RunID: runID, Scope: scope, Data: json.RawMessage(`{"answer":"ok"}`)}))

	verifier.Tasks = safetyDAGLoader{dag: runtime.TaskDAG{RunID: runID, Scope: scope, Tasks: []runtime.AgentTask{{Type: "safety", Status: runtime.TaskSucceeded, Output: json.RawMessage(`{"safe":true}`)}}}}
	require.ErrorIs(t, verifier.VerifyReviewedResponse(context.Background(), service.ReviewedTeamResponse{RunID: runID, Scope: scope, Data: json.RawMessage(`{"answer":"ok"}`)}), service.ErrResponseCommitBlocked)
}
