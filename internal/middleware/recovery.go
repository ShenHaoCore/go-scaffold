package middleware

import (
	"net/http"
	"runtime/debug"

	bizerr "micro-scaffold/pkg/errors"
	"micro-scaffold/pkg/logger"
	"micro-scaffold/pkg/response"
	"micro-scaffold/pkg/trace"
)

// RecoveryMiddleware panic 恢复，统一走 pkg/response。
// 位于 Trace 之前：优先取响应头已写入的 X-Trace-Id（Trace 已执行时），再解析入站 Header，保证与 Trace 一致。
func RecoveryMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				ctx := r.Context()
				id := trace.FromContext(ctx)
				if id == "" {
					id = w.Header().Get(trace.HeaderTraceID)
				}
				if id == "" {
					id = trace.ParseHTTPHeaders(r.Header.Get)
				}
				ctx = trace.WithTrace(ctx, id)
				logger.WithContext(ctx).Errorf("panic recovered: %v\n%s", rec, debug.Stack())
				response.Error(ctx, w, bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil))
			}
		}()
		next(w, r)
	}
}
