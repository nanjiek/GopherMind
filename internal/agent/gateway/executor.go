package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"gophermind/internal/agent/runtime"
)

var (
	ErrInvalidManifest     = errors.New("gateway executable manifest is invalid")
	ErrDuplicateExecutable = errors.New("gateway executable is already registered")
	ErrUnknownExecutable   = errors.New("gateway executable is not registered")
	ErrInvalidInvocation   = errors.New("gateway invocation is invalid")
	ErrOutputTooLarge      = errors.New("gateway executable output exceeds limit")
)

// ExecutableKind distinguishes constrained in-process Tools and Skills.
type ExecutableKind string

const (
	ExecutableTool  ExecutableKind = "tool"
	ExecutableSkill ExecutableKind = "skill"
)

// Manifest declares the smallest execution contract required by the Gateway.
// Input/output schema enforcement is added when schema registry work begins.
type Manifest struct {
	ID             string
	Kind           ExecutableKind
	Capability     runtime.Capability
	MaxOutputBytes int
}

// Handler performs the side effect only after Executor completes policy checks.
type Handler func(context.Context, json.RawMessage) (json.RawMessage, error)

// Invocation is trusted request metadata plus untrusted structured input.
type Invocation struct {
	Scope      runtime.Metadata
	AgentID    string
	Executable string
	Purpose    string
	Input      json.RawMessage
}

// Result contains only structured output and declared execution metadata.
type Result struct {
	Manifest Manifest
	Output   json.RawMessage
}

// Executor is an in-process registry that checks Capability policy immediately
// before invoking a registered Tool or Skill handler.
type Executor struct {
	policy *CapabilityPolicy
	mu     sync.RWMutex
	items  map[string]registeredExecutable
}

type registeredExecutable struct {
	manifest Manifest
	handler  Handler
}

func NewExecutor(policy *CapabilityPolicy) (*Executor, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: capability policy is required", ErrInvalidInvocation)
	}
	return &Executor{policy: policy, items: make(map[string]registeredExecutable)}, nil
}

// Register adds one restricted executable. Registration itself grants no
// permission; execution still requires an agent-specific policy grant.
func (e *Executor) Register(manifest Manifest, handler Handler) error {
	if err := validateManifest(manifest); err != nil || handler == nil {
		return fmt.Errorf("%w: valid manifest and handler are required", ErrInvalidManifest)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.items[manifest.ID]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateExecutable, manifest.ID)
	}
	e.items[manifest.ID] = registeredExecutable{manifest: manifest, handler: handler}
	return nil
}

// Execute validates input, authorizes the exact executable capability, then
// invokes its handler. Authorization is deliberately adjacent to the handler
// call so future callers cannot treat an earlier schema-filter check as enough.
func (e *Executor) Execute(ctx context.Context, invocation Invocation) (Result, error) {
	if invocation.Executable == "" || invocation.AgentID == "" || invocation.Purpose == "" || (len(invocation.Input) > 0 && !json.Valid(invocation.Input)) {
		return Result{}, ErrInvalidInvocation
	}
	e.mu.RLock()
	item, ok := e.items[invocation.Executable]
	e.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownExecutable, invocation.Executable)
	}
	if err := e.policy.Authorize(AccessRequest{
		Scope:      invocation.Scope,
		AgentID:    invocation.AgentID,
		Capability: item.manifest.Capability,
		Purpose:    invocation.Purpose,
	}); err != nil {
		return Result{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	output, err := item.handler(ctx, append(json.RawMessage(nil), invocation.Input...))
	if err != nil {
		return Result{}, err
	}
	if len(output) > item.manifest.MaxOutputBytes {
		return Result{}, ErrOutputTooLarge
	}
	if len(output) > 0 && !json.Valid(output) {
		return Result{}, ErrInvalidInvocation
	}
	return Result{Manifest: item.manifest, Output: append(json.RawMessage(nil), output...)}, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.ID == "" || manifest.Capability == "" || manifest.MaxOutputBytes <= 0 || (manifest.Kind != ExecutableTool && manifest.Kind != ExecutableSkill) {
		return ErrInvalidManifest
	}
	return nil
}
