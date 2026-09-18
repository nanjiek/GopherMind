package integration_test

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
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"gophermind/internal/config"
	"gophermind/internal/core/service"
	postgresrepo "gophermind/internal/repo/postgres"
	redisrepo "gophermind/internal/repo/redis"
	"gophermind/internal/session/surface"
)

func TestRedisSurfaceDeletionRebuildsFromPostgres(t *testing.T) {
	db, cleanupDB := isolatedPostgresSchema(t)
	defer cleanupDB()
	require.NoError(t, postgresrepo.ApplyMigrations(context.Background(), db))

	redisAddress := os.Getenv("REDIS_TEST_ADDR")
	if redisAddress == "" {
		t.Skip("REDIS_TEST_ADDR is required for Redis integration tests")
	}
	cache := redisrepo.NewSessionCache(config.RedisConfig{
		Addrs: []string{redisAddress}, PoolSize: 4, MinIdleConns: 1,
	}, zap.NewNop())
	defer cache.Close()
	require.False(t, cache.IsDegraded())

	repo := postgresrepo.NewSessionRepository(db)
	sessions := service.NewSessionService(repo, cache, zap.NewNop())
	created, err := repo.CreateSessionWithFirstMessage(context.Background(), "surface-user", "Surface", "first", "surface-request-1")
	require.NoError(t, err)
	key := surface.ConversationKey("surface-user", created.ID)
	defer cache.InvalidateSurface(context.Background(), key)

	firstWindow, err := sessions.LoadWindow(context.Background(), "surface-user", created.ID)
	require.NoError(t, err)
	require.Len(t, firstWindow, 1)
	firstSurface, ok, err := cache.GetSurface(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, repo.AppendUserMessage(context.Background(), "surface-user", created.ID, "second", "surface-request-2"))
	secondWindow, err := sessions.LoadWindow(context.Background(), "surface-user", created.ID)
	require.NoError(t, err)
	require.Len(t, secondWindow, 2)
	secondSurface, ok, err := cache.GetSurface(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, secondSurface.SourceSeq, firstSurface.SourceSeq)

	require.NoError(t, cache.InvalidateSurface(context.Background(), key))
	_, ok, err = cache.GetSurface(context.Background(), key)
	require.NoError(t, err)
	require.False(t, ok)

	rebuiltWindow, err := sessions.LoadWindow(context.Background(), "surface-user", created.ID)
	require.NoError(t, err)
	require.Equal(t, secondWindow, rebuiltWindow)
	rebuiltSurface, ok, err := cache.GetSurface(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, secondSurface.SourceSeq, rebuiltSurface.SourceSeq)
	require.Equal(t, secondSurface.Messages, rebuiltSurface.Messages)
	require.True(t, rebuiltSurface.FreshFor(secondSurface.SourceSeq, time.Now()))
}

func isolatedPostgresSchema(t *testing.T) (*gorm.DB, func()) {
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

	return db, func() {
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
}
