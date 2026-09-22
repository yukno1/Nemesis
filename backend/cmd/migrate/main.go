// 数据库迁移工具：执行 migrations/*.sql 并种子管理员账号。
//
// 为什么独立成 cmd：服务进程保持无状态启动（main.go 不做 DDL），
// 迁移由 `make migrate`（本命令）显式执行 —— 生产可用 CI 卡点，教学可手动执行。
//
// 用法：
//
//	go run ./cmd/migrate          # 执行全部 up 迁移 + 种子
//	go run ./cmd/migrate -down 1  # 回滚最近 1 个版本（危险，教学演示用）
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // lib/pq 驱动 + schema_migrations 表
	_ "github.com/golang-migrate/migrate/v4/source/file"       // file:// 读取 migrations/ 目录
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/infra"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/repo"
)

func main() {
	down := flag.Int("down", 0, "回滚 N 个版本（默认 0 = 只做升级）")
	flag.Parse()

	if err := run(*down); err != nil {
		fmt.Fprintf(os.Stderr, "migrate failed: %v\n", err)
		os.Exit(1)
	}
}

// func run(down int) error {
// 	// ① 复用统一配置加载（PG DSN / sqlite path 来自 config.yaml / NX_ 环境变量）
// 	cfg, err := config.Load("")
// 	if err != nil {
// 		return fmt.Errorf("load config: %w", err)
// 	}

// 	// ② 升级：file source 顺序执行 migrations/NNN_xxx.up.sql，
// 	//    已应用的记录在 schema_migrations 表（golang-migrate 自动维护，幂等）
// 	m, err := migrate.New("file://migrations", cfg.PG.DSN)
// 	if err != nil {
// 		return fmt.Errorf("new migrator: %w", err)
// 	}
// 	defer func() { _, _ = m.Close() }()

// 	if down > 0 {
// 		if err := m.Steps(-down); err != nil {
// 			return fmt.Errorf("step down %d: %w", down, err)
// 		}
// 		fmt.Printf("rolled back %d version(s)\n", down)
// 		return nil
// 	}
// 	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
// 		return fmt.Errorf("up: %w", err)
// 	}
// 	fmt.Println("migrations applied")

// 	// ③ 种子管理员：users 表为空才插入（幂等，密码哈希在代码层生成，
// 	//    与 002_seed.up.sql 头部注释的设计一致）
// 	if err := seedAdmin(cfg.PG.DSN); err != nil {
// 		return fmt.Errorf("seed admin: %w", err)
// 	}
// 	return nil
// }

func run(down int) error {
	// ① 复用统一配置加载（PG DSN / sqlite path 来自 config.yaml / NX_ 环境变量）
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// ② 建立连接（PG / SQLite 由 cfg.DB.Mode 决定）
	db, dialect, err := infra.NewDB(disabledCtx(), cfg)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	repo.SetDialect(dialect)
	defer func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}()

	// ③ SQLite 本地模式：golang-migrate 的 sqlite 驱动依赖 CGO，
	//    这里改用 GORM AutoMigrate（依据 model tag 建表）；PG 路径保持原样。
	if cfg.DB.Mode == "sqlite" {
		if down > 0 {
			return fmt.Errorf("sqlite 模式不支持回滚：删除 %s 后重跑即可", cfg.Sqlite.Path)
		}
		if err := infra.AutoMigrate(db); err != nil {
			return fmt.Errorf("automigrate: %w", err)
		}
		fmt.Println("sqlite schema ready")

		// SQLite 模式没有 SQL 迁移，这里补上模型种子
		if err := seedDefaultModels(db); err != nil {
			return fmt.Errorf("seed models: %w", err)
		}

		return seedAdmin(db)
	}

	// ④ PG：file source 顺序执行 migrations/NNN_xxx.up.sql
	if cfg.DB.Mode == "pg" {
		m, err := migrate.New("file://migrations", cfg.PG.DSN)
		if err != nil {
			return fmt.Errorf("new migrator: %w", err)
		}
		defer func() { _, _ = m.Close() }()

		if down > 0 {
			if err := m.Steps(-down); err != nil {
				return fmt.Errorf("step down %d: %w", down, err)
			}
			fmt.Printf("rolled back %d version(s)\n", down)
			return nil
		}
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			return fmt.Errorf("up: %w", err)
		}
		fmt.Println("migrations applied")
	}

	// ⑤ 种子管理员：users 表为空才插入（幂等）
	return seedAdmin(db)
}

// seedAdmin 插入默认管理员 admin / admin123（仅当 users 表为空）。
// func seedAdmin(dsn string) error {
// 	db, err := infra.NewPG(disabledCtx(), &config.PG{DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1})
// 	if err != nil {
// 		return err
// 	}

// 	sqlDB, err := db.DB()
// 	if err != nil {
// 		return err
// 	}
// 	defer sqlDB.Close()

