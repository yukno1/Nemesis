// Qdrant 实现的 VectorStore。
//
// 与 MilvusStore 的关键差异（教学点）：
//  1. schemaless：Milvus 要先建 Schema（doc_id/seq/page/content 各一列），
//     Qdrant 元数据直接进 payload，建集合只需声明「向量参数 + HNSW」。
//  2. 真 upsert：同 ID 覆盖，不需要 Milvus 那种 insert + flush 的两步式教学简化。
//  3. 建完即可检索：没有 Milvus 的 CreateIndex / LoadCollection 两步。
package vector

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"

	"github.com/chengpeng-cp/nexus-agent/internal/infra"
)

// QdrantStore 基于 qdrant go-client（gRPC，默认 6334）的实现。
type QdrantStore struct {
	cli *qdrant.Client
}

// NewQdrantStore 构造。
func newQdrantStore(cli *qdrant.Client) *QdrantStore { return &QdrantStore{cli: cli} }

// EnsureCollection 确保集合存在（集合参数定义见 infra.NewQdrantCollection）。
func (s *QdrantStore) EnsureCollection(ctx context.Context, collection string, dim int) error {
	exists, err := s.cli.CollectionExists(ctx, collection)
	if err != nil {
		return fmt.Errorf("has collection: %w", err)
	}
	if exists {
		return nil
	}
	// 知识库集合与记忆集合共用同一套向量参数，区别只在 payload 字段
	if err := s.cli.CreateCollection(ctx, infra.NewQdrantCollection(collection, dim)); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	return nil
}

// Upsert 写入/覆盖向量（同 ID 覆盖，天然幂等）。
func (s *QdrantStore) Upsert(ctx context.Context, collection string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	points := make([]*qdrant.PointStruct, 0, len(records))
	for _, r := range records {
		points = append(points, &qdrant.PointStruct{
			Id:      qdrant.NewIDNum(uint64(r.ID)),
			Vectors: qdrant.NewVectorsDense(r.Vector),
			Payload: toQdrantPayload(r.Metadata),
		})
	}
	// Wait=true 同步等待落盘：写完立刻可检索（吞吐优先的大批量场景可改 false）
	_, err := s.cli.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collection,
		Points:         points,
		Wait:           qdrant.PtrOf(true),
	})
	if err != nil {
		return fmt.Errorf("upsert points: %w", err)
	}
	return nil
}

// Search 最近邻检索（度量在建集合时固定为 COSINE）。
func (s *QdrantStore) Search(ctx context.Context, collection string, vector []float32, topK int) ([]SearchHit, error) {
	if topK <= 0 {
		topK = 10
	}
	// 新版 SDK 没有独立的 Search，统一走 Query + Nearest
	res, err := s.cli.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collection,
		Query:          qdrant.NewQueryNearest(qdrant.NewVectorInputDense(vector)),
		Limit:          qdrant.PtrOf(uint64(topK)),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		// HNSW 的 ef 决定候选队列长度，对齐 Milvus 侧的 ef=64
		Params: &qdrant.SearchParams{HnswEf: qdrant.PtrOf(uint64(64))},
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant query: %w", err)
	}

	hits := make([]SearchHit, 0, len(res))
	for _, p := range res {
		id := p.GetId()
		if id == nil {
			continue
		}
		hits = append(hits, SearchHit{
			// 主键是 chunk_id / memory_id，正文由 repo 回查补齐（与 Milvus 路径一致）
			Record: Record{ID: int64(id.GetNum()), Metadata: fromQdrantPayload(p.GetPayload())},
			Score:  p.GetScore(),
		})
	}
	return hits, nil
}

// Delete 按主键删除。
func (s *QdrantStore) Delete(ctx context.Context, collection string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	pids := make([]*qdrant.PointId, 0, len(ids))
	for _, id := range ids {
		pids = append(pids, qdrant.NewIDNum(uint64(id)))
	}
	_, err := s.cli.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: collection,
		Wait:           qdrant.PtrOf(true),
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{Ids: pids},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("delete points: %w", err)
	}
	return nil
}

// DropCollection 删除集合（知识库删除时）。
func (s *QdrantStore) DropCollection(ctx context.Context, collection string) error {
	exists, err := s.cli.CollectionExists(ctx, collection)
	if err != nil || !exists {
		return err
	}
	return s.cli.DeleteCollection(ctx, collection)
}

// Count 统计点数。
func (s *QdrantStore) Count(ctx context.Context, collection string) (int64, error) {
	n, err := s.cli.Count(ctx, &qdrant.CountPoints{
		CollectionName: collection,
		Exact:          qdrant.PtrOf(true), // 精确计数；超大集合可改 false 走估算
	})
	if err != nil {
		return 0, err
	}
	return int64(n), nil
}

// ---- payload 与 metadata 互转（Qdrant 的 payload 就是 Milvus 的元数据列）----

// toQdrantPayload 把 Record.Metadata 转成 Qdrant payload；不支持的类型直接跳过。
func toQdrantPayload(meta map[string]any) map[string]*qdrant.Value {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]*qdrant.Value, len(meta))
	for k, v := range meta {
		switch vv := v.(type) {
		case string:
			out[k] = qdrant.NewValueString(vv)
		case int:
			out[k] = qdrant.NewValueInt(int64(vv))
		case int32:
			out[k] = qdrant.NewValueInt(int64(vv))
		case int64:
			out[k] = qdrant.NewValueInt(vv)
		case float32:
			out[k] = qdrant.NewValueDouble(float64(vv))
		case float64:
			out[k] = qdrant.NewValueDouble(vv)
		case bool:
			out[k] = qdrant.NewValueBool(vv)
		}
	}
	return out
}

// fromQdrantPayload 把检索返回的 payload 还原成 Metadata（当前链路只用 ID+Score，
// 保留转换是为了调试面板与后续「按 payload 过滤」的扩展）。
func fromQdrantPayload(payload map[string]*qdrant.Value) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if v == nil {
			continue
		}
		switch kk := v.Kind.(type) {
		case *qdrant.Value_StringValue:
			out[k] = kk.StringValue
		case *qdrant.Value_IntegerValue:
			out[k] = kk.IntegerValue
		case *qdrant.Value_DoubleValue:
			out[k] = kk.DoubleValue
		case *qdrant.Value_BoolValue:
			out[k] = kk.BoolValue
		}
	}
	return out
}
