package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"gophermind/internal/agent/runtime"
)

// AuthorizedTeamAgent adapts one fixed Team worker to the P3 Executor. Scope
// is read only from the runtime context, never from worker input; Execute
// reauthorizes immediately before the registered side effect.
func AuthorizedTeamAgent(executor interface {
	Execute(context.Context, Invocation) (Result, error)
}, agentID, executable, purpose string) runtime.TeamAgent {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if executor == nil || agentID == "" || executable == "" || purpose == "" {
			return nil, fmt.Errorf("authorized team agent is not configured")
		}
		scope, ok := runtime.MetadataFromContext(ctx)
		if !ok {
			return nil, fmt.Errorf("authorized team agent requires runtime scope")
		}
		result, err := executor.Execute(ctx, Invocation{Scope: scope, AgentID: agentID, Executable: executable, Purpose: purpose, Input: append(json.RawMessage(nil), input...)})
		if err != nil {
			return nil, err
		}
		return append(json.RawMessage(nil), result.Output...), nil
	}
}
