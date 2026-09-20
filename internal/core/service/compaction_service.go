package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gophermind/internal/session/compaction"
	"gophermind/internal/session/eventlog"
)

var ErrCompactionNoNewEvents = errors.New("no new events to compact")
var ErrCompactionSnapshotTooLarge = errors.New("compaction snapshot exceeds bounded event limit")

const maxCompactionEvents = 1000

type CompactionStore interface {
	Load(context.Context, compaction.Scope) (compaction.Record, bool, error)
	Publish(context.Context, compaction.Record, int64) (compaction.Record, error)
}

type CompactionEventReader interface {
	CurrentSequence(context.Context, eventlog.Scope, string) (int64, error)
	ReadStream(context.Context, eventlog.Scope, string, int64, int) ([]eventlog.Event, error)
}

// CompactionService snapshots a committed Event Stream and publishes a
// structured summary by revision CAS. It never includes events appended after
// its snapshot watermark, so concurrent messages remain available afterwards.
type CompactionService struct {
	Events     CompactionEventReader
	Store      CompactionStore
	Summarizer compaction.Summarizer
	Budget     compaction.Budget
}

func (s *CompactionService) Compact(ctx context.Context, scope compaction.Scope) (compaction.Record, error) {
	if s == nil || s.Events == nil || s.Store == nil || s.Summarizer == nil {
		return compaction.Record{}, fmt.Errorf("compaction service requires events, store, and summarizer")
	}
	if err := s.Budget.Validate(); err != nil {
		return compaction.Record{}, err
	}
	previous, exists, err := s.Store.Load(ctx, scope)
	if err != nil {
		return compaction.Record{}, err
	}
	baseSeq, revision := int64(0), int64(0)
	var priorSummary json.RawMessage
	if exists {
		baseSeq, revision, priorSummary = previous.SourceSeq, previous.Revision, previous.Summary
	}
	eventScope := eventlog.Scope{TenantID: scope.TenantID, UserID: scope.UserID}
	watermark, err := s.Events.CurrentSequence(ctx, eventScope, scope.SessionID)
	if err != nil {
		return compaction.Record{}, err
	}
	if watermark <= baseSeq {
		return compaction.Record{}, ErrCompactionNoNewEvents
	}
	if watermark-baseSeq > maxCompactionEvents {
		return compaction.Record{}, ErrCompactionSnapshotTooLarge
	}
	events, err := s.Events.ReadStream(ctx, eventScope, scope.SessionID, baseSeq, maxCompactionEvents)
	if err != nil {
		return compaction.Record{}, err
	}
	inputEvents := make([]compaction.Event, 0, len(events))
	for _, event := range events {
		if event.StreamSeq > watermark {
			break
		}
		inputEvents = append(inputEvents, compaction.Event{StreamSeq: event.StreamSeq, EventType: event.EventType, Payload: append(json.RawMessage(nil), event.Payload...)})
	}
	if len(inputEvents) == 0 || inputEvents[len(inputEvents)-1].StreamSeq != watermark {
		return compaction.Record{}, fmt.Errorf("%w: event snapshot is incomplete", ErrCompactionSnapshotTooLarge)
	}
	input, err := compaction.BuildInput(scope, priorSummary, inputEvents, watermark, s.Budget)
	if err != nil {
		return compaction.Record{}, err
	}
	summary, err := s.Summarizer.Summarize(ctx, input)
	if err != nil {
		return compaction.Record{}, err
	}
	record := compaction.Record{Scope: scope, SourceSeq: watermark, Summary: append(json.RawMessage(nil), summary...), InputTokens: input.InputTokens, ReservedOutputTokens: input.ReservedOutputTokens, Revision: revision + 1}
	if err := compaction.ValidateRecord(record); err != nil {
		return compaction.Record{}, err
	}
	return s.Store.Publish(ctx, record, revision)
}
