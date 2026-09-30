package grpcx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/grpcx"
	"go-scaffold/pkg/response"
	"go-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// 本文件用 bufconn（内存连接，真实 HTTP/2 传输，无端口、CI 友好）做端到端验证。
// 验的是"线上格式对不对"：状态码、ErrorInfo.Reason、trailer、客户端还原后的类型与文案、
// 语言与链路的跨进程透传、流式错误的各阶段还原。这些用 fake 都验不出来。

// echoService 空接口：grpc.RegisterService 对 HandlerType 做的是 reflect Implements 检查，
// 空接口恒被满足，因此无需生成 .proto 即可声明服务——集成测试专用。
type echoService interface{}

const (
	svcEcho     = "test.Echo"
	methodOK    = "/test.Echo/OK"
	methodFail  = "/test.Echo/Fail"
	methodPanic = "/test.Echo/Panic"
	methodWhoAm = "/test.Echo/WhoAmI"
	methodList  = "/test.Echo/List"
	methodDrain = "/test.Echo/Drain"
)

// 服务端处理函数签名与生成代码一致（interceptor 由 grpc.ChainUnaryInterceptor 在 Server 层注入）。
func unaryMethod(name string, fn func(ctx context.Context, in *wrapperspb.StringValue) (any, error)) grpc.MethodDesc {
	return grpc.MethodDesc{
		MethodName: name,
		Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			in := new(wrapperspb.StringValue)
			if err := dec(in); err != nil {
				return nil, err
			}
			if interceptor == nil {
				return fn(ctx, in)
			}
			info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + svcEcho + "/" + name}
			return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
				return fn(ctx, req.(*wrapperspb.StringValue))
			})
		},
	}
}

func serverStreamMethod(name string, fn func(stream grpc.ServerStream) error) grpc.StreamDesc {
	return grpc.StreamDesc{
		StreamName:    name,
		ServerStreams: true,
		Handler:       func(srv any, stream grpc.ServerStream) error { return fn(stream) },
	}
}

var echoDesc = grpc.ServiceDesc{
	ServiceName: svcEcho,
	HandlerType: (*echoService)(nil),
	Methods: []grpc.MethodDesc{
		unaryMethod("OK", func(context.Context, *wrapperspb.StringValue) (any, error) {
			return wrapperspb.String("ok"), nil
		}),
		// 业务错误：文案随 ctx 语言变化，用于验证 lang 的跨进程透传
		unaryMethod("Fail", func(ctx context.Context, _ *wrapperspb.StringValue) (any, error) {
			return nil, bizerr.NewFromContext(ctx, bizerr.CodeRequired, map[string]any{"Field": "name"})
		}),
		unaryMethod("Panic", func(context.Context, *wrapperspb.StringValue) (any, error) {
			panic("e2e-boom-secret") // 该文案绝不允许出现在客户端可见的错误里
		}),
		// 回显服务端观测到的链路/语言，用于验证元数据透传
		unaryMethod("WhoAmI", func(ctx context.Context, _ *wrapperspb.StringValue) (any, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			return wrapperspb.String(fmt.Sprintf("%s|%s|%s",
				trace.FromContext(ctx),
				bizerr.LangFromContext(ctx),
				joinFirst(md.Get(trace.MetadataKey)),
			)), nil
		}),
	},
	Streams: []grpc.StreamDesc{
		// 先正常发两条，再以业务错误收尾 → 客户端须在第三条拿到 BizError
		serverStreamMethod("List", func(stream grpc.ServerStream) error {
			var req wrapperspb.StringValue
			if err := stream.RecvMsg(&req); err != nil {
				return err
			}
			for i := 1; i <= 2; i++ {
				if err := stream.SendMsg(wrapperspb.String(fmt.Sprintf("item-%d", i))); err != nil {
					return err
				}
			}
			return bizerr.NewFromContext(stream.Context(), bizerr.CodeUnauthorized, nil)
		}),
		// 正常结束 → 客户端须收到 io.EOF（不得被包装成 BizError）
		serverStreamMethod("Drain", func(stream grpc.ServerStream) error {
			var req wrapperspb.StringValue
			if err := stream.RecvMsg(&req); err != nil {
				return err
			}
			return stream.SendMsg(wrapperspb.String("only"))
		}),
	},
}

