package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/core/model"
)

type modelExecutableRouter struct{ output string }

func (r modelExecutableRouter) Get(string) (ModelProvider, error) { return nil, nil }
func (r modelExecutableRouter) GenerateWithFallback(context.Context, string, string) (string, model.Usage, error) {
	return r.output, model.Usage{}, nil
}
func (r modelExecutableRouter) GenerateStreamWithFallback(context.Context, string, string, func(string) error) (string, model.Usage, error) {
	return r.output, model.Usage{}, nil
}

func TestModelTeamExecutableRequiresRoleSchemas(t *testing.T) {
	safety := ModelTeamExecutable(modelExecutableRouter{output: `{"approved":true,"review_context":"safe"}`}, "fast", "safety")
	out, err := safety(context.Background(), json.RawMessage(`{"evidence":"x"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"approved":true,"review_context":"safe"}`, string(out))

	rejected := ModelTeamExecutable(modelExecutableRouter{output: `{"approved":false}`}, "fast", "safety")
	_, err = rejected(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)

	response := ModelTeamExecutable(modelExecutableRouter{output: `{"answer":"ok"}`}, "fast", "response")
	_, err = response(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
}
