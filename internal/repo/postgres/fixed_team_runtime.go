package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"gophermind/internal/agent/runtime"
)

// FixedTeamRuntime wires P4's closed coordinator to the PostgreSQL Task,
// Mailbox, and Run authorities. Worker implementations remain injected so
// this package never gives them direct database or topology access.
type FixedTeamRuntime struct {
	coordinator *runtime.FixedTeamCoordinator
	checkpoints runtime.CheckpointStore
}

func NewFixedTeamRuntime(db *gorm.DB, team runtime.FixedTeam, lease time.Duration) *FixedTeamRuntime {
	if db == nil {
		return nil
	}
	return &FixedTeamRuntime{
		coordinator: &runtime.FixedTeamCoordinator{Tasks: NewTaskDAGStore(db), Mailbox: NewDurableMailboxStore(db), Team: team, Lease: lease},
		checkpoints: NewWorkflowCheckpointStore(db),
	}
}

// Start creates the canonical Run before creating its immutable Task graph.
// A duplicate Run must resume through Resume rather than create a second DAG.
func (r *FixedTeamRuntime) Start(ctx context.Context, spec runtime.FixedTeamSpec, request json.RawMessage) (runtime.ResponseOutput, error) {
	if r == nil || r.coordinator == nil || r.checkpoints == nil {
		return runtime.ResponseOutput{}, errors.New("fixed team runtime is nil")
	}
	_, err := r.checkpoints.Create(ctx, runtime.Checkpoint{RunID: spec.RunID, Scope: spec.Scope, WorkflowID: "fixed-team", WorkflowVersion: "v1", Status: runtime.RunRunning, Revision: 1, State: json.RawMessage(`{}`)})
	if err != nil {
		return runtime.ResponseOutput{}, err
	}
	return r.coordinator.Start(ctx, spec, request)
}

func (r *FixedTeamRuntime) Resume(ctx context.Context, scope runtime.Metadata, runID string) (runtime.ResponseOutput, error) {
	if r == nil || r.coordinator == nil {
		return runtime.ResponseOutput{}, errors.New("fixed team runtime is nil")
	}
	return r.coordinator.Resume(ctx, scope, runID)
}
