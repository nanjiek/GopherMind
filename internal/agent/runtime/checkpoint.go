package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidCheckpoint  = errors.New("runtime checkpoint is invalid")
	ErrCheckpointConflict = errors.New("runtime checkpoint revision conflicts")
	ErrCheckpointNotFound = errors.New("runtime checkpoint is not found")
)

// MaxCheckpointStateBytes bounds opaque structured node state kept by the Go
// checkpoint authority. It excludes hidden model reasoning and large tool
// results, which require their own future storage contract.
const MaxCheckpointStateBytes = 1 << 20

// Checkpoint is the durable, scope-bound execution state of one Run. State is
// structured node data only; it must never contain hidden model reasoning.
type Checkpoint struct {
	RunID           string
	Scope           Metadata
	WorkflowID      string
	WorkflowVersion string
	Status          RunStatus
	CurrentNode     string
	Revision        int64
	State           json.RawMessage
	FailureKind     FailureKind
	ErrorCode       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CheckpointStore is the single durable authority for one Run's checkpoint
// revision. Save uses compare-and-swap so an obsolete worker cannot replace a
// newer checkpoint. Task DAG, Mailbox, leasing, and queue delivery are later
// P4 contracts and intentionally not represented here.
type CheckpointStore interface {
	Create(context.Context, Checkpoint) (Checkpoint, error)
	Save(context.Context, int64, Checkpoint) (Checkpoint, error)
	Load(context.Context, Metadata, string) (Checkpoint, error)
}

// ValidateCheckpoint rejects ambiguous scope, state, and lifecycle data before
// it crosses a durable storage boundary.
func ValidateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.RunID == "" || checkpoint.Scope.TenantID == "" || checkpoint.Scope.UserID == "" || checkpoint.WorkflowID == "" || checkpoint.WorkflowVersion == "" || checkpoint.Revision <= 0 {
		return fmt.Errorf("%w: run ID, trusted tenant/user scope, workflow ID/version, and positive revision are required", ErrInvalidCheckpoint)
	}
	if !checkpoint.Status.valid() {
		return fmt.Errorf("%w: unknown run status", ErrInvalidCheckpoint)
	}
	if checkpoint.FailureKind != "" && !checkpoint.FailureKind.valid() {
		return fmt.Errorf("%w: unknown failure kind", ErrInvalidCheckpoint)
	}
	if (checkpoint.FailureKind == "") != (checkpoint.ErrorCode == "") {
		return fmt.Errorf("%w: failure kind and error code must be supplied together", ErrInvalidCheckpoint)
	}
	if len(checkpoint.State) == 0 || len(checkpoint.State) > MaxCheckpointStateBytes || !json.Valid(checkpoint.State) {
		return fmt.Errorf("%w: state must be bounded valid JSON", ErrInvalidCheckpoint)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(checkpoint.State, &object); err != nil || object == nil {
		return fmt.Errorf("%w: state must be a JSON object", ErrInvalidCheckpoint)
	}
	return nil
}

func cloneCheckpoint(checkpoint Checkpoint) Checkpoint {
	checkpoint.State = cloneJSON(checkpoint.State)
	return checkpoint
}
