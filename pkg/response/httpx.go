package response

import (
	"context"
	"net/http"

	bizerr "go-scaffold/pkg/errors"
	"go-scaffold/pkg/logger"
	"go-scaffold/pkg/trace"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// InstallFrameworkErrorHandler 把 go-zero 框架内部走 httpx.Error / httpx.ErrorCtx 的失败，
// 接到本包的统一响应体上。应在启动时调用一次（cmd/api）。
//
// 不装会怎样：go-zero 在未设置 ErrorHandler 时回退到
// `http.Error(w, err.Error(), 400)` —— 原始 error 文案直接进响应体、状态码恒为 400。
// 这与 error-code-spec.md 的契约（原始 error 绝不进响应体、状态码按错误码推导）冲突。
// 框架内会走这条路的至少有：rest 的 Timeout 中间件，以及任何用 httpx.Error 的 handler。
//
// 装上也管不到的（已知边界，勿误以为已统一）：
//   - Breaker / Shedding / MaxConns / MaxBytes：直接 w.WriteHeader，不写 body，
//     无钩子可用；它们默认开启（保护性），要统一只能改 config 的 Middlewares.* 关掉。
//   - 路由 404：go-zero 走 http.NotFoundHandler()，不经 httpx.Error。
//
// 另：httpx 的 ErrorHandler 是进程级全局，重复调用以最后一次为准（幂等覆盖）。
func InstallFrameworkErrorHandler() {
	httpx.SetErrorHandlerCtx(func(ctx context.Context, err error) (int, any) {
		traceID := trace.FromContext(ctx)
		log := logger.WithContext(ctx)

		if err == nil {
			err = bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil)
		}

		if be, ok := bizerr.AsBizError(err); ok {
			status := be.HTTP
			if status == 0 {
				status = bizerr.HTTPStatus(be.Code)
			}
			log.Infof("framework error code=%s message=%s", be.Code, be.Message)
			return status, Body{
				Code:    be.Code,
				Message: be.Message,
				Detail:  be.Detail,
				TraceID: traceID,
			}
		}

		log.Errorf("unhandled framework error -> COM1001: %+v", err)
		be := bizerr.NewFromContext(ctx, bizerr.CodeInternal, nil)
		return http.StatusInternalServerError, Body{
			Code:    be.Code,
			Message: be.Message,
			TraceID: traceID,
		}
	})
}
