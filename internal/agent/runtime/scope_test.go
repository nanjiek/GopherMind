package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestScopeMetadataAndParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	metadata := Metadata{TenantID: "tenant-1", UserID: "user-1", PatientID: "patient-1", SessionID: "session-1", RunID: "run-1", RequestID: "request-1", TraceID: "trace-1"}
	scope := NewScope(parent, metadata)
	t.Cleanup(func() { _ = scope.Close(context.Background()) })

	got, ok := MetadataFromContext(scope.Context())
	if !ok || got != metadata {
		t.Fatalf("MetadataFromContext() = %#v, %v; want %#v, true", got, ok, metadata)
	}

	cancel()
	select {
	case <-scope.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("scope context did not inherit parent cancellation")
	}
}

func TestScopeCloseUsesLIFOOrder(t *testing.T) {
	scope := NewScope(context.Background(), Metadata{})
	var (
		mu    sync.Mutex
		order []int
	)
	for _, value := range []int{1, 2, 3} {
		value := value
		if err := scope.Defer(func(context.Context) error {
			mu.Lock()
			order = append(order, value)
			mu.Unlock()
			return nil
		}); err != nil {
			t.Fatalf("Defer() error = %v", err)
		}
	}

	if err := scope.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if want := []int{3, 2, 1}; !reflect.DeepEqual(order, want) {
		t.Fatalf("cleanup order = %v, want %v", order, want)
	}
}

func TestInitializeRollsBackPartialSetup(t *testing.T) {
	initializeErr := errors.New("second resource failed")
	var order []string
	_, err := Initialize(context.Background(), Metadata{}, func(scope *Scope) error {
		if err := scope.Effect(func(context.Context) (Disposer, error) {
			return func(context.Context) error {
				order = append(order, "first")
				return nil
			}, nil
		}); err != nil {
			return err
		}
		if err := scope.Effect(func(context.Context) (Disposer, error) {
			return func(context.Context) error {
				order = append(order, "second")
				return nil
			}, nil
		}); err != nil {
			return err
		}
		return initializeErr
	})
	if !errors.Is(err, initializeErr) {
		t.Fatalf("Initialize() error = %v, want %v", err, initializeErr)
	}
	if want := []string{"second", "first"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("rollback order = %v, want %v", order, want)
	}
}

func TestScopeCloseIsIdempotent(t *testing.T) {
	scope := NewScope(context.Background(), Metadata{})
	var calls int
	if err := scope.Defer(func(context.Context) error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("Defer() error = %v", err)
	}
	if err := scope.Close(context.Background()); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := scope.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("disposer calls = %d, want 1", calls)
	}
}
