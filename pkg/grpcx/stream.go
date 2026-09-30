package grpcx

import (
	"context"

	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/logger"
	"micro-scaffold/pkg/response"
	"micro-scaffold/pkg/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ctxServerStream 覆盖 Context，供 Stream Trace/Lang 注入。
type ctxServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *ctxServerStream) Context() context.Context { return s.ctx }

// StreamServerRecoverInterceptor panic → COM1001（与 Unary 同口径）。
// 进入内层前先注入 trace 并包装 Stream.Context，保证 panic trailer 与业务链同一 tid。
func StreamServerRecoverInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	ctx := trace.InjectIncomingFromMD(ss.Context())
	ss = &ctxServerStream{ServerStream: ss, ctx: ctx}
	defer func() {
		if rec := recover(); rec != nil {
			logger.WithContext(ctx).Errorf("stream panic recovered: %v\n%s", rec, stack())
			err = response.GRPCError(ctx, bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil))
		}
	}()
	return handler(srv, ss)
}

// StreamServerTraceInterceptor 从入站 metadata 注入 trace。
func StreamServerTraceInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := trace.InjectIncomingFromMD(ss.Context())
	return handler(srv, &ctxServerStream{ServerStream: ss, ctx: ctx})
}

// StreamServerLangInterceptor 从 metadata「lang」写入 context。
func StreamServerLangInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(MetadataLang); len(vals) > 0 && vals[0] != "" {
			ctx = bizerr.WithLang(ctx, vals[0])
		}
	}
	return handler(srv, &ctxServerStream{ServerStream: ss, ctx: ctx})
}

// StreamServerErrorInterceptor 流式版错误翻译（与 Unary 同口径）。
// 放在链尾：返回的 error 是流的终结状态，改写它等于改写客户端的最终观测结果。
func StreamServerErrorInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	err := handler(srv, ss)
	if err == nil || isPassthroughErr(err) {
		return err
	}
	return response.GRPCError(ss.Context(), err)
}
