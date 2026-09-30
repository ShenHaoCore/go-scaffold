package grpcx

import (
	"context"
	"errors"
	"fmt"
	"io"

	"micro-scaffold/pkg/env"
	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/response"
	"micro-scaffold/pkg/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// 客户端 Interceptor 强制顺序（外 → 内）：
//
//	Trace → Lang → Error → transport
//
// 与 HTTP 侧 internal/middleware 的 Recovery → Trace → Lang 对齐，差异只在没有 Recovery：
// 客户端 panic 属调用方进程自身缺陷，不该被传输层吞掉。
//
// Error 放在最内层，是为了让更靠外的拦截器（以及业务代码）拿到的一律是 *bizerr.BizError，
// 而不是裸 status error。

// DialOptions 返回与服务端同源的出站拦截器链。
//
// 与 zrpc 共用时注意：grpc 的 WithChainUnaryInterceptor 是**累加**语义
// （见 google.golang.org/grpc/dialoptions.go），go-zero 自己的客户端拦截器在
// zrpc.MustNewClient 内部先注册，因此会位于**外侧**，本链位于内侧。
// 用法：zrpc.MustNewClient(c, zrpc.WithDialOption(grpcx.DialOptions()...))。
// 直接拨号见 NewClient / NewInsecureClient。
func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(
			UnaryClientTraceInterceptor,
			UnaryClientLangInterceptor,
			UnaryClientErrorInterceptor,
		),
		grpc.WithChainStreamInterceptor(
			StreamClientTraceInterceptor,
			StreamClientLangInterceptor,
			StreamClientErrorInterceptor,
		),
	}
}

// NewClient 建立出站连接（不发起 IO，首次 RPC 时才连）。
//
// 凭据必须由调用方显式给出（WithTransportCredentials）：grpc.NewClient 在没有凭据时
// 会直接返回 errNoTransportSecurity，因此不存在「忘了配 TLS 而静默明文」的情况。
// 本地/测试要明文请用 NewInsecureClient。
func NewClient(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, fmt.Errorf("grpcx.NewClient: empty target")
	}
	all := append(DialOptions(), opts...)
	return grpc.NewClient(target, all...)
}

// NewInsecureClient 建立明文出站连接，仅供本地与测试。
// APP_ENV=prod 时直接拒绝——prod 必须走 TLS，避免明文跨网传输。
func NewInsecureClient(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if env.IsProd() {
		return nil, fmt.Errorf(
			"grpcx.NewInsecureClient refused when APP_ENV=prod (use NewClient with TLS transport credentials)")
	}
	all := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, opts...)
	return NewClient(target, all...)
}

// UnaryClientTraceInterceptor 保证出站 metadata 带 x-trace-id：
// context 已有（HTTP 入站解析过 / 上游 gRPC 传入）则原样透传，没有则新建，
// 避免每个下游各自生成 trace id 导致跨服务断链。
func UnaryClientTraceInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(trace.InjectOutgoing(ctx), method, req, reply, cc, opts...)
}

// UnaryClientErrorInterceptor 把 gRPC status 还原为 *bizerr.BizError。
//
// 取码顺序：ErrorInfo.Reason（跨语言通道）→ trailer x-error-code。
// 都取不到时原样返回——连通性类错误（Unavailable / DeadlineExceeded 等）
// 不得被伪装成 COM1001，否则排查时看不到真实故障面。
func UnaryClientErrorInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	var trailer metadata.MD
	// 不可就地 append：调用方传入的 opts 若有剩余容量，就地追加会写到上层共用数组上
	opts = append(append([]grpc.CallOption{}, opts...), grpc.Trailer(&trailer))

	err := invoker(ctx, method, req, reply, cc, opts...)
	if err == nil {
		return nil
	}
	return response.FromGRPCError(err, trailer)
}

// StreamClientTraceInterceptor 流式版 trace 透传；同时把出站 ctx 交给 streamer。
func StreamClientTraceInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(trace.InjectOutgoing(ctx), desc, cc, method, opts...)
}

// StreamClientErrorInterceptor 建立流时即失败的错误还原。
//
// 注意：流建立成功后的 Recv/Send 失败由 errTranslatingStream 在各阶段还原——
// trailer 只有流结束后才完整，所以不能在建立阶段一次性包掉。
func StreamClientErrorInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	var trailer metadata.MD
	opts = append(append([]grpc.CallOption{}, opts...), grpc.Trailer(&trailer))

	cs, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, response.FromGRPCError(err, trailer)
	}
	return &errTranslatingStream{ClientStream: cs}, nil
}

// errTranslatingStream 把 RecvMsg/SendMsg 返回的 gRPC status 还原为 *bizerr.BizError。
// 此时 trailer 已由 transport 填好，可直接读取。
type errTranslatingStream struct {
	grpc.ClientStream
}

func (s *errTranslatingStream) RecvMsg(m any) error {
	return translateStreamErr(s.ClientStream.RecvMsg(m), s.ClientStream)
}

func (s *errTranslatingStream) SendMsg(m any) error {
	return translateStreamErr(s.ClientStream.SendMsg(m), s.ClientStream)
}

// translateStreamErr io.EOF 必须原样透传：它是「流正常结束」的信号，
// 业务侧靠 errors.Is(err, io.EOF) 判断收流完成；包装成 BizError 会破坏该契约。
func translateStreamErr(err error, cs grpc.ClientStream) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	return response.FromGRPCError(err, cs.Trailer())
}

// BizCode 便于调用方快速取错误码；非业务错误返回空串。
func BizCode(err error) string {
	if be, ok := bizerr.AsBizError(err); ok {
		return be.Code
	}
	return ""
}
