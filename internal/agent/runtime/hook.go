package runtime

import (
	"context"
	"errors"
	"sync"
)

// HookStage identifies a lifecycle point available to runtime extensions.
type HookStage string

const (
	HookBeforeAction      HookStage = "before_action"
	HookAfterAction       HookStage = "after_action"
	HookBeforeObservation HookStage = "before_observation"
	HookAfterObservation  HookStage = "after_observation"
	HookBeforeTransition  HookStage = "before_transition"
	HookAfterTransition   HookStage = "after_transition"
)

// HookEvent contains safe protocol metadata. It never contains hidden model
// reasoning and may be extended by later persistence/observability nodes.
type HookEvent struct {
	Stage       HookStage
	Run         RunSnapshot
	Action      *Action
	Observation *Observation
}

// Hook wraps runtime work. Before can reject execution; After always runs for
// hooks whose Before succeeded, in reverse registration order.
type Hook interface {
	Before(context.Context, HookEvent) error
	After(context.Context, HookEvent, error) error
}

// HookFunc adapts simple functions to Hook.
type HookFunc struct {
	BeforeFunc func(context.Context, HookEvent) error
	AfterFunc  func(context.Context, HookEvent, error) error
}

func (h HookFunc) Before(ctx context.Context, event HookEvent) error {
	if h.BeforeFunc == nil {
		return nil
	}
	return h.BeforeFunc(ctx, event)
}

func (h HookFunc) After(ctx context.Context, event HookEvent, result error) error {
	if h.AfterFunc == nil {
		return nil
	}
	return h.AfterFunc(ctx, event, result)
}

// Pipeline is a concurrency-safe, ordered Hook registry.
type Pipeline struct {
	mu     sync.RWMutex
	nextID uint64
	hooks  []hookEntry
}

type hookEntry struct {
	id   uint64
	hook Hook
}

// Register adds hook and binds its removal to scope when one is supplied.
func (p *Pipeline) Register(scope *Scope, hook Hook) error {
	if hook == nil {
		return errors.New("runtime hook is nil")
	}
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	p.hooks = append(p.hooks, hookEntry{id: id, hook: hook})
	p.mu.Unlock()
	if scope == nil {
		return nil
	}
	if err := scope.Defer(func(context.Context) error {
		p.remove(id)
		return nil
	}); err != nil {
		p.remove(id)
		return err
	}
	return nil
}

// Execute invokes Before in registration order and After in reverse order.
func (p *Pipeline) Execute(ctx context.Context, event HookEvent, next func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.RLock()
	entries := append([]hookEntry(nil), p.hooks...)
	p.mu.RUnlock()
	entered := make([]Hook, 0, len(entries))
	var result error
	for _, entry := range entries {
		hook := entry.hook
		if result = hook.Before(ctx, event); result != nil {
			break
		}
		entered = append(entered, hook)
	}
	if result == nil && next != nil {
		result = next(ctx)
	}
	for index := len(entered) - 1; index >= 0; index-- {
		result = errors.Join(result, entered[index].After(ctx, event, result))
	}
	return result
}

func (p *Pipeline) remove(id uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for position, current := range p.hooks {
		if current.id == id {
			p.hooks = append(p.hooks[:position], p.hooks[position+1:]...)
			return
		}
	}
}
