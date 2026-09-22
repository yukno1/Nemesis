package vector

import (
	"context"
	"errors"
	"sync"

	"github.com/chengpeng-cp/nexus-agent/internal/observability"
)

// ErrDisabled 向量库未启用。
var ErrDisabled = errors.New("vector store disabled")

// NoopStore 向量库未启用时的空实现：写操作静默成功，检索返回空结果，
// 使 RAG 自然退化为纯关键词检索（自研 keyword.go），而不是整条链路报错。
type NoopStore struct{ once sync.Once }

func NewNoopStore() *NoopStore { return &NoopStore{} }

func (s *NoopStore) warn() {
	s.once.Do(func() {
		observability.LogWarn("vector store is noop (milvus disabled), dense retrieval returns empty")
	})
}

func (s *NoopStore) EnsureCollection(context.Context, string, int) error { s.warn(); return nil }
func (s *NoopStore) Upsert(context.Context, string, []Record) error      { s.warn(); return nil }
func (s *NoopStore) Delete(context.Context, string, []int64) error       { s.warn(); return nil }
func (s *NoopStore) DropCollection(context.Context, string) error        { s.warn(); return nil }
func (s *NoopStore) Count(context.Context, string) (int64, error)        { s.warn(); return 0, nil }
func (s *NoopStore) Search(context.Context, string, []float32, int) ([]SearchHit, error) {
	s.warn()
	return nil, nil // 空结果：混检退化为空向量通道
}
