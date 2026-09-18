package runtime

import (
	"errors"
	"testing"
)

func TestNewTaskDAGDerivesInitialStatesAndCopiesDependencies(t *testing.T) {
	input := testTaskDAG()
	dag, err := NewTaskDAG(input)
	if err != nil {
		t.Fatalf("NewTaskDAG() error = %v", err)
	}
	if got, want := dag.Tasks[0].Status, TaskReady; got != want {
		t.Fatalf("root status = %q, want %q", got, want)
	}
	if got, want := dag.Tasks[1].Status, TaskBlocked; got != want {
		t.Fatalf("dependent status = %q, want %q", got, want)
	}
	input.Tasks[1].BlockedBy[0] = "changed"
	if got, want := dag.Tasks[1].BlockedBy[0], "intake"; got != want {
		t.Fatalf("dependency = %q, want %q", got, want)
	}
}

func TestNewTaskDAGRejectsInvalidIdentityAndDependencies(t *testing.T) {
	base := testTaskDAG()
	cases := []TaskDAG{
		func() TaskDAG { value := cloneTaskDAG(base); value.Scope.UserID = ""; return value }(),
		func() TaskDAG { value := cloneTaskDAG(base); value.Tasks[1].TaskID = "intake"; return value }(),
		func() TaskDAG {
			value := cloneTaskDAG(base)
			value.Tasks[1].IdempotencyKey = "intake-key"
			return value
		}(),
		func() TaskDAG {
			value := cloneTaskDAG(base)
			value.Tasks[1].BlockedBy = []string{"missing"}
			return value
		}(),
		func() TaskDAG {
			value := cloneTaskDAG(base)
			value.Tasks[1].BlockedBy = []string{"intake", "intake"}
			return value
		}(),
		func() TaskDAG { value := cloneTaskDAG(base); value.Tasks[1].Status = TaskReady; return value }(),
	}
	for _, value := range cases {
		if _, err := NewTaskDAG(value); !errors.Is(err, ErrInvalidTaskDAG) {
			t.Fatalf("NewTaskDAG(%#v) error = %v", value, err)
		}
	}
}

func TestNewTaskDAGRejectsDependencyCycle(t *testing.T) {
	dag := testTaskDAG()
	dag.Tasks[0].BlockedBy = []string{"evidence"}
	if _, err := NewTaskDAG(dag); !errors.Is(err, ErrTaskDAGCycle) {
		t.Fatalf("NewTaskDAG() error = %v", err)
	}
}

func testTaskDAG() TaskDAG {
	return TaskDAG{
		RunID: "run-a", Scope: Metadata{TenantID: "tenant-a", UserID: "user-a", PatientID: "patient-a", SessionID: "session-a"},
		Tasks: []AgentTask{
			{TaskID: "intake", Type: "intake", OwnerAgentID: "intake-agent", IdempotencyKey: "intake-key", Revision: 1},
			{TaskID: "evidence", Type: "evidence", OwnerAgentID: "evidence-agent", IdempotencyKey: "evidence-key", BlockedBy: []string{"intake"}, Revision: 1},
		},
	}
}
