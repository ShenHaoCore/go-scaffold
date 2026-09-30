package model

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go-scaffold/pkg/env"
	"go-scaffold/pkg/logger"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/zeromicro/go-zero/core/logx"
)

// OSSPing 阿里云 OSS 连通检查（ListObjects MaxKeys=1；实现 repo.OSSPinger）。
// 不用 IsBucketExist/ListBuckets：RAM 常无 oss:ListBuckets。
type OSSPing struct {
	bucket         *oss.Bucket
	bucketName     string
	hostConfigured bool
	initErr        error
}

// NewOSSPing 创建 OSS 探针。四项均非空才视为已配置；否则返回未配置（health skipped）。
func NewOSSPing(endpoint, bucket, accessKeyID, accessKeySecret string) *OSSPing {
	endpoint = strings.TrimSpace(endpoint)
	bucket = strings.TrimSpace(bucket)
	accessKeyID = strings.TrimSpace(accessKeyID)
	accessKeySecret = strings.TrimSpace(accessKeySecret)
	if endpoint == "" && bucket == "" && accessKeyID == "" && accessKeySecret == "" {
		return &OSSPing{}
	}
	if endpoint == "" || bucket == "" || accessKeyID == "" || accessKeySecret == "" {
		// 部分字段：视为未启用（health skipped），避免 Secret 仅有 AK 时启动 fail-fast
		logx.Infof("oss: incomplete config ignored (need all of OSS_ENDPOINT, OSS_BUCKET, OSS_ACCESS_KEY_ID, OSS_ACCESS_KEY_SECRET)")
		return &OSSPing{}
	}
	endpoint, err := normalizeOSSEndpoint(endpoint)
	if err != nil {
		return &OSSPing{hostConfigured: true, initErr: err}
	}
	cli, err := oss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		logx.Errorf("oss client init failed: %s", logger.RedactString(err.Error()))
		return &OSSPing{hostConfigured: true, initErr: err}
	}
	// 限制单次 HTTP 往返，降低 Ping 超时后 goroutine/连接长期挂死概率
	cli.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	b, err := cli.Bucket(bucket)
	if err != nil {
		return &OSSPing{hostConfigured: true, initErr: fmt.Errorf("oss bucket: %w", err)}
	}
	return &OSSPing{bucket: b, bucketName: bucket, hostConfigured: true}
}

// normalizeOSSEndpoint 去掉 bucket@ 前缀；补全 https://。
// 形如 <uid>.onaliyun.com 的开通回传串不是 SDK Endpoint，须改用地域域名（如 oss-cn-shenzhen.aliyuncs.com）。
func normalizeOSSEndpoint(endpoint string) (string, error) {
	if i := strings.IndexByte(endpoint, '@'); i >= 0 && i+1 < len(endpoint) {
		endpoint = endpoint[i+1:]
	}
	endpoint = strings.TrimSpace(endpoint)
	lower := strings.ToLower(endpoint)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		u, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("oss endpoint: %w", err)
		}
		if err := rejectNonRegionalOSSHost(u.Hostname()); err != nil {
			return "", err
		}
		if strings.EqualFold(u.Scheme, "http") && env.IsProd() && !ossInsecureAllowed() {
			return "", fmt.Errorf("oss: http endpoint forbidden when APP_ENV=prod (use https://, or OSS_ALLOW_INSECURE=true for break-glass)")
		}
		if strings.EqualFold(u.Scheme, "http") && env.IsProd() && ossInsecureAllowed() {
			logx.Severe("SECURITY: OSS_ALLOW_INSECURE=true with http endpoint in prod (break-glass; prefer https)")
		}
		return endpoint, nil
	}
	if err := rejectNonRegionalOSSHost(endpoint); err != nil {
		return "", err
	}
	return "https://" + endpoint, nil
}

func ossInsecureAllowed() bool {
	v := strings.TrimSpace(os.Getenv("OSS_ALLOW_INSECURE"))
	return strings.EqualFold(v, "true") || v == "1"
}

func rejectNonRegionalOSSHost(host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return fmt.Errorf("oss endpoint empty")
	}
	// 账号 UID.onaliyun.com 不能直接当 OSS Endpoint
	if strings.HasSuffix(host, ".onaliyun.com") && !strings.Contains(host, "oss-") {
		return fmt.Errorf("oss: endpoint %q is not a regional OSS endpoint; set OSS_ENDPOINT=oss-cn-shenzhen.aliyuncs.com (or the bucket region)", host)
	}
	return nil
}

func (o *OSSPing) Configured() bool {
	return o != nil && o.hostConfigured
}

func (o *OSSPing) Ping(ctx context.Context) error {
	if o == nil || !o.hostConfigured {
		return fmt.Errorf("oss not configured")
	}
	if o.initErr != nil {
		return o.initErr
	}
	if o.bucket == nil {
		return fmt.Errorf("oss client nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	go func() {
		_, err := o.bucket.ListObjects(oss.MaxKeys(1))
		if err != nil {
			done <- fmt.Errorf("oss list objects (bucket %q): %w", o.bucketName, err)
			return
		}
		done <- nil
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Close OSS SDK 客户端无显式 Close；占位以便 svc.Close 统一调用。
func (o *OSSPing) Close() error {
	return nil
}
