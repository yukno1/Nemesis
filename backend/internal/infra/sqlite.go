package infra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/chengpeng-cp/nexus-agent/internal/observability/logger"
)

// NewSQLite 本地单文件存储（journal WAL + 单写连接）。
func NewSQLite(ctx context.Context, path string) (*gorm.DB, Dialect, error) {
	if path == "" {
		path = "./.minerva/minerva.sqlite"
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", fmt.Errorf("mkdir for sqlite: %w", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(path), gormConfig())
	if err != nil {
		return nil, "", fmt.Errorf("open sqlite: %w", err)
	}

	// 这四条是 SQLite 能否正常工作的命门
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",   // 读写不互斥
		"PRAGMA synchronous = NORMAL", // WAL 下安全且快数倍
		"PRAGMA busy_timeout = 5000",  // 写冲突自动等待而非立刻报错
		"PRAGMA foreign_keys = ON",    // SQLite 默认关闭外键！
	} {
		if err := db.Exec(p).Error; err != nil {
			return nil, "", fmt.Errorf("apply %q: %w", p, err)
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("get sql.DB: %w", err)
	}
	// 全库单写锁：单机教学场景串行化最稳；并发压力大时改 4 并保留 busy_timeout
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, "", fmt.Errorf("ping sqlite: %w", err)
	}

	logger.L().Info("sqlite connected", logger.TraceID("infra"), zap.String("path", path))
	return db, DialectSQLite, nil
}
