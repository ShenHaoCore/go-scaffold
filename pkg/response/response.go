package response

import (
	"context"
	"net/http"

	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/logger"
	"go-scaffold/pkg/trace"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// Body 统一 HTTP 响应体。
type Body struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Detail  any    `json:"detail,omitempty"`
	TraceID string `json:"trace_id"`
}

// Success 成功响应：code=0。
// 改签名须同步 templates/api/handler.tpl
func Success(ctx context.Context, w http.ResponseWriter, data any) {
	traceID := trace.FromContext(ctx)
	w.Header().Set("X-Trace-Id", traceID)
	httpx.OkJsonCtx(ctx, w, Body{
		Code:    0,
		Message: "success",
		Data:    data,
		TraceID: traceID,
	})
}

// Error 失败响应；BizError 提取字段，普通 error 映射 COM1001（不暴露底层细节）。
// 原始 error 仅在本函数记日志；handler 禁止对同一失败再打一遍业务错误日志。
// 改签名须同步 templates/api/handler.tpl
func Error(ctx context.Context, w http.ResponseWriter, err error) {
	traceID := trace.FromContext(ctx)
	w.Header().Set("X-Trace-Id", traceID)
	log := logger.WithContext(ctx)

	if err == nil {
		log.Warn("response.Error called with nil error")
		err = bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil)
	}

	if be, ok := bizerr.AsBizError(err); ok {
		status := be.HTTP
		if status == 0 {
			status = bizerr.HTTPStatus(be.Code)
		}
		log.Infof("biz error code=%s message=%s", be.Code, be.Message)
		httpx.WriteJsonCtx(ctx, w, status, Body{
			Code:    be.Code,
			Message: be.Message,
			Detail:  be.Detail,
			TraceID: traceID,
		})
		return
	}

	// 普通 / Wrap 后的 error：响应 COM1001，日志记原始 err（含 %+v 链路）
	log.Errorf("unhandled error -> COM1001: %+v", err)
	be := bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil)
	httpx.WriteJsonCtx(ctx, w, http.StatusInternalServerError, Body{
		Code:    be.Code,
		Message: be.Message,
		TraceID: traceID,
	})
}
