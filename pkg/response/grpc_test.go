package response

import (
	"context"
	"errors"
	"net/http"
	"testing"

	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCError_BizError(t *testing.T) {
	ctx := trace.WithTrace(context.Background(), "tid-grpc")
	err := GRPCError(ctx, bizerr.New(bizerr.CodeRequired, "zh-CN", map[string]any{"Field": "name"}))
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())

	var reason string
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			reason = info.Reason
		}
	}
	require.Equal(t, bizerr.CodeRequired, reason)
}

func TestGRPCError_PlainMapsCOM1001(t *testing.T) {
	ctx := context.Background()
	err := GRPCError(ctx, errors.New("boom"))
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Internal, st.Code())

	var reason string
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			reason = info.Reason
		}
	}
	require.Equal(t, bizerr.CodeInternal, reason)
}

func TestFromGRPCError_RoundTrip(t *testing.T) {
	ctx := context.Background()
	src := bizerr.New(bizerr.CodeInvalidParam, "zh-CN", map[string]any{"Field": "name"})
	grpcErr := GRPCError(ctx, src)
	got := FromGRPCError(grpcErr)
	be, ok := bizerr.AsBizError(got)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeInvalidParam, be.Code)
}

func TestFromGRPCError_PassthroughUnknown(t *testing.T) {
	orig := errors.New("plain")
	require.Equal(t, orig, FromGRPCError(orig))
}

func TestFromGRPCError_TrailerFallback(t *testing.T) {
	st := status.New(codes.InvalidArgument, "bad")
	md := metadata.Pairs(MDErrorCode, bizerr.CodeInvalidParam)
	got := FromGRPCError(st.Err(), md)
	be, ok := bizerr.AsBizError(got)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeInvalidParam, be.Code)
}

func TestFromGRPCError_PreferErrorInfoOverMismatchTrailer(t *testing.T) {
	var sawReason, sawTrailer string
	restore := SetReasonTrailerMismatchWarnForTest(func(reason, trailer string) {
		sawReason, sawTrailer = reason, trailer
	})
	t.Cleanup(restore)

	st := status.New(codes.InvalidArgument, "bad")
	st2, err := st.WithDetails(&errdetails.ErrorInfo{Reason: bizerr.CodeRequired, Domain: "scaffold"})
	require.NoError(t, err)
	md := metadata.Pairs(MDErrorCode, bizerr.CodeInvalidParam) // 故意不一致
	got := FromGRPCError(st2.Err(), md)
	be, ok := bizerr.AsBizError(got)
	require.True(t, ok)
	require.Equal(t, bizerr.CodeRequired, be.Code)
	require.Equal(t, bizerr.CodeRequired, sawReason)
	require.Equal(t, bizerr.CodeInvalidParam, sawTrailer)
}

// 契约：HTTP 侧的覆盖项必须自动传导到 gRPC code。
// 这条锁住「两侧共用同一真源」——旧实现里错误码类型位被解析了两遍，
// 鉴权位的 403/401 细分还得在两边各写一次 if。
func TestGRPCError_HTTPOverridePropagatesToGRPCCode(t *testing.T) {
	const code = "TST4012" // 鉴权位，默认 401
	bizerr.Register(bizerr.Spec{Code: code, HTTP: http.StatusForbidden})

	// 用 NewFromContext 取到登记项推导出的默认 HTTP（403），再走 gRPC 映射
	be := bizerr.New(code, "zh-CN", nil)
	require.Equal(t, http.StatusForbidden, be.HTTP)

	st, ok := status.FromError(GRPCError(context.Background(), be))
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code(), "HTTP 403 应一对一到 PermissionDenied")

	// 未覆盖的鉴权位仍是 401 → Unauthenticated
	st2, ok := status.FromError(GRPCError(context.Background(),
		bizerr.New(bizerr.CodeUnauthorized, "zh-CN", nil)))
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st2.Code())

	// 形非法码 fail-closed 到 Internal，不得猜测类型位
	st3, ok := status.FromError(GRPCError(context.Background(),
		&bizerr.BizError{Code: "BAD", Message: "x"}))
	require.True(t, ok)
	require.Equal(t, codes.Internal, st3.Code())
}
