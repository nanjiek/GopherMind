package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const currentSchemaVersion = 2

var requiredTables = []string{
	"users", "refresh_tokens", "sessions", "messages", "consumer_inbox",
	"documents", "eval_runs", "mcp_jobs", "memory_records", "event_streams",
	"clinical_events", "projection_checkpoints", "outbox_messages", "agent_runs", "agent_steps", "agent_run_checkpoints",
}

//go:embed migrations/*.up.sql
var migrationFiles embed.FS

// ApplyMigrations applies each pending migration in its own transaction.
func ApplyMigrations(ctx context.Context, db *gorm.DB) error {
	if err := rejectUnknownUnmanagedSchema(ctx, db); err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)
	for _, name := range entries {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		var applied int64
		if err := db.WithContext(ctx).Raw("SELECT count(*) FROM schema_migrations WHERE version = ?", version).Scan(&applied).Error; err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied != 0 {
			continue
		}
		sqlBytes, err := migrationFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %d: %w", version, err)
		}
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(string(sqlBytes)).Error; err != nil {
				return err
			}
			return tx.Exec("INSERT INTO schema_migrations(version) VALUES (?)", version).Error
		}); err != nil {
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
	}
	return VerifySchema(ctx, db)
}

// VerifySchema ensures the database matches the application schema version.
func VerifySchema(ctx context.Context, db *gorm.DB) error {
	var tableCount int64
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'schema_migrations'`).Scan(&tableCount).Error; err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	if tableCount == 0 {
		return errors.New("database schema is not initialized; run cmd/migrate")
	}
	var version int
	if err := db.WithContext(ctx).Raw("SELECT COALESCE(max(version), 0) FROM schema_migrations").Scan(&version).Error; err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version != currentSchemaVersion {
		return fmt.Errorf("incompatible database schema version %d; expected %d", version, currentSchemaVersion)
	}
	var requiredTableCount int64
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name IN ?`, requiredTables).Scan(&requiredTableCount).Error; err != nil {
		return fmt.Errorf("verify required tables: %w", err)
	}
	if requiredTableCount != int64(len(requiredTables)) {
		return fmt.Errorf("database schema is incomplete: found %d of %d required tables", requiredTableCount, len(requiredTables))
	}
	return nil
}

func rejectUnknownUnmanagedSchema(ctx context.Context, db *gorm.DB) error {
	var managed int64
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'schema_migrations'`).Scan(&managed).Error; err != nil {
		return fmt.Errorf("inspect migration metadata: %w", err)
	}
	if managed != 0 {
		return nil
	}
	var tables []string
	if err := db.WithContext(ctx).Raw(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'
		ORDER BY table_name`).Scan(&tables).Error; err != nil {
		return fmt.Errorf("inspect existing tables: %w", err)
	}
	if len(tables) != 0 {
		return fmt.Errorf("refusing to initialize non-empty unmanaged schema containing: %s", strings.Join(tables, ", "))
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	base := name[strings.LastIndex(name, "/")+1:]
	prefix, _, ok := strings.Cut(base, "_")
	if !ok {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid migration filename %q: %w", name, err)
	}
	return version, nil
}
