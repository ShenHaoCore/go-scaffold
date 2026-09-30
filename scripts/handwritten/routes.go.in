package handler

import (
	healthHandler "micro-scaffold/internal/handler/health"
	"micro-scaffold/internal/middleware"
	"micro-scaffold/internal/svc"
	"time"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterHandlers 手写维护（含 health 与中间件链）。
// make gen 后由本文件（routes.go.in）覆盖回 internal/handler/routes.go。
func RegisterHandlers(server *rest.Server, serverCtx *svc.ServiceContext) {
	// 链：Recovery → Trace → Lang → Auth → Agent → Handler（可解析 X-Agent-Id；无 pkg/agent）
	server.Use(middleware.RecoveryMiddleware)
	server.Use(middleware.TraceMiddleware)
	server.Use(middleware.LangMiddleware)
	server.Use(middleware.AuthMiddleware(serverCtx.Auth, "/health/live"))
	server.Use(middleware.AgentMiddleware)

	// health 单独加长超时，避免与 HealthCheckTimeout 顶死 Rest Timeout
	server.AddRoutes([]rest.Route{
		{Method: "GET", Path: "/health", Handler: healthHandler.HealthHandler(serverCtx)},
		{Method: "GET", Path: "/health/live", Handler: healthHandler.HealthLiveHandler(serverCtx)},
	}, rest.WithTimeout(8*time.Second))
}
