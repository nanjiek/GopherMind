package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gophermind/internal/session/eventlog"
	"gophermind/internal/session/projection"
)

func TestProjectionCheckpointSurvivesStoreReopen(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))
	repo := NewSessionRepository(db)
	events := NewEventStore(db)
	session, err := repo.CreateSessionWithFirstMessage(context.Background(), "projection-user", "Projection", "hello", "projection-request")
	require.NoError(t, err)
	stream, err := events.ReadStream(context.Background(), eventlog.Scope{TenantID: eventlog.DefaultTenantID, UserID: "projection-user"}, session.ID, 0, 100)
	require.NoError(t, err)
	last := stream[len(stream)-1]

	store := NewProjectionStore(db)
	require.NoError(t, store.Save(context.Background(), projection.Checkpoint{
		ProjectionName: "recent-message-surface-v1", Shard: 0,
		LastEventID: &last.EventID, LastRecordedAt: &last.RecordedAt,
	}))

	reopened := NewProjectionStore(db)
	checkpoint, ok, err := reopened.Load(context.Background(), "recent-message-surface-v1", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, last.EventID, *checkpoint.LastEventID)
	require.Equal(t, last.RecordedAt.UTC(), checkpoint.LastRecordedAt.UTC())
}
