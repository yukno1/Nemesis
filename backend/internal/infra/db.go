package infra

import (
	"context"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
)

// Dialect 数据库方言（上层据此写兼容 SQL）。
type Dialect string

const (
	DialectPG     Dialect = "pg"
	DialectSQLite Dialect = "sqlite"
)

// NewDB 按配置选择存储驱动；返回方言供上层写兼容 SQL。
func NewDB(ctx context.Context, cfg *config.Config) (*gorm.DB, Dialect, error) {
	if cfg.DB.Mode == "sqlite" {
		return NewSQLite(ctx, cfg.Sqlite.Path)
	}
	db, err := NewPG(ctx, &cfg.PG) // 已有实现
	if err != nil {
		return nil, "", err
	}
	return db, DialectPG, nil
}

// gormConfig 统一的 GORM 配置（PG / SQLite 共用）。
//
// 抽出来的目的：SQLite 与 PG 共用同一套日志/迁移行为，避免两处配置漂移。
func gormConfig() *gorm.Config {
	return &gorm.Config{
		// 全局禁用外键迁移（表结构由 migrations 管理，GORM 只做读写）
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger: gormlogger.New(
			gormWriter{},
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
			},
		),
	}
}
