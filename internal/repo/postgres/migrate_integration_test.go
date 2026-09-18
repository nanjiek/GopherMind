package postgres

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"gophermind/internal/agent/runtime"
)

func TestMigrationsInitializeEmptySchemaAndAreRepeatable(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()

	require.NoError(t, ApplyMigrations(context.Background(), db))
	require.NoError(t, ApplyMigrations(context.Background(), db))
	require.NoError(t, VerifySchema(context.Background(), db))

	var versions int64
	require.NoError(t, db.Raw("SELECT count(*) FROM schema_migrations").Scan(&versions).Error)
	require.Equal(t, int64(2), versions)

	var tables int64
	require.NoError(t, db.Raw(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema()
		  AND table_name IN ('sessions', 'messages', 'clinical_events', 'projection_checkpoints', 'outbox_messages')
	`).Scan(&tables).Error)
	require.Equal(t, int64(5), tables)
}

func TestWorkflowCheckpointStoreCreatesLoadsAndUsesCAS(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))

	store := NewWorkflowCheckpointStore(db)
	scope := runtime.Metadata{
		TenantID: "tenant-a", UserID: "user-a", PatientID: "patient-a",
		SessionID: "c96e73cf-345e-4eb6-8a05-b9f519799caa", RequestID: "request-a",
	}
	first := runtime.Checkpoint{
		RunID: "1d9e6479-e9d0-4906-9da8-8b2cd3d7c3cb", Scope: scope,
		WorkflowID: "fixed-workflow", WorkflowVersion: "v1", Status: runtime.RunRunning,
		CurrentNode: "evidence", Revision: 1, State: []byte(`{"evidence":"pending"}`),
	}
	created, err := store.Create(context.Background(), first)
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Revision)

	loaded, err := store.Load(context.Background(), scope, first.RunID)
	require.NoError(t, err)
	require.Equal(t, first.State, loaded.State)
	require.Equal(t, first.CurrentNode, loaded.CurrentNode)

	next := loaded
	next.Revision = 2
	next.CurrentNode = "safety"
	next.State = []byte(`{"evidence":"ready"}`)
	saved, err := store.Save(context.Background(), 1, next)
	require.NoError(t, err)
	require.Equal(t, int64(2), saved.Revision)

	_, err = store.Save(context.Background(), 1, next)
	require.ErrorIs(t, err, runtime.ErrCheckpointConflict)
	wrongScope := scope
	wrongScope.UserID = "user-b"
	_, err = store.Load(context.Background(), wrongScope, first.RunID)
	require.ErrorIs(t, err, runtime.ErrCheckpointNotFound)

	var historyCount int64
	require.NoError(t, db.Raw("SELECT count(*) FROM agent_run_checkpoints WHERE run_id = ?", first.RunID).Scan(&historyCount).Error)
	require.Equal(t, int64(2), historyCount)
}

func TestMigrationsRejectUnknownNonEmptySchema(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, db.Exec("CREATE TABLE foreign_table (id BIGINT PRIMARY KEY)").Error)

	err := ApplyMigrations(context.Background(), db)
	require.ErrorContains(t, err, "refusing to initialize non-empty unmanaged schema")
}

func TestVerifySchemaRejectsMissingRequiredTable(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))
	require.NoError(t, db.Exec("DROP TABLE projection_checkpoints").Error)

	err := VerifySchema(context.Background(), db)
	require.ErrorContains(t, err, "database schema is incomplete")
}

func isolatedTestSchema(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is required for PostgreSQL integration tests")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	schema := "test_" + regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(uuid.NewString(), "")
	require.NoError(t, admin.Exec(fmt.Sprintf(`CREATE SCHEMA %q`, schema)).Error)

	testURL, err := url.Parse(dsn)
	require.NoError(t, err)
	query := testURL.Query()
	query.Set("search_path", schema)
	testURL.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(testURL.String()), &gorm.Config{})
	require.NoError(t, err)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, admin.WithContext(ctx).Exec(fmt.Sprintf(`DROP SCHEMA %q CASCADE`, schema)).Error)
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
		if pool, err := admin.DB(); err == nil {
			_ = pool.Close()
		}
	}
	return db, cleanup
}
