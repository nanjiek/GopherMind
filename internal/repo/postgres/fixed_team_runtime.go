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
	// The request Run ID is part of every worker scope. This prevents a
	// capability authorization or audit record from becoming detached from the
	// immutable Run created below.
	if spec.Scope.RunID == "" {
		spec.Scope.RunID = spec.RunID
	}
	requestScope := runtime.NewScope(ctx, spec.Scope)
	defer requestScope.Close(context.Background())
	_, err := r.checkpoints.Create(requestScope.Context(), runtime.Checkpoint{RunID: spec.RunID, Scope: spec.Scope, WorkflowID: "fixed-team", WorkflowVersion: "v1", Status: runtime.RunRunning, Revision: 1, State: json.RawMessage(`{}`)})
	if err != nil {
		return runtime.ResponseOutput{}, err
	}
	return r.coordinator.Start(requestScope.Context(), spec, request)
}

func (r *FixedTeamRuntime) Resume(ctx context.Context, scope runtime.Metadata, runID string) (runtime.ResponseOutput, error) {
	if r == nil || r.coordinator == nil {
		return runtime.ResponseOutput{}, errors.New("fixed team runtime is nil")
	}
	return r.coordinator.Resume(ctx, scope, runID)
}
