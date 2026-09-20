package compaction

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildInputReservesOutputInsideRealBudget(t *testing.T) {
	input, err := BuildInput(Scope{TenantID: "t", UserID: "u", SessionID: "s"}, json.RawMessage(`{"topics":["prior"]}`), []Event{{StreamSeq: 3, EventType: "message", Payload: json.RawMessage(`{"content":"hello"}`)}}, 3, Budget{MaxInputTokens: 100, ReserveOutputTokens: 20})
	require.NoError(t, err)
	require.Greater(t, input.InputTokens, 0)
	require.LessOrEqual(t, input.InputTokens+input.ReservedOutputTokens, 100)
}

func TestBuildInputRejectsOversizedPromptInsteadOfTruncatingSilently(t *testing.T) {
	_, err := BuildInput(Scope{TenantID: "t", UserID: "u", SessionID: "s"}, nil, []Event{{StreamSeq: 1, Payload: json.RawMessage(`{"content":"this is deliberately too long"}`)}}, 1, Budget{MaxInputTokens: 5, ReserveOutputTokens: 2})
	require.ErrorIs(t, err, ErrInvalidBudget)
}