func joinFirst(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// newEchoClient 起一个真实传输的服务端 + 走 grpcx 链路的客户端。
func newEchoClient(t *testing.T) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer(grpcx.ServerOptions()...)
	gs.RegisterService(&echoDesc, struct{}{})
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet", append(grpcx.DialOptions(),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestE2E_Unary_ErrorCodeAndTrailerRoundTrip(t *testing.T) {
	conn := newEchoClient(t)
	ctx := bizerr.WithLang(trace.WithTrace(context.Background(), "e2e-tid"), "en-US")

	var out wrapperspb.StringValue
	err := conn.Invoke(ctx, methodOK, wrapperspb.String("ping"), &out)
	require.NoError(t, err)
	require.Equal(t, "ok", out.GetValue())

	// 失败调用：同时用调用方自己的 Trailer 选项捕获（验证我们的追加不覆盖调用方）
	var md metadata.MD
	err = conn.Invoke(ctx, methodFail, wrapperspb.String("x"), &out, grpc.Trailer(&md))
	require.Error(t, err)

	// 线上状态码：COM2001 是参数位 → InvalidArgument
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 跨语言通道：ErrorInfo.Reason 带错误码
	require.Equal(t, bizerr.CodeRequired, reasonOf(t, st))

	// trailer 同步带上错误码与 trace id，且 trace 是客户端透传下去的同一个
	require.Equal(t, []string{bizerr.CodeRequired}, md.Get(response.MDErrorCode))
	require.Equal(t, []string{"e2e-tid"}, md.Get(response.MDTraceID))

	// 客户端侧已还原成 BizError（而非裸 status error），文案随请求语言走
	be, ok := bizerr.AsBizError(err)
	require.True(t, ok, "客户端拦截器须还原为 BizError，实际 %T", err)
	require.Equal(t, bizerr.CodeRequired, be.Code)
	require.Equal(t, "Field name is required", be.Message)
	require.Equal(t, 400, be.HTTP, "HTTP 状态须由注册表推导")

	// 中文请求得到中文文案（语言确实跨进程生效）
	var out2 wrapperspb.StringValue
	errZh := conn.Invoke(bizerr.WithLang(trace.WithTrace(context.Background(), "zh-tid"), "zh-CN"),
		methodFail, wrapperspb.String("x"), &out2)
	beZh, ok := bizerr.AsBizError(errZh)
	require.True(t, ok)
	require.Equal(t, "参数 name 为必填", beZh.Message)
}

func TestE2E_Unary_TraceAndLangMetadataPropagate(t *testing.T) {
	conn := newEchoClient(t)
	ctx := bizerr.WithLang(trace.WithTrace(context.Background(), "prop-tid"), "en-US")

	var out wrapperspb.StringValue
	require.NoError(t, conn.Invoke(ctx, methodWhoAm, wrapperspb.String(""), &out))
	// trace.FromContext | LangFromContext | 入站 metadata 里的 x-trace-id
	require.Equal(t, "prop-tid|en-US|prop-tid", out.GetValue())
}

func TestE2E_Unary_NoTraceIDStillGeneratesOne(t *testing.T) {
	conn := newEchoClient(t)

	var out wrapperspb.StringValue
	require.NoError(t, conn.Invoke(context.Background(), methodWhoAm, wrapperspb.String(""), &out))

	parts := splitPipe(out.GetValue())
	require.Len(t, parts, 3, "WhoAmI 回显格式应为 tid|lang|入站tid")
	require.NotEmpty(t, parts[0], "无 trace 的 context 也须由客户端拦截器补一个 id，避免下游各自生成")
	require.Equal(t, parts[0], parts[2], "入站 metadata 与 context 里的 tid 必须一致")
}

