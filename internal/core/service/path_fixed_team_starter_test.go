package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gophermind/internal/agent/runtime"
)

func TestPathFixedTeamStarterUsesOnlyClosedPathTarget(t *testing.T) {
	simple, standard, human := &recordedFixedTeam{}, &recordedFixedTeam{}, &recordedFixedTeam{}
	starter := PathFixedTeamStarter{Simple: simple, Standard: standard, Human: human}
	spec := runtime.FixedTeamSpec{RunID: "run", Scope: runtime.Metadata{TenantID: "tenant", UserID: "user"}, Deadline: time.Now().Add(time.Minute), Path: runtime.FixedTeamPathStandard}
	_, err := starter.Start(context.Background(), spec, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Zero(t, simple.calls)
	require.Equal(t, 1, standard.calls)
	require.Zero(t, human.calls)
}
