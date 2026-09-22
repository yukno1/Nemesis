package infra

import (
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"gorm.io/gorm"
)

// AutoMigrate 仅用于 sqlite 本地模式：由 GORM 依据 model tag 建表。
//
// 注意：PG 生产路径仍然走 migrations/*.sql（DDL 版本化，DDL 与运行时分离的设计不变）。
// 这里只是为了省掉一套方言迁移脚本，属于本地开发的便利措施。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.User{}, &model.ModelProvider{}, &model.ModelConfig{},
		&model.Agent{}, &model.Session{}, &model.Message{},
		&model.MCPServer{}, &model.Tool{},
		&model.KnowledgeBase{}, &model.Document{}, &model.DocumentChunk{},
		&model.Memory{}, &model.Prompt{},
		&model.Workflow{}, &model.WorkflowRun{}, &model.WorkflowStepRun{},
		&model.EvalDataset{}, &model.EvalCase{}, &model.EvalRun{}, &model.EvalResult{},
		&model.Feedback{}, &model.UsageLog{}, &model.SandboxExecution{},
	)
}
