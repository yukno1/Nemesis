// 基础设施：s3 兼容对象存储（原始文档/附件）。
package infra

import (
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/observability/logger"
)

// NewObjectStorage 初始化 s3 客户端并确保桶存在。
func NewObjectStorage(ctx context.Context, cfg *config.ObjectStorage) (*minio.Client, error) {
	var cli *minio.Client
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "rustfs":
		rustfsCli, err := NewRustFS(ctx, &config.RustFS{
			Endpoint:  cfg.Endpoint,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			Bucket:    cfg.Bucket,
			UseSSL:    cfg.UseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("new rustfs client: %w", err)
		}
		cli = rustfsCli
	case "seaweedfs":
		seaweedfsCli, err := NewSeaweedFS(ctx, &config.SeaweedFS{
			Endpoint:  cfg.Endpoint,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			Bucket:    cfg.Bucket,
			UseSSL:    cfg.UseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("new rustfs client: %w", err)
		}
		cli = seaweedfsCli
	default: // minio 或未指定：直接用配置值，不做厂商默认兜底
		if strings.TrimSpace(cfg.Endpoint) == "" {
			logger.L().Warn("object storage disabled", logger.TraceID("infra"))
			return nil, nil
		}
		minioCli, err := minio.New(cfg.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.UseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("new s3 client: %w", err)
		}
		cli = minioCli
	}

	exists, err := cli.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %s: %w", cfg.Bucket, err)
		}
	}
	return cli, nil
}

// ObjectKey 生成文档对象 Key：docs/{kbID}/{docID}_{filename}。
func ObjectKey(kbID, docID int64, filename string) string {
	return fmt.Sprintf("docs/%d/%d_%s", kbID, docID, filename)
}
