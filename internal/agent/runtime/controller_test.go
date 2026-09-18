package runtime

import (
	"context"
	"testing"
)

func TestControllerRunsHooksAroundMutation(t *testing.T) {
	run := newTestRun(t, 1)
	pipeline := &Pipeline{}
	var calls int
	if err := pipeline.Register(nil, HookFunc{BeforeFunc: func(context.Context, HookEvent) error { calls++; return nil }}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	controller := Controller{Run: run, Hooks: pipeline}
	if err := controller.Transition(context.Background(), RunLoadingContext); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook calls = %d, want 1", calls)
	}
}
