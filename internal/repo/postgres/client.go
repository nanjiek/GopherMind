package postgres

import (
	"context"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"gophermind/internal/config"
)

// OpenDB opens the PostgreSQL connection pool without modifying its schema.
func OpenDB(cfg config.PostgresConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	return db, nil
}

// NewDB opens the pool and rejects an uninitialized or incompatible schema.
func NewDB(cfg config.PostgresConfig) (*gorm.DB, error) {
	db, err := OpenDB(cfg)
	if err != nil {
		return nil, err
	}
	if err := VerifySchema(context.Background(), db); err != nil {
		pool, poolErr := db.DB()
		if poolErr == nil {
			_ = pool.Close()
		}
		return nil, err
	}
	return db, nil
}
