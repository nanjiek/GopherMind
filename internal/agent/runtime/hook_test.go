package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPipelineUsesOnionOrder(t *testing.T) {
	var calls []string
	pipeline := &Pipeline{}
	for _, name := range []string{"first", "second"} {
		name := name
		if err := pipeline.Register(nil, HookFunc{
			BeforeFunc: func(context.Context, HookEvent) error { calls = append(calls, name+":before"); return nil },
			AfterFunc:  func(context.Context, HookEvent, error) error { calls = append(calls, name+":after"); return nil },
		}); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	}
	if err := pipeline.Execute(context.Background(), HookEvent{Stage: HookBeforeAction}, func(context.Context) error {
		calls = append(calls, "next")
		return nil
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := []string{"first:before", "second:before", "next", "second:after", "first:after"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestPipelineRejectsAndStillUnwindsEnteredHooks(t *testing.T) {
	blocked := errors.New("blocked")
	var nextCalled bool
	var afterCalled bool
	pipeline := &Pipeline{}
	_ = pipeline.Register(nil, HookFunc{AfterFunc: func(context.Context, HookEvent, error) error { afterCalled = true; return nil }})
	_ = pipeline.Register(nil, HookFunc{BeforeFunc: func(context.Context, HookEvent) error { return blocked }})
	err := pipeline.Execute(context.Background(), HookEvent{}, func(context.Context) error { nextCalled = true; return nil })
	if !errors.Is(err, blocked) || nextCalled || !afterCalled {
		t.Fatalf("Execute() = %v, next=%v, after=%v", err, nextCalled, afterCalled)
	}
}

func TestPipelineRegistrationIsReleasedWithScope(t *testing.T) {
	scope := NewScope(context.Background(), Metadata{})
	pipeline := &Pipeline{}
	var calls int
	if err := pipeline.Register(scope, HookFunc{BeforeFunc: func(context.Context, HookEvent) error { calls++; return nil }}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := scope.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := pipeline.Execute(context.Background(), HookEvent{}, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("released hook calls = %d, want 0", calls)
	}
}
