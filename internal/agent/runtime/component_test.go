package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestStartComponentsResolvesDependenciesBeforeStarting(t *testing.T) {
	var started []string
	components, err := StartComponents(context.Background(), Metadata{}, []Capability{"database"}, []ComponentSpec{
		{
			Name:     "consumer",
			Requires: []Capability{"database", "model"},
			Start: func(*Scope) error {
				started = append(started, "consumer")
				return nil
			},
		},
		{
			Name:     "model",
			Requires: []Capability{"database"},
			Provides: []Capability{"model"},
			Start: func(*Scope) error {
				started = append(started, "model")
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("StartComponents() error = %v", err)
	}
	t.Cleanup(func() { _ = components.Close(context.Background()) })
	if want := []string{"model", "consumer"}; !reflect.DeepEqual(started, want) {
		t.Fatalf("startup order = %v, want %v", started, want)
	}
	if !components.HasCapability("database") || !components.HasCapability("model") {
		t.Fatal("started components did not expose supplied and provided capabilities")
	}
}

func TestStartComponentsRollsBackWhenComponentFails(t *testing.T) {
	startErr := errors.New("consumer failed")
	var cleaned []string
	components, err := StartComponents(context.Background(), Metadata{}, nil, []ComponentSpec{
		{
			Name:     "provider",
			Provides: []Capability{"provider"},
			Start: func(scope *Scope) error {
				return scope.Defer(func(context.Context) error {
					cleaned = append(cleaned, "provider")
					return nil
				})
			},
		},
		{
			Name:     "consumer",
			Requires: []Capability{"provider"},
			Start: func(scope *Scope) error {
				if err := scope.Defer(func(context.Context) error {
					cleaned = append(cleaned, "consumer")
					return nil
				}); err != nil {
					return err
				}
				return startErr
			},
		},
	})
	if components != nil {
		t.Fatal("StartComponents() returned components after a failed startup")
	}
	if !errors.Is(err, startErr) {
		t.Fatalf("StartComponents() error = %v, want %v", err, startErr)
	}
	if want := []string{"consumer", "provider"}; !reflect.DeepEqual(cleaned, want) {
		t.Fatalf("cleanup order = %v, want %v", cleaned, want)
	}
}

func TestStartComponentsRejectsUnavailableDependencyBeforeStarting(t *testing.T) {
	started := false
	_, err := StartComponents(context.Background(), Metadata{}, nil, []ComponentSpec{{
		Name:     "consumer",
		Requires: []Capability{"missing"},
		Start: func(*Scope) error {
			started = true
			return nil
		},
	}})
	if !errors.Is(err, ErrMissingCapability) {
		t.Fatalf("StartComponents() error = %v, want ErrMissingCapability", err)
	}
	if started {
		t.Fatal("component started despite an unavailable dependency")
	}
}

func TestStartComponentsRejectsDuplicateProviders(t *testing.T) {
	_, err := StartComponents(context.Background(), Metadata{}, nil, []ComponentSpec{
		{Name: "first", Provides: []Capability{"shared"}, Start: func(*Scope) error { return nil }},
		{Name: "second", Provides: []Capability{"shared"}, Start: func(*Scope) error { return nil }},
	})
	if !errors.Is(err, ErrDuplicateCapability) {
		t.Fatalf("StartComponents() error = %v, want ErrDuplicateCapability", err)
	}
}
