package grpcx_test

import (
	"context"
	"testing"

	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/grpcx"
	"go-scaffold/pkg/response"
	"go-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryServerRecover_ReturnsCOM1001AndTrace(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "panic-tid")
	_, err := grpcx.UnaryServerRecoverInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/t.T/M"},
		func(ctx context.Context, req any) (any, error) {
			panic("boom")
		})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Internal, st.Code())
	var reason string
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			reason = info.GetReason()
		}
	}
	require.Equal(t, bizerr.CodeInternal, reason)
}

func TestUnaryServerChain_TraceBeforeLang(t *testing.T) {
	var sawTrace, sawLang string
	info := &grpc.UnaryServerInfo{FullMethod: "/t.T/M"}
	handler := grpc.UnaryHandler(func(ctx context.Context, req any) (any, error) {
		sawTrace = trace.FromContext(ctx)
		sawLang = bizerr.LangFromContext(ctx)
		return "ok", nil
	})
	// 外→内：Recover → Trace → Lang → handler（与生产注册顺序一致）
	langWrapped := grpc.UnaryHandler(func(ctx context.Context, req any) (any, error) {
		return grpcx.UnaryServerLangInterceptor(ctx, req, info, handler)
	})
	traceWrapped := grpc.UnaryHandler(func(ctx context.Context, req any) (any, error) {
		return grpcx.UnaryServerTraceInterceptor(ctx, req, info, langWrapped)
	})
	md := metadata.Pairs(trace.MetadataKey, "chain-tid", grpcx.MetadataLang, "en-US")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := grpcx.UnaryServerRecoverInterceptor(ctx, nil, info, traceWrapped)
	require.NoError(t, err)
	require.Equal(t, "chain-tid", sawTrace)
	require.Equal(t, "en-US", sawLang)
}

func TestUnaryServerRecover_MapsViaGRPCError(t *testing.T) {
	_, err := grpcx.UnaryServerRecoverInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/t.T/M"},
		func(ctx context.Context, req any) (any, error) {
			panic("x")
		})
	require.Error(t, err)
	mapped := response.FromGRPCError(err)
	be, ok := bizerr.AsBizError(mapped)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeInternal, be.Code)
}

// 无入站 metadata：Recover 预注入后 Trace 须复用同一 tid（panic 不得再 NewID）。
func TestUnaryServerChain_PanicTidStableWithoutIncomingMD(t *testing.T) {
	var beforeTrace, afterTrace string
	info := &grpc.UnaryServerInfo{FullMethod: "/t.T/M"}
	inner := grpc.UnaryHandler(func(ctx context.Context, req any) (any, error) {
		afterTrace = trace.FromContext(ctx)
		panic("boom")
	})
	traceWrapped := grpc.UnaryHandler(func(ctx context.Context, req any) (any, error) {
		beforeTrace = trace.FromContext(ctx)
		return grpcx.UnaryServerTraceInterceptor(ctx, req, info, inner)
	})
	_, err := grpcx.UnaryServerRecoverInterceptor(context.Background(), nil, info, traceWrapped)
	require.Error(t, err)
	require.NotEmpty(t, beforeTrace, "Recover 须在进入 Trace 前注入 tid")
	require.Equal(t, beforeTrace, afterTrace, "Trace 须幂等复用 Recover 预注入的 tid")
}
