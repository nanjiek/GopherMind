package runtime

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCheckpoint(t *testing.T) {
	checkpoint := testCheckpoint()
	if err := ValidateCheckpoint(checkpoint); err != nil {
		t.Fatalf("ValidateCheckpoint() error = %v", err)
	}

	invalid := []Checkpoint{
		func() Checkpoint { value := checkpoint; value.Scope.TenantID = ""; return value }(),
		func() Checkpoint { value := checkpoint; value.Revision = 0; return value }(),
		func() Checkpoint { value := checkpoint; value.Status = "unknown"; return value }(),
		func() Checkpoint { value := checkpoint; value.State = []byte(`[]`); return value }(),
		func() Checkpoint { value := checkpoint; value.State = []byte(`not-json`); return value }(),
		func() Checkpoint { value := checkpoint; value.FailureKind = FailureTimeout; return value }(),
		func() Checkpoint { value := checkpoint; value.ErrorCode = "timeout"; return value }(),
	}
	for _, value := range invalid {
		if err := ValidateCheckpoint(value); !errors.Is(err, ErrInvalidCheckpoint) {
			t.Fatalf("ValidateCheckpoint(%#v) error = %v", value, err)
		}
	}
}

func TestValidateCheckpointBoundsState(t *testing.T) {
	checkpoint := testCheckpoint()
	checkpoint.State = []byte(`{"data":"` + strings.Repeat("a", MaxCheckpointStateBytes) + `"}`)
	if err := ValidateCheckpoint(checkpoint); !errors.Is(err, ErrInvalidCheckpoint) {
		t.Fatalf("ValidateCheckpoint() error = %v", err)
	}
}

func TestCloneCheckpointCopiesState(t *testing.T) {
	checkpoint := testCheckpoint()
	cloned := cloneCheckpoint(checkpoint)
	copy(checkpoint.State, []byte(`{"state":"changed"}`))
	if string(cloned.State) != `{"state":"running"}` {
		t.Fatalf("cloned state = %s", cloned.State)
	}
}

func testCheckpoint() Checkpoint {
	return Checkpoint{
		RunID:           "2b1a4cca-9aef-4e0e-93e7-f6c8f9f3bb9a",
		Scope:           Metadata{TenantID: "tenant-a", UserID: "user-a", SessionID: "1e46a080-98b9-4e05-a4b0-2a67e6436e06"},
		WorkflowID:      "fixed-workflow",
		WorkflowVersion: "v1",
		Status:          RunRunning,
		CurrentNode:     "evidence",
		Revision:        1,
		State:           []byte(`{"state":"running"}`),
	}
}
