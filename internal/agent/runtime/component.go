package runtime

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidComponent    = errors.New("runtime component is invalid")
	ErrDuplicateComponent  = errors.New("runtime component name is duplicated")
	ErrDuplicateCapability = errors.New("runtime capability provider is duplicated")
	ErrMissingCapability   = errors.New("runtime component dependency is unavailable")
)

// Capability identifies a dependency a runtime component consumes or provides.
// It is an in-process startup contract, not an authorization permission.
type Capability string

// ComponentSpec describes one lifecycle-managed runtime component. Start must
// register every created resource with scope before returning successfully.
type ComponentSpec struct {
	Name     string
	Requires []Capability
	Provides []Capability
	Start    func(scope *Scope) error
}

// Components owns the single scope shared by a successfully started component
// set. Closing it cancels work and releases all component resources in reverse
// startup order through Scope's LIFO disposer stack.
type Components struct {
	scope        *Scope
	capabilities map[Capability]struct{}
}

// Context returns the lifecycle context shared by the started components.
func (c *Components) Context() context.Context { return c.scope.Context() }

// Close cancels component work and releases their registered resources.
func (c *Components) Close(ctx context.Context) error { return c.scope.Close(ctx) }

// HasCapability reports whether the completed component set provides, or was
// started with, capability.
func (c *Components) HasCapability(capability Capability) bool {
	_, ok := c.capabilities[capability]
	return ok
}

// StartComponents validates component contracts, resolves a deterministic
// startup order, and starts all components as one lifecycle transaction.
// initial contains capabilities supplied by the hosting application. If any
// component fails to start, Scope.Initialize rolls back all resources created
// by it and earlier components.
func StartComponents(parent context.Context, metadata Metadata, initial []Capability, specs []ComponentSpec) (*Components, error) {
	ordered, capabilities, err := resolveComponents(initial, specs)
	if err != nil {
		return nil, err
	}

	scope, err := Initialize(parent, metadata, func(scope *Scope) error {
		for _, spec := range ordered {
			if err := spec.Start(scope); err != nil {
				return fmt.Errorf("start component %q: %w", spec.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Components{scope: scope, capabilities: capabilities}, nil
}

func resolveComponents(initial []Capability, specs []ComponentSpec) ([]ComponentSpec, map[Capability]struct{}, error) {
	available := make(map[Capability]struct{}, len(initial))
	for _, capability := range initial {
		if capability == "" {
			return nil, nil, fmt.Errorf("%w: empty initial capability", ErrInvalidComponent)
		}
		available[capability] = struct{}{}
	}

	names := make(map[string]struct{}, len(specs))
	providers := make(map[Capability]string)
	for _, spec := range specs {
		if spec.Name == "" || spec.Start == nil {
			return nil, nil, fmt.Errorf("%w: component needs a name and Start function", ErrInvalidComponent)
		}
		if _, exists := names[spec.Name]; exists {
			return nil, nil, fmt.Errorf("%w: %s", ErrDuplicateComponent, spec.Name)
		}
		names[spec.Name] = struct{}{}
		for _, capability := range spec.Provides {
			if capability == "" {
				return nil, nil, fmt.Errorf("%w: component %q provides an empty capability", ErrInvalidComponent, spec.Name)
			}
			if _, exists := available[capability]; exists {
				return nil, nil, fmt.Errorf("%w: %q is already supplied by the host", ErrDuplicateCapability, capability)
			}
			if existing, exists := providers[capability]; exists {
				return nil, nil, fmt.Errorf("%w: %q is provided by both %q and %q", ErrDuplicateCapability, capability, existing, spec.Name)
			}
			providers[capability] = spec.Name
		}
	}

	pending := append([]ComponentSpec(nil), specs...)
	ordered := make([]ComponentSpec, 0, len(specs))
	for len(pending) > 0 {
		progressed := false
		next := pending[:0]
		for _, spec := range pending {
			if requirementsAvailable(spec.Requires, available) {
				ordered = append(ordered, spec)
				for _, capability := range spec.Provides {
					available[capability] = struct{}{}
				}
				progressed = true
				continue
			}
			next = append(next, spec)
		}
		if !progressed {
			return nil, nil, unresolvedDependencyError(next, available)
		}
		pending = next
	}
	return ordered, available, nil
}

func requirementsAvailable(requires []Capability, available map[Capability]struct{}) bool {
	for _, capability := range requires {
		if capability == "" {
			return false
		}
		if _, ok := available[capability]; !ok {
			return false
		}
	}
	return true
}

func unresolvedDependencyError(pending []ComponentSpec, available map[Capability]struct{}) error {
	for _, spec := range pending {
		for _, capability := range spec.Requires {
			if capability == "" {
				return fmt.Errorf("%w: component %q requires an empty capability", ErrInvalidComponent, spec.Name)
			}
			if _, ok := available[capability]; !ok {
				return fmt.Errorf("%w: component %q requires %q", ErrMissingCapability, spec.Name, capability)
			}
		}
	}
	return ErrMissingCapability
}