func TestE2E_Unary_PanicBecomesCOM1001WithoutLeaking(t *testing.T) {
	conn := newEchoClient(t)

	var out wrapperspb.StringValue
	err := conn.Invoke(trace.WithTrace(context.Background(), "panic-tid"),
		methodPanic, wrapperspb.String(""), &out)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Internal, st.Code())
	require.Equal(t, bizerr.CodeInternal, reasonOf(t, st))

	be, ok := bizerr.AsBizError(err)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeInternal, be.Code)
	// 内部细节不泄漏：panic 文案与堆栈都不得出现在客户端可见的错误里
	require.NotContains(t, be.Message, "e2e-boom-secret")
	require.NotContains(t, err.Error(), "e2e-boom-secret")
}

func TestE2E_Stream_ErrorTranslatedAndEOFPassthrough(t *testing.T) {
	conn := newEchoClient(t)

	t.Run("流内业务错误在 RecvMsg 阶段被还原", func(t *testing.T) {
		desc := &grpc.StreamDesc{StreamName: "List", ServerStreams: true}
		cs, err := conn.NewStream(context.Background(), desc, methodList)
		require.NoError(t, err)
		require.NoError(t, cs.SendMsg(wrapperspb.String("go")))
		require.NoError(t, cs.CloseSend())

		var got []string
		var recvErr error
		for {
			var v wrapperspb.StringValue
			if recvErr = cs.RecvMsg(&v); recvErr != nil {
				break
			}
			got = append(got, v.GetValue())
		}

		require.Equal(t, []string{"item-1", "item-2"}, got, "错误之前的数据须正常收到")
		require.Error(t, recvErr)
		require.False(t, errors.Is(recvErr, io.EOF), "业务错误不得被误判为流正常结束")

		be, ok := bizerr.AsBizError(recvErr)
		require.True(t, ok, "流式错误也须还原为 BizError，实际 %T", recvErr)
		require.Equal(t, bizerr.CodeUnauthorized, be.Code)

		st, ok := status.FromError(recvErr)
		require.True(t, ok)
		require.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("正常结束仍是 io.EOF", func(t *testing.T) {
		desc := &grpc.StreamDesc{StreamName: "Drain", ServerStreams: true}
		cs, err := conn.NewStream(context.Background(), desc, methodDrain)
		require.NoError(t, err)
		require.NoError(t, cs.SendMsg(wrapperspb.String("go")))
		require.NoError(t, cs.CloseSend())

		var v wrapperspb.StringValue
		require.NoError(t, cs.RecvMsg(&v))
		require.Equal(t, "only", v.GetValue())

		err = cs.RecvMsg(&v)
		require.True(t, errors.Is(err, io.EOF),
			"流正常结束必须原样透传 io.EOF（业务靠它判断收流完成），实际 %v", err)
		_, isBiz := bizerr.AsBizError(err)
		require.False(t, isBiz)
	})
}

func TestNewClient_RequiresExplicitCredentials(t *testing.T) {
	// 不传凭据：grpc 自身即拒绝（不会静默明文），因此不存在"忘配 TLS 就明文上公网"
	_, err := grpcx.NewClient("127.0.0.1:1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no transport security")
}

func TestNewInsecureClient_RefusedInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	_, err := grpcx.NewInsecureClient("127.0.0.1:1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "prod")

	t.Setenv("APP_ENV", "dev")
	conn, err := grpcx.NewInsecureClient("passthrough:///none")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

func TestBizCode(t *testing.T) {
	require.Equal(t, bizerr.CodeInternal, grpcx.BizCode(bizerr.New(bizerr.CodeInternal, "zh-CN", nil)))
	require.Equal(t, "", grpcx.BizCode(errors.New("plain")))
	require.Equal(t, "", grpcx.BizCode(nil))
}

func reasonOf(t *testing.T, st *status.Status) string {
	t.Helper()
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

func splitPipe(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '|' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
