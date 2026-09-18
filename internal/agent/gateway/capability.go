package gateway

import (
	"errors"
	"fmt"

	"gophermind/internal/agent/runtime"
)

var (
	ErrInvalidGrant     = errors.New("gateway capability grant is invalid")
	ErrCapabilityDenied = errors.New("gateway capability is denied")
)

// Grant allows one named agent to use one declared runtime capability.
// PatientBound grants additionally require a trusted patient scope.
type Grant struct {
	AgentID      string
	Capability   runtime.Capability
	PatientBound bool
}

// AccessRequest is the trusted policy input checked before a future tool or
// skill executor runs. Prompt content cannot add a capability to this request.
type AccessRequest struct {
	Scope      runtime.Metadata
	AgentID    string
	Capability runtime.Capability
	Purpose    string
}

// CapabilityPolicy is an immutable, in-process allow-list.
type CapabilityPolicy struct {
	grants map[string]map[runtime.Capability]Grant
}

// NewCapabilityPolicy validates unique explicit grants.
func NewCapabilityPolicy(grants []Grant) (*CapabilityPolicy, error) {
	policy := &CapabilityPolicy{grants: make(map[string]map[runtime.Capability]Grant)}
	for _, grant := range grants {
		if grant.AgentID == "" || grant.Capability == "" {
			return nil, ErrInvalidGrant
		}
		byCapability := policy.grants[grant.AgentID]
		if byCapability == nil {
			byCapability = make(map[runtime.Capability]Grant)
			policy.grants[grant.AgentID] = byCapability
		}
		if _, exists := byCapability[grant.Capability]; exists {
			return nil, fmt.Errorf("%w: duplicate %q for agent %q", ErrInvalidGrant, grant.Capability, grant.AgentID)
		}
		byCapability[grant.Capability] = grant
	}
	return policy, nil
}

// Authorize permits only explicitly granted capabilities. A patient-bound
// grant cannot be used without tenant, user, and patient identifiers supplied
// by authenticated request handling.
func (p *CapabilityPolicy) Authorize(request AccessRequest) error {
	if request.AgentID == "" || request.Capability == "" || request.Purpose == "" {
		return fmt.Errorf("%w: agent, capability, and purpose are required", ErrCapabilityDenied)
	}
	grant, ok := p.grants[request.AgentID][request.Capability]
	if !ok {
		return fmt.Errorf("%w: agent %q lacks %q", ErrCapabilityDenied, request.AgentID, request.Capability)
	}
	if grant.PatientBound && (request.Scope.TenantID == "" || request.Scope.UserID == "" || request.Scope.PatientID == "") {
		return fmt.Errorf("%w: patient-bound %q requires tenant, user, and patient scope", ErrCapabilityDenied, request.Capability)
	}
	return nil
}

// Allows is a convenience check for schema filtering. Executors must still
// call Authorize immediately before the side effect in a later P3 node.
func (p *CapabilityPolicy) Allows(request AccessRequest) bool {
	return p.Authorize(request) == nil
}
