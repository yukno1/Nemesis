// 基础设施：SeaweedFS 对象存储（S3 兼容）。
//
// 与 RustFS 的差异（教学点，可与 infra/rustfs.go 对照）：
//   - 端口：SeaweedFS 的 S3 网关默认 8333，master 9333、filer 8888（控制台）；
//     容器里要显式 `weed server -s3` 才会拉起 S3 网关，官方镜像不给 command 起不来。
//   - 凭证：SeaweedFS 没有内置默认账号 —— 不挂 -s3.config 时是匿名访问，
//     匿名通常只有读权限，PutObject 会失败。所以这里不做凭证兜底，留空只告警不报错，
//     由部署方决定是「匿名只读」还是「挂 identity 文件」。
//
// 协议同为 S3，客户端依然复用 minio-go，业务代码感知不到后端换成了 SeaweedFS。
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

// defaultSeaweedFSEndpoint SeaweedFS S3 网关默认地址（区别于 RustFS/MinIO 的 9000）。
const defaultSeaweedFSEndpoint = "127.0.0.1:8333"

// NewSeaweedFS 初始化 SeaweedFS 客户端并确保桶存在。
//
// cfg 为 nil 视为关闭，返回 (nil, nil)，由调用方降级；
// endpoint 留空用 8333 兜底；凭证留空则走匿名（可能只读，见文件头注释）。
func NewSeaweedFS(ctx context.Context, cfg *config.SeaweedFS) (*minio.Client, error) {
	if cfg == nil {
		logger.L().Warn("seaweedfs disabled, object storage features degraded", logger.TraceID("infra"))
		return nil, nil
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = defaultSeaweedFSEndpoint
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		// SeaweedFS 匿名访问通常只有读权限，PutObject/ListBuckets 可能被拒
		logger.L().Warn("seaweedfs credentials empty, anonymous access may be read-only",
			logger.TraceID("infra"))
	}

	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("new seaweedfs client: %w", err)
	}

	// 与 NewMilvus / NewQdrant / NewRustFS 一致：建连超时保护
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if cfg.Bucket == "" {
		logger.L().Warn("seaweedfs bucket not configured, skip ensure", logger.TraceID("infra"))
		return cli, nil
	}
	exists, err := cli.BucketExists(dialCtx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check seaweedfs bucket: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(dialCtx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create seaweedfs bucket %s: %w", cfg.Bucket, err)
		}
	}
	logger.L().Info("seaweedfs connected", logger.TraceID("infra"),
		zap.String("endpoint", endpoint), zap.String("bucket", cfg.Bucket))
	return cli, nil
}
