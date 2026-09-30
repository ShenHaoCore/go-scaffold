package grpcx

import (
	"context"
	"errors"
	"io"
	"runtime/debug"

	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/logger"
	"go-scaffold/pkg/response"
	"go-scaffold/pkg/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// 服务端 Interceptor 强制顺序（与 HTTP 一致；外→内）：
//
//	[go-zero 自带] → Recovery → Trace → Lang → Error → Handler
//
// Error 必须在最内层：它只负责翻译 handler 的真实返回值，
// 放在外侧会把上层拦截器自己产生的错误也一并改写。
// Unary / Stream 均须注册；见 RegisterServerInterceptors。
//
// 「go-zero 自带」那一段来自 zrpc.MustNewServer 内部的 setupUnaryInterceptors /
// setupStreamInterceptors —— 它先把自己的 Trace/Recover/Stat/Prometheus/Breaker/
// Shedding/Timeout 装好，我们再 Add*Interceptors 追加（zrpc internal 的实现是
// slice append，见 zrpc/internal/server.go）。因此我们的链整体位于内侧，
// 而 Breaker / Shedding / Timeout 直接产生的失败**不经过** response.GRPCError：
// 调用方拿到的是传输层错误，没有 x-error-code、也没有 ErrorInfo.Reason。
// 这是有意保留的保护（与 HTTP 侧 BodylessBuiltinMiddlewares 同一类取舍），
// 要完全统一只能在 yaml 里关掉对应项。Trace / Recover 已由 config 强制关闭，
// 避免双轨 trace 与 panic 文案泄漏。

// InterceptorRegistrar zrpc.RpcServer 满足此接口。
type InterceptorRegistrar interface {
	AddUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor)
	AddStreamInterceptors(interceptors ...grpc.StreamServerInterceptor)
}

// RegisterServerInterceptors 按强制顺序注册 Unary + Stream 链。
func RegisterServerInterceptors(s InterceptorRegistrar) {
	s.AddUnaryInterceptors(
		UnaryServerRecoverInterceptor,
		UnaryServerTraceInterceptor,
		UnaryServerLangInterceptor,
		UnaryServerErrorInterceptor,
	)
	s.AddStreamInterceptors(
		StreamServerRecoverInterceptor,
		StreamServerTraceInterceptor,
		StreamServerLangInterceptor,
		StreamServerErrorInterceptor,
	)
}

// collector 收集拦截器，供非 zrpc 场景（如 bufconn 集成测）构建 grpc.ServerOption。
type collector struct {
	unary  []grpc.UnaryServerInterceptor
	stream []grpc.StreamServerInterceptor
}

func (c *collector) AddUnaryInterceptors(is ...grpc.UnaryServerInterceptor) {
	c.unary = append(c.unary, is...)
}

func (c *collector) AddStreamInterceptors(is ...grpc.StreamServerInterceptor) {
	c.stream = append(c.stream, is...)
}

// ServerOptions 返回与 RegisterServerInterceptors 同序的 grpc.ServerOption（Unary+Stream）。
func ServerOptions() []grpc.ServerOption {
	var c collector
	RegisterServerInterceptors(&c)
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(c.unary...),
		grpc.ChainStreamInterceptor(c.stream...),
	}
}

func stack() string { return string(debug.Stack()) }

// UnaryServerRecoverInterceptor panic → COM1001（via response.GRPCError），trailer 带 x-error-code / x-trace-id。
// 进入内层前先 InjectIncomingFromMD：与后续 Trace 幂等复用同一 tid，避免 panic 时外层 ctx 再 NewID。
func UnaryServerRecoverInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	ctx = trace.InjectIncomingFromMD(ctx)
	defer func() {
		if rec := recover(); rec != nil {
			logger.WithContext(ctx).Errorf("panic recovered: %v\n%s", rec, stack())
			resp = nil
			err = response.GRPCError(ctx, bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil))
		}
	}()
	return handler(ctx, req)
}

// UnaryServerTraceInterceptor 从入站 metadata 注入 trace（与 HTTP Trace 中间件对称）。
func UnaryServerTraceInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx = trace.InjectIncomingFromMD(ctx)
	return handler(ctx, req)
}

// UnaryServerLangInterceptor 从 metadata「lang」写入 context（与 HTTP Lang 中间件对称）。
func UnaryServerLangInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(MetadataLang); len(vals) > 0 && vals[0] != "" {
			ctx = bizerr.WithLang(ctx, vals[0])
		}
	}
	return handler(ctx, req)
}

// UnaryServerErrorInterceptor 把 handler 返回的错误翻译成带错误码的 gRPC status。
//
// 为什么必须有：gRPC 对 handler 返回的**非 status error** 一律降级为 codes.Unknown，
// 并把 err.Error() 原样塞进 status message。于是 `*errors.BizError` 过网后变成
// `code = Unknown desc = "COM2001: 参数 name 为必填"`——错误码退化成裸字符串，
// ErrorInfo.Reason 与 trailer 双双丢失，调用方再也无法结构化还原。本拦截器补上这一环。
//
// 放行规则（不改写，保留原始语义）：
//   - nil → nil
//   - io.EOF → 原样返回（gRPC 流式约定的正常结束信号）
//   - 已是 gRPC status → 原样返回（多半来自下游调用，改写会把真实故障面盖成 COM1001）
//
// 其余一律交给 response.GRPCError：BizError 按错误码映射，普通 error → COM1001。
func UnaryServerErrorInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if err == nil || isPassthroughErr(err) {
		return resp, err
	}
	return nil, response.GRPCError(ctx, err)
}

// isPassthroughErr 判断错误是否应原样过网（见 UnaryServerErrorInterceptor 的放行规则）。
//
// 只用**直接类型断言**判断"是否已是 gRPC status"，刻意不顺着 Unwrap 下钻：
// 客户端拦截器产出的 *BizError 会把原始 status 作为 cause 挂在身上，
// 若按错误链判定就会把中继来的业务错误误认成"已是 status"而原样过网，
// 错误码会二次丢失（回到 codes.Unknown + 裸字符串）。
func isPassthroughErr(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	_, ok := err.(interface{ GRPCStatus() *status.Status })
	return ok
}
