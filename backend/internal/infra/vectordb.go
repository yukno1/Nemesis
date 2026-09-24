// 基础设施：向量库分发层。
//
// 职责边界（与 internal/vector 的分工）：
//   - 本文件只按 cfg.Vector.Driver 建「客户端」，不碰 Store 接口；
//     因为 internal/vector 依赖 infra（milvus_store.go 要用 infra.NewKBSchema），
//     infra 再 import vector 会成环。
//   - 客户端 → Store 的转换在 internal/vector.NewStore 完成（可接受 *infra.VectorDB）。
//
// 降级约定（与 NewMilvus / NewQdrant 一致）：后端没配好时客户端为 nil，
// 由上层落成 NoopStore，而不是让 RAG 链路 nil panic。
package infra

import (
	"context"
	"fmt"
	"strings"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/qdrant/go-client/qdrant"
	"go.uber.org/zap"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/observability/logger"
)

// 向量库 driver 取值（与 cfg.Vector.Driver 对齐）。
const (
	VectorDriverMilvus = "milvus"
	VectorDriverQdrant = "qdrant"
	VectorDriverOff    = "off"
)

// VectorDB 向量库句柄：driver + 二选一的客户端 + 统一维度。
//
// 两个客户端字段最多只有一个非 nil；都为 nil 即代表未启用（Driver 会被置为 off）。
type VectorDB struct {
	Driver string
	Dim    int // 统一维度，避免 main 里再纠结 vector.dim 还是 milvus.dim
	Milvus client.Client
	Qdrant *qdrant.Client
}

// NewVectorDB 按 cfg.Vector.Driver 初始化向量库客户端。
func NewVectorDB(ctx context.Context, cfg *config.Config) (*VectorDB, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Vector.Driver))

	// 维度统一取 vector.dim，兼容旧配置写在 milvus.dim 的情况
	dim := cfg.Vector.Dim
	if dim <= 0 {
		dim = cfg.Milvus.Dim
	}
	vdb := &VectorDB{Driver: driver, Dim: dim}

	switch driver {
	case VectorDriverMilvus:
		cli, err := NewMilvus(ctx, &cfg.Milvus)
		if err != nil {
			return nil, fmt.Errorf("milvus: %w", err)
		}
		vdb.Milvus = cli

	case VectorDriverQdrant, "":
		// driver 未显式配置时也走 qdrant（config.yaml 现状），
		// 若 qdrant.mode=off / addr 为空，NewQdrant 返回 nil，下面统一降级
		cli, err := NewQdrant(ctx, &cfg.Qdrant)
		if err != nil {
			return nil, fmt.Errorf("qdrant: %w", err)
		}
		vdb.Qdrant = cli

	case VectorDriverOff, "disabled", "noop":
		logger.L().Warn("vector db disabled by config", logger.TraceID("infra"))

	default:
		return nil, fmt.Errorf("unknown vector driver %q (want milvus|qdrant|off)", cfg.Vector.Driver)
	}

	// 后端没起来/没配好：统一降级为 off，交给上层选 NoopStore
	if vdb.Milvus == nil && vdb.Qdrant == nil {
		vdb.Driver = VectorDriverOff
	}
	logger.L().Info("vector db ready", logger.TraceID("infra"),
		zap.String("driver", vdb.Driver), zap.Int("dim", vdb.Dim))
	return vdb, nil
}
