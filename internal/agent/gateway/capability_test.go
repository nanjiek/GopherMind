package gateway

import (
	"errors"
	"testing"

	"gophermind/internal/agent/runtime"
)

func TestCapabilityPolicyAuthorizesExplicitGrant(t *testing.T) {
	policy, err := NewCapabilityPolicy([]Grant{{AgentID: "evidence", Capability: "knowledge.search"}})
	if err != nil {
		t.Fatalf("NewCapabilityPolicy() error = %v", err)
	}
	err = policy.Authorize(AccessRequest{AgentID: "evidence", Capability: "knowledge.search", Purpose: "evidence retrieval"})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
}

func TestCapabilityPolicyRejectsUnregisteredGrant(t *testing.T) {
	policy, err := NewCapabilityPolicy(nil)
	if err != nil {
		t.Fatalf("NewCapabilityPolicy() error = %v", err)
	}
	err = policy.Authorize(AccessRequest{AgentID: "unknown", Capability: "tool.call", Purpose: "test"})
	if !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Authorize() error = %v, want ErrCapabilityDenied", err)
	}
}

func TestCapabilityPolicyRequiresTrustedPatientScope(t *testing.T) {
	policy, err := NewCapabilityPolicy([]Grant{{AgentID: "medication", Capability: "patient.medication.read", PatientBound: true}})
	if err != nil {
		t.Fatalf("NewCapabilityPolicy() error = %v", err)
	}
	request := AccessRequest{AgentID: "medication", Capability: "patient.medication.read", Purpose: "safety review"}
	if err := policy.Authorize(request); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Authorize() without scope error = %v, want ErrCapabilityDenied", err)
	}
	request.Scope = runtime.Metadata{TenantID: "tenant", UserID: "user", PatientID: "patient"}
	if err := policy.Authorize(request); err != nil {
		t.Fatalf("Authorize() with scope error = %v", err)
	}
}

func TestCapabilityPolicyRejectsDuplicateGrant(t *testing.T) {
	_, err := NewCapabilityPolicy([]Grant{
		{AgentID: "agent", Capability: "tool.call"},
		{AgentID: "agent", Capability: "tool.call"},
	})
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("NewCapabilityPolicy() error = %v, want ErrInvalidGrant", err)
	}
}
