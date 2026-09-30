package model

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go-scaffold/pkg/env"
	"go-scaffold/pkg/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// RedisPing Redis 连通检查（实现 repo.RedisPinger）。
type RedisPing struct {
	Client         *redis.Redis
	hostConfigured bool
	initErr        error
}

// NewRedisPing 创建 Redis 探针。typ 对应 go-zero RedisConf.Type（node|cluster；空按 node）。
// user 非空时按阿里云自定义账号规则将 Pass 组装为 user:pass（go-zero 仅支持 Pass）。
// prod 且配置了 Host 时：须 tls=true，或显式 REDIS_ALLOW_INSECURE=true。
func NewRedisPing(host, typ, user, pass string, tls bool) *RedisPing {
	if host == "" {
		return &RedisPing{}
	}
	if env.IsProd() && !tls && !redisInsecureAllowed() {
		return &RedisPing{
			hostConfigured: true,
			initErr:        fmt.Errorf("redis: TLS required when APP_ENV=prod (set RedisConf.Tls / REDIS_TLS=true, or REDIS_ALLOW_INSECURE=true for break-glass)"),
		}
	}
	if strings.TrimSpace(typ) == "" {
		typ = "node"
	}
	authPass := ComposeAliyunRedisPass(user, pass)
	r, err := redis.NewRedis(redis.RedisConf{
		Host: host,
		Type: typ,
		Pass: authPass,
		Tls:  tls,
	})
	if err != nil {
		logx.Errorf("redis client init failed: %s", logger.RedactString(err.Error()))
		return &RedisPing{hostConfigured: true, initErr: err}
	}
	return &RedisPing{Client: r, hostConfigured: true}
}

// ComposeAliyunRedisPass 阿里云 Redis/Tair 自定义账号：AUTH 使用 user:password 单字段。
// user 为空则原样返回 pass（默认账号仅密码）。
func ComposeAliyunRedisPass(user, pass string) string {
	user = strings.TrimSpace(user)
	pass = strings.TrimSpace(pass)
	if user == "" {
		return pass
	}
	prefix := user + ":"
	if strings.HasPrefix(pass, prefix) {
		return pass
	}
	return prefix + pass
}

func redisInsecureAllowed() bool {
	v := strings.TrimSpace(os.Getenv("REDIS_ALLOW_INSECURE"))
	return strings.EqualFold(v, "true") || v == "1"
}

func (r *RedisPing) Configured() bool {
	return r != nil && r.hostConfigured
}

func (r *RedisPing) Ping(ctx context.Context) error {
	if r == nil || !r.hostConfigured {
		return fmt.Errorf("redis not configured")
	}
	if r.initErr != nil {
		return r.initErr
	}
	if r.Client == nil {
		return fmt.Errorf("redis client nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.Client.PingCtx(ctx) {
		return fmt.Errorf("redis ping failed")
	}
	return nil
}

// Close Phase1：go-zero Redis 客户端无 Close；进程退出由 OS 回收。
func (r *RedisPing) Close() error {
	return nil
}
