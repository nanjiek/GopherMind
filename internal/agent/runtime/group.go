package runtime

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrInvalidConcurrency = errors.New("runtime task group concurrency must be positive")
	ErrTaskGroupClosed    = errors.New("runtime task group is closed")
)

// Task is a unit of concurrently executed runtime work.
type Task func(context.Context) error

// TaskGroup runs accepted tasks with a fixed upper bound on active tasks. The
// first task error cancels the group's context, so running and queued tasks can
// stop promptly when they observe ctx.Done().
type TaskGroup struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	sem    chan struct{}

	mu     sync.Mutex
	closed bool
	first  error
	wg     sync.WaitGroup
}

// NewTaskGroup creates a task group derived from parent.
func NewTaskGroup(parent context.Context, limit int) (*TaskGroup, error) {
	if limit <= 0 {
		return nil, ErrInvalidConcurrency
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &TaskGroup{
		ctx:    ctx,
		cancel: cancel,
		sem:    make(chan struct{}, limit),
	}, nil
}

// Context returns the context shared by group tasks.
func (g *TaskGroup) Context() context.Context { return g.ctx }

// Go accepts a task. It returns promptly; accepted tasks wait for a bounded
// execution slot and will not start if the group is canceled first.
func (g *TaskGroup) Go(task Task) error {
	if task == nil {
		return errors.New("runtime task is nil")
	}

	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return ErrTaskGroupClosed
	}
	g.wg.Add(1)
	g.mu.Unlock()

	go func() {
		defer g.wg.Done()
		select {
		case <-g.ctx.Done():
			return
		case g.sem <- struct{}{}:
		}
		defer func() { <-g.sem }()

		if err := task(g.ctx); err != nil {
			g.mu.Lock()
			if g.first == nil {
				g.first = err
				g.cancel(err)
			}
			g.mu.Unlock()
		}
	}()
	return nil
}

// Wait prevents further task submission and waits for all accepted tasks. It
// returns the first task error, if any. Successful completion cancels the
// context to release callers holding it after the group is no longer usable.
func (g *TaskGroup) Wait() error {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()

	g.wg.Wait()
	g.mu.Lock()
	err := g.first
	g.mu.Unlock()
	g.cancel(nil)
	return err
}
