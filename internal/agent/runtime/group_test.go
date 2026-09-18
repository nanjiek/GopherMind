package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskGroupFirstErrorCancelsOtherTasks(t *testing.T) {
	group, err := NewTaskGroup(context.Background(), 2)
	if err != nil {
		t.Fatalf("NewTaskGroup() error = %v", err)
	}
	firstErr := errors.New("first task failed")
	canceled := make(chan struct{})
	started := make(chan struct{})
	if err := group.Go(func(context.Context) error {
		<-started
		return firstErr
	}); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	if err := group.Go(func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return nil
		case <-time.After(time.Second):
			return errors.New("task was not canceled")
		}
	}); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	if err := group.Wait(); !errors.Is(err, firstErr) {
		t.Fatalf("Wait() error = %v, want %v", err, firstErr)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("other task did not observe cancellation")
	}
}

func TestTaskGroupRespectsConcurrencyLimit(t *testing.T) {
	const (
		limit = 3
		tasks = 18
	)
	group, err := NewTaskGroup(context.Background(), limit)
	if err != nil {
		t.Fatalf("NewTaskGroup() error = %v", err)
	}
	var active, peak int32
	for range tasks {
		if err := group.Go(func(context.Context) error {
			current := atomic.AddInt32(&active, 1)
			for {
				observed := atomic.LoadInt32(&peak)
				if current <= observed || atomic.CompareAndSwapInt32(&peak, observed, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			return nil
		}); err != nil {
			t.Fatalf("Go() error = %v", err)
		}
	}
	if err := group.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if peak > limit {
		t.Fatalf("peak concurrency = %d, limit = %d", peak, limit)
	}
	if peak != limit {
		t.Fatalf("peak concurrency = %d, want limit %d to be exercised", peak, limit)
	}
}
