package health

import (
	"context"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// Status 整体或单项状态。
type Status string

const (
	StatusOK       Status = "ok"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
	StatusSkipped  Status = "skipped"
)

// CheckResult 单项检查结果。
type CheckResult struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Report /health 响应体。
type Report struct {
	Status Status                 `json:"status"`
	Checks map[string]CheckResult `json:"checks"`
}

// RedactForPublic 对外 readiness：保留 name/status，去掉 Message，降低基础设施信息暴露。
func (r Report) RedactForPublic() Report {
	out := Report{
		Status: r.Status,
		Checks: make(map[string]CheckResult, len(r.Checks)),
	}
	for k, c := range r.Checks {
		out.Checks[k] = CheckResult{Name: c.Name, Status: c.Status}
	}
	return out
}

// SummaryOnly 仅保留整体 Status（不暴露 db/redis 等依赖名），供公网/prod 探针。
func (r Report) SummaryOnly() Report {
	return Report{Status: r.Status, Checks: map[string]CheckResult{}}
}

// Checker 依赖检查器；必须尊重 ctx 取消/超时，否则超时后 goroutine 可能泄漏。
type Checker func(ctx context.Context) CheckResult

// Runner 带超时的健康检查执行器。
type Runner struct {
	Timeout  time.Duration
	Checkers map[string]Checker
}

// Run 执行全部 checker；超时记为 degraded；任一 degraded → 整体 degraded。
func (r *Runner) Run(ctx context.Context) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	checks := make(map[string]CheckResult, len(r.Checkers))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, checker := range r.Checkers {
		wg.Add(1)
		go func(n string, c Checker) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			done := make(chan CheckResult, 1)
			go func() {
				defer func() {
					if rec := recover(); rec != nil {
						logx.Errorf("health checker %s panic: %v", n, rec)
						done <- CheckResult{Name: n, Status: StatusDegraded, Message: "panic"}
					}
				}()
				done <- c(cctx)
			}()
			var res CheckResult
			select {
			case res = <-done:
			case <-cctx.Done():
				res = CheckResult{Name: n, Status: StatusDegraded, Message: "timeout"}
				// 短暂排空即可；grace 须远小于 Timeout，避免最坏耗时≈2×Timeout 顶死 K8s readiness
				grace := 200 * time.Millisecond
				if timeout < grace {
					grace = timeout
				}
				timer := time.NewTimer(grace)
				select {
				case <-done:
					if !timer.Stop() {
						<-timer.C
					}
				case <-timer.C:
					logx.Errorf("health checker %s still running after timeout grace", n)
				}
			}
			if res.Name == "" {
				res.Name = n
			}
			mu.Lock()
			checks[n] = res
			mu.Unlock()
		}(name, checker)
	}
	wg.Wait()

	overall := StatusOK
	for _, c := range checks {
		switch c.Status {
		case StatusDown:
			overall = StatusDown
		case StatusDegraded:
			if overall != StatusDown {
				overall = StatusDegraded
			}
		}
	}
	return Report{Status: overall, Checks: checks}
}

// Live 仅进程存活。
func Live() Report {
	return Report{
		Status: StatusOK,
		Checks: map[string]CheckResult{
			"process": {Name: "process", Status: StatusOK},
		},
	}
}

// MQSkipped 未配置 MQ 时的兼容占位（已配置时由 svc 真实 Ping）。
func MQSkipped(_ context.Context) CheckResult {
	return CheckResult{Name: "mq", Status: StatusSkipped, Message: "mq not configured"}
}
