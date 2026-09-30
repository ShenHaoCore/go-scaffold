package health_test

import (
	"context"
	"testing"
	"time"

	"go-scaffold/pkg/health"

	"github.com/stretchr/testify/require"
)

func TestRunnerTimeoutDegraded(t *testing.T) {
	r := &health.Runner{
		Timeout: 50 * time.Millisecond,
		Checkers: map[string]health.Checker{
			"slow": func(ctx context.Context) health.CheckResult {
				select {
				case <-time.After(200 * time.Millisecond):
					return health.CheckResult{Status: health.StatusOK}
				case <-ctx.Done():
					return health.CheckResult{Status: health.StatusDegraded, Message: "timeout"}
				}
			},
			"mq": health.MQSkipped,
		},
	}
	report := r.Run(context.Background())
	require.Equal(t, health.StatusDegraded, report.Status)
	require.Equal(t, health.StatusSkipped, report.Checks["mq"].Status)
}

func TestRunnerCheckerPanicDegraded(t *testing.T) {
	r := &health.Runner{
		Timeout: time.Second,
		Checkers: map[string]health.Checker{
			"boom": func(ctx context.Context) health.CheckResult {
				panic("boom")
			},
		},
	}
	report := r.Run(context.Background())
	require.Equal(t, health.StatusDegraded, report.Status)
	require.Equal(t, health.StatusDegraded, report.Checks["boom"].Status)
}

func TestRedactForPublic(t *testing.T) {
	r := health.Report{
		Status: health.StatusOK,
		Checks: map[string]health.CheckResult{
			"db": {Name: "db", Status: health.StatusOK, Message: "secret host"},
		},
	}
	pub := r.RedactForPublic()
	require.Empty(t, pub.Checks["db"].Message)
}

func TestSummaryOnly(t *testing.T) {
	r := health.Report{
		Status: health.StatusDegraded,
		Checks: map[string]health.CheckResult{
			"db": {Name: "db", Status: health.StatusDegraded},
		},
	}
	s := r.SummaryOnly()
	require.Equal(t, health.StatusDegraded, s.Status)
	require.Empty(t, s.Checks)
}
