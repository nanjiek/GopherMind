package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/session/compaction"
	"gophermind/internal/session/eventlog"
)

type compactionTestEvents struct {
	watermark int64
	events    []eventlog.Event
}

func (e compactionTestEvents) CurrentSequence(context.Context, eventlog.Scope, string) (int64, error) {
	return e.watermark, nil
}
func (e compactionTestEvents) ReadStream(context.Context, eventlog.Scope, string, int64, int) ([]eventlog.Event, error) {
	return append([]eventlog.Event(nil), e.events...), nil
}

type compactionTestStore struct {
	record   compaction.Record
	exists   bool
	expected int64
}

func (s *compactionTestStore) Load(context.Context, compaction.Scope) (compaction.Record, bool, error) {
	return s.record, s.exists, nil
}
func (s *compactionTestStore) Publish(_ context.Context, record compaction.Record, expected int64) (compaction.Record, error) {
	s.record, s.exists, s.expected = record, true, expected
	return record, nil
}

type compactionTestSummarizer struct{ input compaction.Input }

func (s *compactionTestSummarizer) Summarize(_ context.Context, input compaction.Input) (json.RawMessage, error) {
	s.input = input
	return json.RawMessage(`{"topics":["reviewed"],"facts":[]}`), nil
}

func TestCompactionServicePublishesVersionedSnapshotAndLeavesLaterEventsOut(t *testing.T) {
	store := &compactionTestStore{record: compaction.Record{Scope: compaction.Scope{TenantID: "t", UserID: "u", SessionID: "s"}, SourceSeq: 2, Summary: json.RawMessage(`{"topics":["old"]}`), InputTokens: 2, ReservedOutputTokens: 10, Revision: 1}, exists: true}
	summarizer := &compactionTestSummarizer{}
	service := &CompactionService{Events: compactionTestEvents{watermark: 4, events: []eventlog.Event{{StreamSeq: 3, EventType: eventlog.MessageAppendedV1, Payload: json.RawMessage(`{"content":"three"}`)}, {StreamSeq: 4, EventType: eventlog.MessageAppendedV1, Payload: json.RawMessage(`{"content":"four"}`)}, {StreamSeq: 5, EventType: eventlog.MessageAppendedV1, Payload: json.RawMessage(`{"content":"later"}`)}}}, Store: store, Summarizer: summarizer, Budget: compaction.Budget{MaxInputTokens: 200, ReserveOutputTokens: 20}}

	record, err := service.Compact(context.Background(), compaction.Scope{TenantID: "t", UserID: "u", SessionID: "s"})
	require.NoError(t, err)
	require.Equal(t, int64(4), record.SourceSeq)
	require.Equal(t, int64(2), record.Revision)
	require.Equal(t, int64(1), store.expected)
	require.Len(t, summarizer.input.Events, 2)
	require.Equal(t, int64(4), summarizer.input.Events[1].StreamSeq)
}

func TestCompactionServiceDoesNotRepublishWithoutNewEvents(t *testing.T) {
	store := &compactionTestStore{record: compaction.Record{Scope: compaction.Scope{TenantID: "t", UserID: "u", SessionID: "s"}, SourceSeq: 4, Summary: json.RawMessage(`{"topics":[]}`), InputTokens: 1, ReservedOutputTokens: 10, Revision: 1}, exists: true}
	service := &CompactionService{Events: compactionTestEvents{watermark: 4}, Store: store, Summarizer: &compactionTestSummarizer{}, Budget: compaction.Budget{MaxInputTokens: 100, ReserveOutputTokens: 10}}
	_, err := service.Compact(context.Background(), compaction.Scope{TenantID: "t", UserID: "u", SessionID: "s"})
	require.ErrorIs(t, err, ErrCompactionNoNewEvents)
}

func TestCompactionServiceRejectsUnboundedSnapshotBeforeReadingEvents(t *testing.T) {
	service := &CompactionService{Events: compactionTestEvents{watermark: maxCompactionEvents + 1}, Store: &compactionTestStore{}, Summarizer: &compactionTestSummarizer{}, Budget: compaction.Budget{MaxInputTokens: 100, ReserveOutputTokens: 10}}
	_, err := service.Compact(context.Background(), compaction.Scope{TenantID: "t", UserID: "u", SessionID: "s"})
	require.ErrorIs(t, err, ErrCompactionSnapshotTooLarge)
}
