// Package sdkinterfaces Phase 2 SDK 公开接口草案。
// 正式实现落在独立 Go Module；本目录仅契约，禁止在 pkg/sdk/* 放实现。
package sdkinterfaces

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// ---------- 云资源：只做连接 + Ping，不做 Repository ----------

// RDSClient 云 RDS / 托管 PostgreSQL 客户端。
// DB() 返回已配置连接池；**仅供 internal/model 经 sqlx 注入使用**，业务 logic 禁止直接拿 *sql.DB 写 SQL。
type RDSClient interface {
	DB() *sql.DB
	Ping(ctx context.Context) error
	Close() error
}

// RedisClient 云 Redis 客户端。
type RedisClient interface {
	Ping(ctx context.Context) error
	Close() error
}

// OSSClient 云对象存储客户端（连接 + 桶可达性；业务 Put/Get 由后续 Module 扩展）。
type OSSClient interface {
	Ping(ctx context.Context) error
	Close() error
}

// ---------- 权限：SDK 侧类型；脚手架用适配器映射到 pkg/auth.Authenticator ----------

// AuthClient 权限 SDK 客户端（由独立的权限 Module 实现，业务仓注入）。
type AuthClient interface {
	Verify(ctx context.Context, token string) (*Identity, error)
	CheckPermission(ctx context.Context, identity *Identity, resource, action string) error
}

// Identity 权限服务返回的主体。
// Extra 仅供适配器内部；业务 logic 禁止直接读 Extra 键，稳定字段须升 Principal/Identity 显式字段。
type Identity struct {
	Subject string
	Roles   []string
	Extra   map[string]string
}

// ---------- 可观测：不替换 pkg/logger；仅 Writer / Redactor 实现 ----------

// SLSWriter 对接 logx 额外 Writer；业务仍只调 logger.WithContext。
type SLSWriter interface {
	Write(p []byte) (n int, err error)
	Close() error
}

// 埋点上报不在本包定义：属独立 Module（track.Client）。
// 脚手架 go.mod / ServiceContext 不引入；由业务服务自行挂载。

// ---------- MQ：加 MQ 前缀是为了不与本包内其他 Producer/Consumer 命名冲突 ----------

// MQHandler 消费回调（须含 topic，与 MQ 侧 Handler 基线一致）。
type MQHandler func(ctx context.Context, topic string, body []byte) error

// MQProducer RocketMQ 生产者。
type MQProducer interface {
	Send(ctx context.Context, topic string, body []byte) error
	Ping(ctx context.Context) error
}

// MQConsumer RocketMQ 消费者。
type MQConsumer interface {
	Subscribe(ctx context.Context, topic, group string, h MQHandler) error
	Ping(ctx context.Context) error
}

// ---------- Adapter：超时 + 可选重试；不做熔断/服务发现 ----------

// HTTPAdapter 出站 HTTP 骨架。
type HTTPAdapter interface {
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}

// HTTPAdapterConfig Adapter 配置边界。
type HTTPAdapterConfig struct {
	Timeout       string // 必填，如 3s
	RetryMax      int    // 0=关闭
	RetryOnStatus []int  // 可选
}

// CallOption RPC 出站可选参数（风格对齐标准库 Option；具体实现由业务仓落地）。
type CallOption func(*CallOptions)

// CallOptions RPC 调用选项。
type CallOptions struct {
	Timeout  time.Duration
	RetryMax int
	Headers  map[string]string
}

// WithTimeout 设置单次调用超时（占位；实现由业务仓提供，不强制独立 Module）。
func WithTimeout(d time.Duration) CallOption {
	return func(o *CallOptions) {
		if o != nil {
			o.Timeout = d
		}
	}
}

// WithRetry 设置最大重试次数；0=关闭。
func WithRetry(max int) CallOption {
	return func(o *CallOptions) {
		if o != nil {
			o.RetryMax = max
		}
	}
}

// WithHeader 追加出站 Header / metadata。
func WithHeader(key, value string) CallOption {
	return func(o *CallOptions) {
		if o == nil {
			return
		}
		if o.Headers == nil {
			o.Headers = make(map[string]string)
		}
		o.Headers[key] = value
	}
}

// RPCAdapter 出站 RPC 骨架（超时 + 可选重试 + metadata/Trace 透传）。
type RPCAdapter interface {
	Call(ctx context.Context, method string, req, resp any, opts ...CallOption) error
}
