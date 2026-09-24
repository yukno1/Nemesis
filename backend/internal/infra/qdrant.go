// 基础设施：Qdrant 向量库。
//
// 与 Milvus 的定位差异（教学点，可与 infra/milvus.go 对照讲解）：
//   - Milvus 要先定义 Schema 再建集合，字段固定（见 NewKBSchema/NewMemorySchema）；
//   - Qdrant 是 schemaless 的，元数据直接进 payload，建集合只需声明向量参数，
//     所以这里没有 Schema 段，只有「向量配置 / HNSW 配置 / 建集合请求」。
//
// 端口别填错：6333 是 REST，6334 是 gRPC，Go SDK 走 gRPC，addr 应填 6334。
package infra

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/observability/logger"
)

// NewQdrant 初始化 Qdrant gRPC 客户端。
//
// 约定与 NewMilvus 一致：mode=off 或 addr 为空时返回 (nil, nil)，
// 由装配处降级为空实现，而不是让整条 RAG 链路崩掉。
func NewQdrant(ctx context.Context, cfg *config.Qdrant) (*qdrant.Client, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "off" || mode == "disabled" || cfg.Addr == "" {
		logger.L().Warn("qdrant disabled, vector features degraded", logger.TraceID("infra"))
		return nil, nil
	}

	// SDK 需要 host/port 分开，addr 统一写成 host:port
	host, portStr, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("split qdrant addr %q: %w", cfg.Addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("parse qdrant port %q: %w", portStr, err)
	}

	// 与 NewMilvus 一致：超时保护，连不上时快速失败而不是静默挂死
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cli, err := qdrant.NewClient(&qdrant.Config{
		Host:   host,
		Port:   port,
		APIKey: cfg.APIKey,
		UseTLS: cfg.HTTPS,
	})
	if err != nil {
		return nil, fmt.Errorf("new qdrant client: %w", err)
	}
	// gRPC 是惰性连接：不探活的话，连不上的错误会推迟到首次检索才暴露
	if _, err := cli.HealthCheck(dialCtx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("qdrant health check: %w", err)
	}
	return cli, nil
}

// ---- 集合参数定义（对应 milvus.go 的「集合 Schema 定义」段）----

// NewQdrantVectorsConfig 单路稠密向量配置（COSINE，dim 维）。
//
// 用无名向量即可，不需要像 Milvus 那样区分 dense / vector 字段名。
func NewQdrantVectorsConfig(dim int) *qdrant.VectorsConfig {
	return qdrant.NewVectorsConfig(&qdrant.VectorParams{
		Size:     uint64(dim),
		Distance: qdrant.Distance_Cosine, // 对齐 infra.NewHNSWIndex 的 entity.COSINE
	})
}

// NewQdrantHNSWConfig HNSW 参数（对齐 infra.NewHNSWIndex：M=16 / ef_construct=200）。
func NewQdrantHNSWConfig() *qdrant.HnswConfigDiff {
	return &qdrant.HnswConfigDiff{
		M:                  qdrant.PtrOf(uint64(16)),
		EfConstruct:        qdrant.PtrOf(uint64(200)),
		FullScanThreshold:  qdrant.PtrOf(uint64(10000)), // 小集合直接暴力扫，比走索引更快
		MaxIndexingThreads: qdrant.PtrOf(uint64(0)),     // 0 = 自动
	}
}

// NewQdrantCollection 建集合请求。
//
// 注意：Qdrant 的 HNSW 由服务端自动维护，不需要 Milvus 那套
// CreateIndex + LoadCollection，建完即可写入检索。
func NewQdrantCollection(collection string, dim int) *qdrant.CreateCollection {
	return &qdrant.CreateCollection{
		CollectionName: collection,
		VectorsConfig:  NewQdrantVectorsConfig(dim),
		HnswConfig:     NewQdrantHNSWConfig(),
	}
}
