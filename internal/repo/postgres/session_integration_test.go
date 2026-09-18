package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionRepositoryTransactionsConstraintsAndOwnership(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))
	repo := NewSessionRepository(db)

	session, err := repo.CreateSessionWithFirstMessage(context.Background(), "user-a", "First", "hello", "request-1")
	require.NoError(t, err)

	_, err = repo.GetSession(context.Background(), "user-b", session.ID)
	require.True(t, IsNotFoundError(err))
	err = repo.AppendUserMessage(context.Background(), "user-b", session.ID, "unauthorized", "request-2")
	require.True(t, IsNotFoundError(err))

	messages, err := repo.ListMessages(context.Background(), "user-a", session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	_, err = repo.CreateSessionWithFirstMessage(context.Background(), "user-a", "Duplicate", "duplicate", "request-1")
	require.Error(t, err)
	sessions, err := repo.ListSessions(context.Background(), "user-a", 10)
	require.NoError(t, err)
	require.Len(t, sessions, 1, "failed message insert must roll back its session")

	require.NoError(t, repo.AppendUserMessage(context.Background(), "user-a", session.ID, "follow-up", "request-2"))
	require.Error(t, repo.AppendUserMessage(context.Background(), "user-a", session.ID, "duplicate", "request-2"))
	messages, err = repo.ListMessages(context.Background(), "user-a", session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2, "unique request constraint must prevent a duplicate message")
}
