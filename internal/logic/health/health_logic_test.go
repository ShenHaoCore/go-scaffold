package logic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"micro-scaffold/internal/config"
	"micro-scaffold/internal/svc"
	"micro-scaffold/pkg/health"

	"github.com/stretchr/testify/require"
)

func TestHealthCheckDegradedReturns503(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Config: config.Config{HealthProbe: config.HealthProbeConf{ExposeDependencies: true}},
		Health: &health.Runner{
			Timeout: 0,
			Checkers: map[string]health.Checker{
				"db": func(ctx context.Context) health.CheckResult {
					return health.CheckResult{Name: "db", Status: health.StatusDegraded, Message: "down"}
				},
				"mq": health.MQSkipped,
			},
		},
	}
	rec := httptest.NewRecorder()
	NewHealthLogic(context.Background(), svcCtx).Check(rec)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var report health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	require.Equal(t, health.StatusDegraded, report.Status)
	require.Equal(t, health.StatusDegraded, report.Checks["db"].Status)
	require.Empty(t, report.Checks["db"].Message, "public readiness must redact Message")
}

func TestHealthLiveAlways200(t *testing.T) {
	svcCtx := &svc.ServiceContext{Health: &health.Runner{}}
	rec := httptest.NewRecorder()
	NewHealthLogic(context.Background(), svcCtx).Live(rec)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHealthCheck_IgnoresRequestCancel(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		Health: &health.Runner{
			Timeout: time.Second,
			Checkers: map[string]health.Checker{
				"db": func(ctx context.Context) health.CheckResult {
					return health.CheckResult{Name: "db", Status: health.StatusOK}
				},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟客户端断开
	rec := httptest.NewRecorder()
	NewHealthLogic(ctx, svcCtx).Check(rec)
	require.Equal(t, http.StatusOK, rec.Code)
}
