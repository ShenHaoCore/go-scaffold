package repo

import (
	"context"
	"errors"
	"math"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// ErrInvalidPage / ErrInvalidPageSize 分页参数非法（业务 logic 映射 COM2002）。
var (
	ErrInvalidPage     = errors.New("invalid page")
	ErrInvalidPageSize = errors.New("invalid page_size")
)

// NormalizePage 校验并规范化分页：page≥1；pageSize∈[1,100]。非法返回 ErrInvalidPage / ErrInvalidPageSize。
// 调用方（HTTP form）应已用 default；不再静默把非法 pageSize 改回默认值，避免掩盖客户端错误。
// page 超出 (MaxInt/pageSize) 时 clamp 到上限：仅防 OFFSET 整数溢出，非掩盖客户端错误。
func NormalizePage(page, pageSize int) (int, int, error) {
	if pageSize < 1 || pageSize > maxPageSize {
		return 0, 0, ErrInvalidPageSize
	}
	if page < 1 {
		return 0, 0, ErrInvalidPage
	}
	maxPage := math.MaxInt / pageSize
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		page = maxPage
	}
	return page, pageSize, nil
}

// PageOffset 安全计算 OFFSET；page/pageSize 须已通过 NormalizePage。
func PageOffset(page, pageSize int) int {
	if page < 1 || pageSize < 1 {
		return 0
	}
	return (page - 1) * pageSize
}

// DefaultPageSize 导出默认页大小（文档/测试用）。
func DefaultPageSize() int { return defaultPageSize }

// DBPinger DB 连通性。
type DBPinger interface {
	Ping(ctx context.Context) error
}

// RedisPinger Redis 连通性。
type RedisPinger interface {
	Ping(ctx context.Context) error
	Configured() bool
}

// OSSPinger 对象存储连通性（HeadBucket）。
type OSSPinger interface {
	Ping(ctx context.Context) error
	Configured() bool
}

// MQProducer RocketMQ 5.x 生产者（Send + Ping）。
type MQProducer interface {
	Send(ctx context.Context, topic string, body []byte) error
	Ping(ctx context.Context) error
	Configured() bool
}

// SLSClient 阿里云 SLS 写入 + 探活（对齐 sdkinterfaces.SLSWriter）。
type SLSClient interface {
	Write(p []byte) (n int, err error)
	WriteFields(level, message string, fields map[string]string) error
	Ping(ctx context.Context) error
	Close() error
	Configured() bool
}
