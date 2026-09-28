// 基础设施：s3 兼容对象存储（原始文档/附件）。
package infra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/observability/logger"
)

const defaultEndpoint = "127.0.0.1:9000"

// NewObjectStorage 初始化 s3 兼容对象存储并确保桶存在。
//
// 凭证语义（rustfs / seaweedfs 完全一致）：
//
//	access_key 与 secret_key 均非空 → SigV4 签名
//	任一为空                        → 匿名（minio-go 不加 Authorization 头）
func NewObjectStorage(ctx context.Context, cfg *config.ObjectStorage) (*minio.Client, error) {
	if cfg == nil {
		logger.L().Warn("object storage disabled, degraded", logger.TraceID("infra"))
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "rustfs":
		return NewRustFS(ctx, cfg)
	case "seaweedfs", "weed":
		return NewSeaweedFS(ctx, cfg)
	default:
		// 默认：通用 S3 兼容后端（minio / 其它）
		endpoint := strings.TrimSpace(cfg.Endpoint)
		if endpoint == "" {
			endpoint = defaultEndpoint
		}

		cli, err := minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.UseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("new %s client: %w", cfg.Driver, err)
		}

		if err := ensureBucket(ctx, cli, cfg.Bucket); err != nil {
			return nil, err
		}
		logger.L().Info("object storage connected", logger.TraceID("infra"),
			zap.String("driver", cfg.Driver), zap.String("endpoint", endpoint),
			zap.String("bucket", cfg.Bucket), zap.String("auth", AuthMode(cfg.AccessKey, cfg.SecretKey)))
		return cli, nil
	}

}

// ObjectKey 生成文档对象 Key：docs/{kbID}/{docID}_{filename}。
func ObjectKey(kbID, docID int64, filename string) string {
	return fmt.Sprintf("docs/%d/%d_%s", kbID, docID, filename)
}

// AuthMode 认证模式（启动日志用）：均非空 signed，任一为空 anonymous。
func AuthMode(ak, sk string) string {
	if ak == "" || sk == "" {
		return "anonymous"
	}
	return "signed"
}

// ensureBucket 桶不存在则创建（10s 建连超时保护）。
func ensureBucket(ctx context.Context, cli *minio.Client, bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		logger.L().Warn("object storage bucket not configured, skip ensure", logger.TraceID("infra"))
		return nil
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exists, err := cli.BucketExists(dialCtx, bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(dialCtx, bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", bucket, err)
		}
	}
	return nil
}
