package response

import (
	"context"
	"net/http"

	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/logger"
	"micro-scaffold/pkg/trace"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	MDErrorCode = "x-error-code"
	MDTraceID   = "x-trace-id"
)

// reasonTrailerMismatchWarn Reason 与 trailer 不一致时告警；单测可替换（export_test.go）。
var reasonTrailerMismatchWarn = func(reason, trailer string) {
	logger.WithContext(context.Background()).Warnf(
		"FromGRPCError: ErrorInfo.Reason=%s != trailer x-error-code=%s; prefer Reason",
		reason, trailer)
}

// GRPCError 将 error 映射为 gRPC status；错误码写入 details（ErrorInfo.Reason）与 trailing metadata。
func GRPCError(ctx context.Context, err error) error {
	tid := trace.FromContext(ctx)
	log := logger.WithContext(ctx)

	if err == nil {
		log.Warn("response.GRPCError called with nil error")
		err = bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil)
	}

	if be, ok := bizerr.AsBizError(err); ok {
		return toGRPCStatus(ctx, be, tid)
	}

	log.Errorf("unhandled error -> COM1001: %+v", err)
	be := bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil)
	return toGRPCStatus(ctx, be, tid)
}

func toGRPCStatus(ctx context.Context, be *bizerr.BizError, tid string) error {
	if err := grpc.SetTrailer(ctx, metadata.Pairs(
		MDErrorCode, be.Code,
		MDTraceID, tid,
	)); err != nil {
		logger.WithContext(ctx).Warnf("grpc.SetTrailer failed: %v", err)
	}

	st := status.New(grpcCode(be.Code), be.Message)
	info := &errdetails.ErrorInfo{
		Reason: be.Code,
		Domain: "scaffold",
	}
	if st2, err := st.WithDetails(info); err == nil {
		return st2.Err()
	}
	return st.Err()
}

// FromGRPCError 将 gRPC status 还原为 BizError。
// 优先 ErrorInfo.Reason；其次 trailers 中的 x-error-code（客户端须 grpc.Trailer 捕获后传入）。
func FromGRPCError(err error, trailers ...metadata.MD) error {
	if err == nil {
		return nil
	}
	if _, ok := bizerr.AsBizError(err); ok {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	code := ""
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() != "" {
			code = info.GetReason()
			break
		}
	}
	trailerCode := ""
	for _, md := range trailers {
		if vals := md.Get(MDErrorCode); len(vals) > 0 && vals[len(vals)-1] != "" {
			trailerCode = vals[len(vals)-1]
			break
		}
	}
	if code == "" {
		code = trailerCode
	} else if trailerCode != "" && trailerCode != code {
		reasonTrailerMismatchWarn(code, trailerCode)
	}
	if code == "" {
		return err
	}
	// 保留原始 status 作为 cause：调用方仍可用 status.FromError 取到线上 gRPC code，
	// 不因"统一成 BizError"而丢掉传输层信息。
	be := &bizerr.BizError{
		Code:    code,
		Message: st.Message(),
		HTTP:    bizerr.HTTPStatus(code),
	}
	return be.WithCause(err)
}

// httpToGRPC HTTP 状态码 → gRPC code 的一一对照表。
//
// 这里是**纯对照**，不含任何错误码解析逻辑：错误码的语义（类型位、覆盖项）
// 统一由 pkg/errors 注册表决定，本包只把它的 HTTP 结果翻译成 gRPC 结果。
// 之所以经 HTTP 中转而不是各自解析类型位，是为了让「同一份规则只写一遍」——
// 否则新增业务码时，HTTP 与 gRPC 两侧的类型位 switch 极易漂移。
var httpToGRPC = map[int]codes.Code{
	http.StatusInternalServerError: codes.Internal,
	http.StatusBadRequest:          codes.InvalidArgument,
	http.StatusUnprocessableEntity: codes.FailedPrecondition,
	http.StatusConflict:            codes.Aborted,
	http.StatusUnauthorized:        codes.Unauthenticated,
	http.StatusForbidden:           codes.PermissionDenied,
	http.StatusTooManyRequests:     codes.ResourceExhausted,
}

// grpcCode 由 pkg/errors.HTTPStatus 推导 gRPC code。
// 契约：未登记的 HTTP 状态码一律按 codes.Internal 处理，不猜测。
func grpcCode(code string) codes.Code {
	if c, ok := httpToGRPC[bizerr.HTTPStatus(code)]; ok {
		return c
	}
	return codes.Internal
}