// 	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
// 	if err != nil {
// 		return err
// 	}

// 	// 唯一条记录插入：存在任何用户则跳过（与种子 SQL 的幂等约定一致）
// 	res := db.WithContext(disabledCtx()).Exec(`
// 		INSERT INTO users (username, email, password_hash, role, status)
// 		SELECT 'admin', 'admin@nexus.local', ?, 'admin', 1
// 		WHERE NOT EXISTS (SELECT 1 FROM users)`, string(hash))
// 	if res.Error != nil {
// 		return res.Error
// 	}
// 	if res.RowsAffected > 0 {
// 		fmt.Println("seeded admin user: admin / admin123 (请在管理页修改密码)")
// 	} else {
// 		fmt.Println("users table not empty, skip seed")
// 	}
// 	return nil
// }

// seedAdmin 插入默认管理员 admin / admin123（仅当 users 表为空）。
func seedAdmin(db *gorm.DB) error {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 唯一条记录插入：存在任何用户则跳过（PG 与 SQLite 都支持该写法）
	res := db.WithContext(disabledCtx()).Exec(`
		INSERT INTO users (username, email, password_hash, role, status)
		SELECT 'admin', 'admin@nexus.local', ?, 'admin', 1
		WHERE NOT EXISTS (SELECT 1 FROM users)`, string(hash))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		fmt.Println("seeded admin user: admin / admin123 (请在管理页修改密码)")
	} else {
		fmt.Println("users table not empty, skip seed")
	}
	return nil
}

// disabledCtx 迁移场景不需要可取消上下文，给一个空实现保持 infra.NewPG 签名。
func disabledCtx() (ctx context.Context) {
	return context.Background()
}

// seedDefaultModels SQLite 模式的模型种子（等价于 migrations 002+003 的 provider/model 部分）。
//
// PG 路径仍由 SQL 迁移负责（保持 DDL/种子集中管理的设计）；SQLite 无法复用
// 那套 PG 方言 SQL（元 'xx'::jsonb 强转、ON CONFLICT 差异），故用结构体实现。
func seedDefaultModels(db *gorm.DB) error {
	ctx := disabledCtx()

	providers := []model.ModelProvider{
		{Code: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Status: 1},
		{Code: "qwen", Name: "通义千问", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Status: 1},
		{Code: "zhipu", Name: "智谱AI", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Status: 1},
		{Code: "moonshot", Name: "Kimi", BaseURL: "https://api.moonshot.cn/v1", Status: 1},
		{Code: "doubao", Name: "豆包Ark", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Status: 1},
		{Code: "ollama", Name: "Ollama本地", BaseURL: "http://127.0.0.1:11434/v1", Status: 1},
	}
	for i := range providers {
		p := providers[i]
		var found model.ModelProvider
		err := db.WithContext(ctx).Where("code = ?", p.Code).First(&found).Error
		if err == nil {
			continue // 已存在，幂等跳过
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query provider %s: %w", p.Code, err)
		}
		if err := db.WithContext(ctx).Create(&p).Error; err != nil {
			return fmt.Errorf("create provider %s: %w", p.Code, err)
		}
	}

	// 默认模型绑定 ollama 供应商（对齐 config.yaml: llm.default_chat / default_embedding）
	var ollama model.ModelProvider
	if err := db.WithContext(ctx).Where("code = ?", "ollama").First(&ollama).Error; err != nil {
		return fmt.Errorf("load ollama provider: %w", err)
	}

	cfgs := []model.ModelConfig{
		{
			ProviderID: ollama.ID, ModelName: "qwen3:4b", Alias: "qwen3:4b",
			Type: model.ModelTypeChat, ContextWindow: 32768, MaxOutput: 4096,
			Priority: 10, Capabilities: model.CapabilitiesJSON([]string{"function_call", "reasoning"}),
			IsDefault: true, Status: 1,
		},
		{
			ProviderID: ollama.ID, ModelName: "qwen3-embedding:0.6b", Alias: "qwen3-embedding:0.6b",
			Type: model.ModelTypeEmbed, ContextWindow: 8192, MaxOutput: 0,
			Priority: 10, Capabilities: model.CapabilitiesJSON(nil),
			IsDefault: true, Status: 1,
		},
	}
	for i := range cfgs {
		c := cfgs[i]
		var found model.ModelConfig
		err := db.WithContext(ctx).Where("model_name = ? AND type = ?", c.ModelName, c.Type).First(&found).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("query model %s: %w", c.ModelName, err)
		}
		if err := db.WithContext(ctx).Create(&c).Error; err != nil {
			return fmt.Errorf("create model %s: %w", c.ModelName, err)
		}
	}

	fmt.Println("default models seeded (ollama: qwen3:4b / qwen3-embedding:0.6b)")
	return nil
}
