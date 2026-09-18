// Package runtime contains the small lifecycle primitives shared by future
// agent runtime components. It deliberately does not define run state,
// persistence, actions, observations, or hooks.
package runtime

import (
	"context"
	"errors"
	"sync"
)

var ErrScopeClosed = errors.New("runtime scope is closed")

// Metadata is trusted request-scope identity metadata. It is propagated in a
// context owned by Scope so downstream runtime components do not need to pass
// these fields separately.
//
// Authorization is intentionally not implemented here. Later policy layers
// must validate this metadata at their own trust boundaries.
type Metadata struct {
	TenantID  string
	UserID    string
	PatientID string
	SessionID string
	RunID     string
	RequestID string
	TraceID   string
}

type metadataKey struct{}

// MetadataFromContext returns request metadata installed by NewScope.
func MetadataFromContext(ctx context.Context) (Metadata, bool) {
	metadata, ok := ctx.Value(metadataKey{}).(Metadata)
	return metadata, ok
}

// Disposer releases a resource registered with a Scope.
type Disposer func(context.Context) error

// Setup creates one scoped resource and returns its cleanup function.
type Setup func(context.Context) (Disposer, error)

// Scope owns a cancelable request context and its registered resources.
// Resources are released in reverse registration order. Close is safe to call
// concurrently and releases each registered resource at most once.
type Scope struct {
	ctx      context.Context
	cancel   context.CancelFunc
	metadata Metadata

	mu           sync.Mutex
	disposers    []Disposer
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error
}

// NewScope creates a request scope whose cancellation follows parent.
func NewScope(parent context.Context, metadata Metadata) *Scope {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	ctx = context.WithValue(ctx, metadataKey{}, metadata)
	return &Scope{
		ctx:       ctx,
		cancel:    cancel,
		metadata:  metadata,
		closeDone: make(chan struct{}),
	}
}

// Context returns the cancelable context carrying this scope's metadata.
func (s *Scope) Context() context.Context { return s.ctx }

// Metadata returns the immutable metadata with which the scope was created.
func (s *Scope) Metadata() Metadata { return s.metadata }

// Defer registers a resource cleanup. A closed scope never accepts new
// disposers.
func (s *Scope) Defer(disposer Disposer) error {
	if disposer == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closeStarted {
		return ErrScopeClosed
	}
	s.disposers = append(s.disposers, disposer)
	return nil
}

// Effect creates a scoped resource and registers its disposer. If Close races
// with setup, the just-created resource is immediately released and the call
// reports ErrScopeClosed.
func (s *Scope) Effect(setup Setup) error {
	if setup == nil {
		return nil
	}

	s.mu.Lock()
	if s.closeStarted {
		s.mu.Unlock()
		return ErrScopeClosed
	}
	s.mu.Unlock()

	disposer, err := setup(s.ctx)
	if err != nil {
		return err
	}
	if disposer == nil {
		return nil
	}

	s.mu.Lock()
	if !s.closeStarted {
		s.disposers = append(s.disposers, disposer)
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	return errors.Join(ErrScopeClosed, disposer(context.Background()))
}

// Close stops scoped work, then releases registered resources in LIFO order.
// A later Close waits for the first close and returns its result without
// running any disposer again.
func (s *Scope) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closeStarted {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closeStarted = true
	disposers := s.disposers
	s.disposers = nil
	s.cancel()
	s.mu.Unlock()

	var closeErr error
	for index := len(disposers) - 1; index >= 0; index-- {
		closeErr = errors.Join(closeErr, disposers[index](ctx))
	}

	s.mu.Lock()
	s.closeErr = closeErr
	close(s.closeDone)
	s.mu.Unlock()
	return closeErr
}

// Initialize creates a scope and runs initialize as one lifecycle
// transaction. If initialization fails, all resources registered before the
// error are rolled back before the error is returned.
func Initialize(parent context.Context, metadata Metadata, initialize func(*Scope) error) (*Scope, error) {
	scope := NewScope(parent, metadata)
	if initialize == nil {
		return scope, nil
	}
	if err := initialize(scope); err != nil {
		return nil, errors.Join(err, scope.Close(context.Background()))
	}
	return scope, nil
}
