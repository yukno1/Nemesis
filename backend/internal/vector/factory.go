// Package vector 向量存储实现的工厂：把 infra 建好的客户端包成 Store。
package vector

import "github.com/chengpeng-cp/nexus-agent/internal/infra"

// NewStore 按 VectorDB.Driver 选择实现；未启用或客户端缺失时降级 NoopStore（永不 panic）。
func NewStore(vdb *infra.VectorDB) Store {
	if vdb == nil {
		return newNoopStore()
	}
	switch vdb.Driver {
	case infra.VectorDriverMilvus:
		if vdb.Milvus != nil {
			return newMilvusStore(vdb.Milvus)
		}
	case infra.VectorDriverQdrant:
		if vdb.Qdrant != nil {
			return newQdrantStore(vdb.Qdrant) // 依赖 vector/qdrant_store.go
		}
	}
	return newNoopStore()
}
