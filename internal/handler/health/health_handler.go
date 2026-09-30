package handler

import (
	"net/http"

	logic "go-scaffold/internal/logic/health"
	"go-scaffold/internal/svc"
)

// Health 手写实现：degraded/down → HTTP 503。
// 不纳入 main.api → make gen，避免被 handler.tpl（pkg/response 恒 200）覆盖。

func HealthHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logic.NewHealthLogic(r.Context(), svcCtx).Check(w)
	}
}

func HealthLiveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logic.NewHealthLogic(r.Context(), svcCtx).Live(w)
	}
}
