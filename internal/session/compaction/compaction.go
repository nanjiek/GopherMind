// Package compaction defines the bounded, versioned conversation-compaction
// contract. It is intentionally separate from cached legacy summaries.
package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

var (
	ErrInvalidBudget = errors.New("invalid compaction token budget")
	ErrInvalidRecord = errors.New("invalid compaction record")
)

type Scope struct {
	TenantID  string
	UserID    string
	SessionID string
}

type Budget struct {
	MaxInputTokens      int
	ReserveOutputTokens int
}

func (b Budget) Validate() error {
	if b.MaxInputTokens <= 0 || b.ReserveOutputTokens <= 0 || b.ReserveOutputTokens >= b.MaxInputTokens {
		return ErrInvalidBudget
	}
	return nil
}

type Event struct {
	StreamSeq int64
	EventType string
	Payload   json.RawMessage
}

// Input is the entire bounded prompt material allowed to reach a summarizer.
// Events after SourceSeq are deliberately absent and remain visible after the
// compacted summary rather than being dropped during a concurrent append.
type Input struct {
	Scope                Scope
	PreviousSummary      json.RawMessage
	Events               []Event
	SourceSeq            int64
	InputTokens          int
	ReservedOutputTokens int
}

type Record struct {
	Scope                Scope
	SourceSeq            int64
	Summary              json.RawMessage
	InputTokens          int
	ReservedOutputTokens int
	Revision             int64
}

type Summarizer interface {
	Summarize(context.Context, Input) (json.RawMessage, error)
}

// EstimateTokens is a deterministic conservative planning estimate. Provider
// accounting is not trusted for admission control because it arrives after
// the request; a configured production summarizer may report finer usage.
func EstimateTokens(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	return (utf8.RuneCount(raw) + 3) / 4
}

func BuildInput(scope Scope, previous json.RawMessage, events []Event, sourceSeq int64, budget Budget) (Input, error) {
	if scope.TenantID == "" || scope.UserID == "" || scope.SessionID == "" || sourceSeq < 0 {
		return Input{}, ErrInvalidRecord
	}
	if err := budget.Validate(); err != nil {
		return Input{}, err
	}
	encoded, err := json.Marshal(struct {
		Previous json.RawMessage `json:"previous_summary,omitempty"`
		Events   []Event         `json:"events"`
	}{Previous: previous, Events: events})
	if err != nil {
		return Input{}, fmt.Errorf("encode compaction input: %w", err)
	}
	tokens := EstimateTokens(encoded)
	if tokens+budget.ReserveOutputTokens > budget.MaxInputTokens {
		return Input{}, fmt.Errorf("%w: input plus output reserve exceeds budget", ErrInvalidBudget)
	}
	return Input{Scope: scope, PreviousSummary: append(json.RawMessage(nil), previous...), Events: cloneEvents(events), SourceSeq: sourceSeq, InputTokens: tokens, ReservedOutputTokens: budget.ReserveOutputTokens}, nil
}

func ValidateRecord(record Record) error {
	if record.Scope.TenantID == "" || record.Scope.UserID == "" || record.Scope.SessionID == "" || record.SourceSeq < 0 || record.InputTokens < 0 || record.ReservedOutputTokens <= 0 || record.Revision <= 0 {
		return ErrInvalidRecord
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(record.Summary, &object) != nil || object == nil {
		return fmt.Errorf("%w: summary must be a JSON object", ErrInvalidRecord)
	}
	return nil
}

func cloneEvents(events []Event) []Event {
	cloned := make([]Event, len(events))
	for i := range events {
		cloned[i] = events[i]
		cloned[i].Payload = append(json.RawMessage(nil), events[i].Payload...)
	}
	return cloned
}
