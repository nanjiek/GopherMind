package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/session/eventlog"
)

func TestSessionWritesAppendOrderedEventsAndReplay(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))
	repo := NewSessionRepository(db)
	store := NewEventStore(db)
	scope := eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: "user-a"}

	session, err := repo.CreateSessionWithFirstMessage(context.Background(), "user-a", "First", "hello", "request-1")
	require.NoError(t, err)
	replayed, err := repo.CreateSessionWithFirstMessage(context.Background(), "user-a", "ignored", "ignored", "request-1")
	require.NoError(t, err)
	require.Equal(t, session.ID, replayed.ID)

	require.NoError(t, repo.AppendUserMessage(context.Background(), "user-a", session.ID, "follow-up", "request-2"))
	require.NoError(t, repo.AppendUserMessage(context.Background(), "user-a", session.ID, "ignored", "request-2"))
	require.NoError(t, repo.AppendAssistantMessage(context.Background(), "user-a", session.ID, "answer", "request-2", "test", "test-model"))
	require.NoError(t, repo.AppendAssistantMessage(context.Background(), "user-a", session.ID, "ignored", "request-2", "test", "test-model"))

	events, err := store.ReadStream(context.Background(), scope, session.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 4)
	require.Equal(t, []int64{1, 2, 3, 4}, []int64{events[0].StreamSeq, events[1].StreamSeq, events[2].StreamSeq, events[3].StreamSeq})
	require.Equal(t, eventlog.SessionCreatedV1, events[0].EventType)
	for _, event := range events[1:] {
		require.Equal(t, eventlog.MessageAppendedV1, event.EventType)
	}

	messages, err := repo.ListMessages(context.Background(), "user-a", session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 3)
	_, err = store.ReadStream(context.Background(), eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: "user-b"}, session.ID, 0, 100)
	require.ErrorIs(t, err, eventlog.ErrScopeMismatch)
	patientID := "other-patient"
	_, err = store.ReadStream(context.Background(), eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: "user-a", PatientID: &patientID}, session.ID, 0, 100)
	require.ErrorIs(t, err, eventlog.ErrScopeMismatch)
}

func TestConcurrentAppendsHaveContinuousSequenceAndSameKeyIsIdempotent(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))
	repo := NewSessionRepository(db)
	store := NewEventStore(db)
	scope := eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: "user-a"}
	session, err := repo.CreateSessionWithFirstMessage(context.Background(), "user-a", "Concurrent", "start", "root-request")
	require.NoError(t, err)

	const uniqueAppends = 8
	errorsByCall := make(chan error, uniqueAppends+4)
	var wg sync.WaitGroup
	for i := 0; i < uniqueAppends; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errorsByCall <- repo.AppendUserMessage(context.Background(), "user-a", session.ID, fmt.Sprintf("message-%d", index), fmt.Sprintf("request-%d", index))
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsByCall <- repo.AppendUserMessage(context.Background(), "user-a", session.ID, "same", "same-request")
		}()
	}
	wg.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		require.NoError(t, err)
	}

	events, err := store.ReadStream(context.Background(), scope, session.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 2+uniqueAppends+1)
	for index, event := range events {
		require.Equal(t, int64(index+1), event.StreamSeq)
	}
	messages, err := repo.ListMessages(context.Background(), "user-a", session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 1+uniqueAppends+1)
}

func TestEventStoreReturnsCommittedEventForIdempotentRetry(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))
	repo := NewSessionRepository(db)
	store := NewEventStore(db)
	session, err := repo.CreateSessionWithFirstMessage(context.Background(), "user-a", "Event", "start", "request-1")
	require.NoError(t, err)
	request := eventlog.AppendRequest{
		Scope:     eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: "user-a"},
		SessionID: session.ID, EventType: eventlog.GenerationStartedV1, SchemaVersion: 1,
		RequestID: "generation-1", IdempotencyKey: "generation-1:started",
		CorrelationID: "generation-1", ActorType: "system", ActorID: "query-service",
		Payload: map[string]any{"generation_id": "generation-1", "workflow_version": "go-v1"},
	}

	first, err := store.Append(context.Background(), request)
	require.NoError(t, err)
	require.False(t, first.Idempotent)
	second, err := store.Append(context.Background(), request)
	require.NoError(t, err)
	require.True(t, second.Idempotent)
	require.Equal(t, first.Event.EventID, second.Event.EventID)
	require.Equal(t, first.Revision, second.Revision)
}
