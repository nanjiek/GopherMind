package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gophermind/internal/agent/runtime"
)

func TestExecutorAuthorizesImmediatelyBeforeHandler(t *testing.T) {
	policy, err := NewCapabilityPolicy([]Grant{{AgentID: "evidence", Capability: "knowledge.search"}})
	if err != nil {
		t.Fatalf("NewCapabilityPolicy() error = %v", err)
	}
	executor, err := NewExecutor(policy)
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	called := false
	if err := executor.Register(Manifest{ID: "search", Kind: ExecutableSkill, Capability: "knowledge.search", MaxOutputBytes: 128}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		called = true
		return []byte(`{"hits":[]}`), nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	result, err := executor.Execute(context.Background(), Invocation{AgentID: "evidence", Executable: "search", Purpose: "evidence retrieval", Input: []byte(`{"query":"fever"}`)})
	if err != nil || !called || string(result.Output) != `{"hits":[]}` {
		t.Fatalf("Execute() = %#v, %v, called=%v", result, err, called)
	}
}

func TestExecutorDenialPreventsHandler(t *testing.T) {
	policy, _ := NewCapabilityPolicy(nil)
	executor, _ := NewExecutor(policy)
	called := false
	_ = executor.Register(Manifest{ID: "tool", Kind: ExecutableTool, Capability: "patient.read", MaxOutputBytes: 128}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		called = true
		return []byte(`{}`), nil
	})
	_, err := executor.Execute(context.Background(), Invocation{AgentID: "untrusted", Executable: "tool", Purpose: "test", Input: []byte(`{}`)})
	if !errors.Is(err, ErrCapabilityDenied) || called {
		t.Fatalf("Execute() error = %v, called=%v", err, called)
	}
}

func TestExecutorRejectsInvalidOrOversizedOutput(t *testing.T) {
	policy, _ := NewCapabilityPolicy([]Grant{{AgentID: "agent", Capability: "tool.call"}})
	executor, _ := NewExecutor(policy)
	_ = executor.Register(Manifest{ID: "bad", Kind: ExecutableTool, Capability: "tool.call", MaxOutputBytes: 1}, func(context.Context, json.RawMessage) (json.RawMessage, error) { return []byte(`{}`), nil })
	_, err := executor.Execute(context.Background(), Invocation{AgentID: "agent", Executable: "bad", Purpose: "test", Input: []byte(`{}`)})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("Execute() error = %v, want ErrOutputTooLarge", err)
	}
	_, err = executor.Execute(context.Background(), Invocation{AgentID: "agent", Executable: "bad", Purpose: "test", Input: []byte(`not-json`)})
	if !errors.Is(err, ErrInvalidInvocation) {
		t.Fatalf("invalid input error = %v, want ErrInvalidInvocation", err)
	}
}

func TestExecutorPatientBoundGrantUsesTrustedScope(t *testing.T) {
	policy, _ := NewCapabilityPolicy([]Grant{{AgentID: "medication", Capability: "patient.medication.read", PatientBound: true}})
	executor, _ := NewExecutor(policy)
	_ = executor.Register(Manifest{ID: "medications", Kind: ExecutableTool, Capability: "patient.medication.read", MaxOutputBytes: 128}, func(context.Context, json.RawMessage) (json.RawMessage, error) { return []byte(`[]`), nil })
	request := Invocation{AgentID: "medication", Executable: "medications", Purpose: "safety review", Input: []byte(`{}`)}
	if _, err := executor.Execute(context.Background(), request); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Execute() without scope error = %v", err)
	}
	request.Scope = runtime.Metadata{TenantID: "tenant", UserID: "user", PatientID: "patient"}
	if _, err := executor.Execute(context.Background(), request); err != nil {
		t.Fatalf("Execute() with scope error = %v", err)
	}
}
