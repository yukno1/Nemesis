// 基础设施：RustFS 对象存储（S3 兼容）。
//
// RustFS 是 Rust 写的 S3 兼容存储：S3 API 默认 9000、控制台 9001，
// 镜像默认账号 rustfsadmin / rustfsadmin（环境变量 RUSTFS_ADDRESS 可改端口）。
// 协议与 MinIO 一致，所以客户端继续复用 minio-go，只有默认值不同
// —— 这也说明为什么「后端可替换」不该体现在业务代码里。
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

// RustFS 默认值（配置留空时兜底，思路同 NewMilvus 的 mode/addr 兜底）。
const (
	defaultRustFSEndpoint  = "127.0.0.1:9000"
	defaultRustFSAccessKey = "rustfsadmin"
	defaultRustFSSecretKey = "rustfsadmin"
)

// NewRustFS 初始化 RustFS 客户端并确保桶存在。
//
// cfg 为 nil 视为关闭，返回 (nil, nil)，由调用方降级；
// endpoint / 凭证留空时用 RustFS 官方默认值，便于本地一键起环境。
func NewRustFS(ctx context.Context, cfg *config.RustFS) (*minio.Client, error) {
	if cfg == nil {
		logger.L().Warn("rustfs disabled, object storage features degraded", logger.TraceID("infra"))
		return nil, nil
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = defaultRustFSEndpoint
	}
	ak, sk := cfg.AccessKey, cfg.SecretKey
	if ak == "" {
		ak = defaultRustFSAccessKey
	}
	if sk == "" {
		sk = defaultRustFSSecretKey
	}

	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(ak, sk, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("new rustfs client: %w", err)
	}

	// 与 NewMilvus / NewQdrant 一致：建连超时保护
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if cfg.Bucket == "" {
		logger.L().Warn("rustfs bucket not configured, skip ensure", logger.TraceID("infra"))
		return cli, nil
	}
	exists, err := cli.BucketExists(dialCtx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check rustfs bucket: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(dialCtx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create rustfs bucket %s: %w", cfg.Bucket, err)
		}
	}
	logger.L().Info("rustfs connected", logger.TraceID("infra"),
		zap.String("endpoint", endpoint), zap.String("bucket", cfg.Bucket))
	return cli, nil
}
