package logic

import (
	"context"
	"encoding/json"
	"net/http"

	"go-scaffold/internal/svc"
	"go-scaffold/pkg/health"
)

type HealthLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHealthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HealthLogic {
	return &HealthLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *HealthLogic) Check(w http.ResponseWriter) {
	// 探针不受客户端断开影响：父请求取消时仍跑完 HealthCheckTimeout 内检查，避免误报 503。
	runCtx := context.WithoutCancel(l.ctx)
	report := l.svcCtx.Health.Run(runCtx)
	status := http.StatusOK
	if report.Status == health.StatusDegraded || report.Status == health.StatusDown {
		status = http.StatusServiceUnavailable
	}
	// 对外去掉 Message；prod 默认可再隐藏依赖名（仅整体 status）
	public := report.RedactForPublic()
	if !l.svcCtx.Config.HealthProbe.ExposeDependencies {
		public = report.SummaryOnly()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(public)
}

func (l *HealthLogic) Live(w http.ResponseWriter) {
	report := health.Live()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(report)
}
