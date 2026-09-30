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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

func TestStreamServerRecover_ReturnsCOM1001(t *testing.T) {
	base := trace.WithTrace(context.Background(), "stream-panic-tid")
	sts := &trailerTransportStream{}
	ctx := grpc.NewContextWithServerTransportStream(base, sts)
	err := grpcx.StreamServerRecoverInterceptor(nil, &mockServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/t.T/S"},
		func(srv any, stream grpc.ServerStream) error {
			panic("stream-boom")
		})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	var reason string
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			reason = info.GetReason()
		}
	}
	require.Equal(t, bizerr.CodeInternal, reason)
	require.Equal(t, []string{bizerr.CodeInternal}, sts.trailer.Get(response.MDErrorCode))
	require.Equal(t, []string{"stream-panic-tid"}, sts.trailer.Get(response.MDTraceID))
}

// trailerTransportStream 供单测捕获 grpc.SetTrailer。
type trailerTransportStream struct {
	grpc.ServerTransportStream
	trailer metadata.MD
}

func (t *trailerTransportStream) Method() string                  { return "/t.T/S" }
func (t *trailerTransportStream) SetHeader(md metadata.MD) error  { return nil }
func (t *trailerTransportStream) SendHeader(md metadata.MD) error { return nil }
func (t *trailerTransportStream) SetTrailer(md metadata.MD) error {
	t.trailer = metadata.Join(t.trailer, md)
	return nil
}

func TestStreamServerChain_TraceBeforeLang(t *testing.T) {
	var sawTrace, sawLang string
	info := &grpc.StreamServerInfo{FullMethod: "/t.T/S"}
	handler := func(srv any, stream grpc.ServerStream) error {
		sawTrace = trace.FromContext(stream.Context())
		sawLang = bizerr.LangFromContext(stream.Context())
		return nil
	}
	langWrapped := func(srv any, stream grpc.ServerStream) error {
		return grpcx.StreamServerLangInterceptor(srv, stream, info, handler)
	}
	traceWrapped := func(srv any, stream grpc.ServerStream) error {
		return grpcx.StreamServerTraceInterceptor(srv, stream, info, langWrapped)
	}
	md := metadata.Pairs(trace.MetadataKey, "stream-tid", grpcx.MetadataLang, "en-US")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	err := grpcx.StreamServerRecoverInterceptor(nil, &mockServerStream{ctx: ctx}, info, traceWrapped)
	require.NoError(t, err)
	require.Equal(t, "stream-tid", sawTrace)
	require.Equal(t, "en-US", sawLang)
}

func TestStreamServerChain_PanicTidStableWithoutIncomingMD(t *testing.T) {
	var beforeTrace, afterTrace string
	info := &grpc.StreamServerInfo{FullMethod: "/t.T/S"}
	inner := func(srv any, stream grpc.ServerStream) error {
		afterTrace = trace.FromContext(stream.Context())
		panic("stream-boom")
	}
	traceWrapped := func(srv any, stream grpc.ServerStream) error {
		beforeTrace = trace.FromContext(stream.Context())
		return grpcx.StreamServerTraceInterceptor(srv, stream, info, inner)
	}
	err := grpcx.StreamServerRecoverInterceptor(nil, &mockServerStream{ctx: context.Background()}, info, traceWrapped)
	require.Error(t, err)
	require.NotEmpty(t, beforeTrace)
	require.Equal(t, beforeTrace, afterTrace)
}

type stubRegistrar struct {
	unary  int
	stream int
}

func (s *stubRegistrar) AddUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) {
	s.unary += len(interceptors)
}
func (s *stubRegistrar) AddStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) {
	s.stream += len(interceptors)
}

func TestRegisterServerInterceptors_RegistersBothChains(t *testing.T) {
	var r stubRegistrar
	grpcx.RegisterServerInterceptors(&r)
	// Recovery → Trace → Lang → Error
	require.Equal(t, 4, r.unary)
	require.Equal(t, 4, r.stream)
}

func TestServerOptions_MatchesRegisterOrder(t *testing.T) {
	opts := grpcx.ServerOptions()
	require.Len(t, opts, 2)
}
