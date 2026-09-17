package mysql

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"gophermind/internal/config"
)

// NewDB 初始化 Gorm 连接池。
func NewDB(cfg config.MySQLConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{})
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

	if err := db.AutoMigrate(
		&SessionModel{},
		&MessageModel{},
		&UserModel{},
		&RefreshTokenModel{},
		&ConsumerInboxModel{},
		&DocumentModel{},
		&EvalRunModel{},
		&MCPJobModel{},
		&MemoryCatalogModel{},
	); err != nil {
		return nil, err
	}
	return db, nil
}
