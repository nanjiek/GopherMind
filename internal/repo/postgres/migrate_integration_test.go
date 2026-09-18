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
	require.Equal(t, int64(5), versions)

	var tables int64
	require.NoError(t, db.Raw(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema()
		  AND table_name IN ('sessions', 'messages', 'clinical_events', 'projection_checkpoints', 'outbox_messages')
	`).Scan(&tables).Error)
	require.Equal(t, int64(5), tables)
}

func TestTaskDAGStoreCreatesLoadsAndScopesStaticGraph(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))

	scope := runtime.Metadata{TenantID: "tenant-a", UserID: "user-a", PatientID: "patient-a", SessionID: "c96e73cf-345e-4eb6-8a05-b9f519799caa"}
	require.NoError(t, db.Create(&SessionModel{ID: scope.SessionID, UserID: scope.UserID, Title: "task DAG test"}).Error)
	checkpointStore := NewWorkflowCheckpointStore(db)
	runID := "96c4f4d6-6ab8-4b5f-b1a4-9266a37eeb6b"
	_, err := checkpointStore.Create(context.Background(), runtime.Checkpoint{
		RunID: runID, Scope: scope, WorkflowID: "fixed-workflow", WorkflowVersion: "v1", Status: runtime.RunRunning,
		CurrentNode: "evidence", Revision: 1, State: []byte(`{"state":"running"}`),
	})
	require.NoError(t, err)

	store := NewTaskDAGStore(db)
	created, err := store.Create(context.Background(), runtime.TaskDAG{RunID: runID, Scope: scope, Tasks: []runtime.AgentTask{
		{TaskID: "08d0d955-0ab9-4e4f-bf98-3266f837a15c", Type: "intake", OwnerAgentID: "intake-agent", IdempotencyKey: "intake", Revision: 1, Deadline: time.Now().Add(time.Hour)},
		{TaskID: "5d9a7ed8-d5ea-4d97-bfc7-87dbd25bd9c0", Type: "evidence", OwnerAgentID: "evidence-agent", IdempotencyKey: "evidence", BlockedBy: []string{"08d0d955-0ab9-4e4f-bf98-3266f837a15c"}, Revision: 1, Deadline: time.Now().Add(time.Hour)},
	}})
	require.NoError(t, err)
	require.Equal(t, runtime.TaskReady, created.Tasks[0].Status)
	require.Equal(t, runtime.TaskBlocked, created.Tasks[1].Status)

	loaded, err := store.Load(context.Background(), scope, runID)
	require.NoError(t, err)
	require.Len(t, loaded.Tasks, 2)
	wrongScope := scope
	wrongScope.UserID = "user-b"
	_, err = store.Load(context.Background(), wrongScope, runID)
	require.ErrorIs(t, err, runtime.ErrTaskDAGNotFound)
}

func TestDurableTaskAndMailboxUseCASFencingAndSeparateCompletion(t *testing.T) {
	db, cleanup := isolatedTestSchema(t)
	defer cleanup()
	require.NoError(t, ApplyMigrations(context.Background(), db))

	scope := runtime.Metadata{TenantID: "tenant-a", UserID: "user-a", PatientID: "patient-a", SessionID: "c96e73cf-345e-4eb6-8a05-b9f519799caa"}
	require.NoError(t, db.Create(&SessionModel{ID: scope.SessionID, UserID: scope.UserID, Title: "coordination test"}).Error)
	runID := "96c4f4d6-6ab8-4b5f-b1a4-9266a37eeb6b"
	_, err := NewWorkflowCheckpointStore(db).Create(context.Background(), runtime.Checkpoint{RunID: runID, Scope: scope, WorkflowID: "fixed-workflow", WorkflowVersion: "v1", Status: runtime.RunRunning, CurrentNode: "evidence", Revision: 1, State: []byte(`{"state":"running"}`)})
	require.NoError(t, err)

	tasks := NewTaskDAGStore(db)
	rootID := "08d0d955-0ab9-4e4f-bf98-3266f837a15c"
	dependentID := "5d9a7ed8-d5ea-4d97-bfc7-87dbd25bd9c0"
	timedOutID := "a8ced57a-c52f-4d07-8b59-79e17b2b695e"
	_, err = tasks.Create(context.Background(), runtime.TaskDAG{RunID: runID, Scope: scope, Tasks: []runtime.AgentTask{
		{TaskID: rootID, Type: "intake", OwnerAgentID: "intake-agent", IdempotencyKey: "intake", Revision: 1, Deadline: time.Now().Add(time.Hour)},
		{TaskID: dependentID, Type: "evidence", OwnerAgentID: "evidence-agent", IdempotencyKey: "evidence", BlockedBy: []string{rootID}, Revision: 1, Deadline: time.Now().Add(time.Hour)},
		{TaskID: timedOutID, Type: "review", OwnerAgentID: "review-agent", IdempotencyKey: "review", BlockedBy: []string{rootID}, Revision: 1, Deadline: time.Now().Add(-time.Minute)},
	}})
	require.NoError(t, err)
	expired, err := tasks.ExpireBlocked(context.Background(), scope, runID, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	rootLease, err := tasks.Claim(context.Background(), scope, rootID, 1, "worker-a", time.Minute)
	require.NoError(t, err)
	_, err = tasks.Complete(context.Background(), scope, rootID, rootLease.Task.Revision, rootLease.FencingToken, runtime.TaskCompletion{Status: runtime.TaskSucceeded, Output: []byte(`{"intake":"done"}`)})
	require.NoError(t, err)
	loaded, err := tasks.Load(context.Background(), scope, runID)
	require.NoError(t, err)
	require.Equal(t, runtime.TaskReady, taskByID(t, loaded, dependentID).Status)
	require.Equal(t, runtime.TaskCancelled, taskByID(t, loaded, timedOutID).Status)

	leaseA, err := tasks.Claim(context.Background(), scope, dependentID, taskByID(t, loaded, dependentID).Revision, "worker-a", time.Minute)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE agent_tasks SET lease_expires_at = now() - interval '1 second' WHERE id = ?", dependentID).Error)
	leaseB, err := tasks.Claim(context.Background(), scope, dependentID, leaseA.Task.Revision, "worker-b", time.Minute)
	require.NoError(t, err)
	_, err = tasks.Complete(context.Background(), scope, dependentID, leaseA.Task.Revision, leaseA.FencingToken, runtime.TaskCompletion{Status: runtime.TaskSucceeded, Output: []byte(`{"evidence":"old"}`)})
	require.ErrorIs(t, err, runtime.ErrTaskDAGConflict)
	_, err = tasks.Complete(context.Background(), scope, dependentID, leaseB.Task.Revision, leaseB.FencingToken, runtime.TaskCompletion{Status: runtime.TaskSucceeded, Output: []byte(`{"evidence":"done"}`)})
	require.NoError(t, err)

	mailbox := NewDurableMailboxStore(db)
	messageID := "1f500a42-fcc9-4860-a8ca-49c4f16667cc"
	_, err = mailbox.Enqueue(context.Background(), runtime.AgentMessage{MessageID: messageID, RunID: runID, Scope: scope, SenderAgentID: "intake-agent", TargetAgentID: "evidence-agent", TaskID: dependentID, IdempotencyKey: "handoff-1", Revision: 1, Payload: []byte(`{"kind":"handoff"}`)})
	require.NoError(t, err)
	delivery, err := mailbox.Claim(context.Background(), scope, runID, "evidence-agent", "worker-a", time.Minute)
	require.NoError(t, err)
	acknowledged, err := mailbox.Acknowledge(context.Background(), scope, messageID, delivery.Message.Revision, delivery.FencingToken)
	require.NoError(t, err)
	require.Equal(t, runtime.MailboxDelivered, acknowledged.Status)

	loaded, err = tasks.Load(context.Background(), scope, runID)
	require.NoError(t, err)
	require.Equal(t, runtime.TaskSucceeded, taskByID(t, loaded, dependentID).Status)
}

func taskByID(t *testing.T, dag runtime.TaskDAG, taskID string) runtime.AgentTask {
	t.Helper()
	for _, task := range dag.Tasks {
		if task.TaskID == taskID {
			return task
		}
	}
	t.Fatalf("task %s missing", taskID)
	return runtime.AgentTask{}
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
	require.NoError(t, db.Create(&SessionModel{ID: scope.SessionID, UserID: scope.UserID, Title: "checkpoint test"}).Error)
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
	require.JSONEq(t, string(first.State), string(loaded.State))
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
