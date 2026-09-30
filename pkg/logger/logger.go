package logger

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"micro-scaffold/pkg/trace"

	"github.com/zeromicro/go-zero/core/logx"
)

// Redactor 脱敏挂载点；默认 Noop；test|prod 由 ApplyEnvRedactor 注入 DefaultMaskRedactor。
type Redactor interface {
	Redact(fields []logx.LogField) []logx.LogField
}

// NoopRedactor 原样返回字段（不做脱敏）。
type NoopRedactor struct{}

func (NoopRedactor) Redact(fields []logx.LogField) []logx.LogField { return fields }

var (
	globalRedactor atomic.Value // *redactorHolder
	metaService    atomic.Value // string
	metaEnv        atomic.Value // string
)

type redactorHolder struct{ r Redactor }

func init() {
	globalRedactor.Store(&redactorHolder{r: NoopRedactor{}})
	metaService.Store("scaffold")
	metaEnv.Store("dev")
}

// Configure 设置规范日志公共字段（service / env）；在 logx.MustSetup 之后由 main 调用。
func Configure(service, appEnv string) {
	service = trimOr(service, "scaffold")
	appEnv = trimOr(appEnv, "dev")
	metaService.Store(service)
	metaEnv.Store(appEnv)
}

func trimOr(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return s
}

func currentService() string {
	if v, ok := metaService.Load().(string); ok && v != "" {
		return v
	}
	return "scaffold"
}

func currentEnv() string {
	if v, ok := metaEnv.Load().(string); ok && v != "" {
		return v
	}
	return "dev"
}

// ServiceName 当前规范字段 service（供 SLS Writer 等复用）。
func ServiceName() string { return currentService() }

// EnvName 当前规范字段 env。
func EnvName() string { return currentEnv() }

// SetRedactor 替换全局脱敏器（可在 main 注入）。
func SetRedactor(r Redactor) {
	if r == nil {
		r = NoopRedactor{}
	}
	globalRedactor.Store(&redactorHolder{r: r})
}

func currentRedactor() Redactor {
	if v := globalRedactor.Load(); v != nil {
		if h, ok := v.(*redactorHolder); ok && h != nil && h.r != nil {
			return h.r
		}
	}
	return NoopRedactor{}
}

// Logger 带 trace_id 的结构化日志封装；业务侧统一使用，禁止裸 logx.Info 漏字段。
// 禁止在本包调用 Setup/MustSetup；级别与输出仅由 main 中 logx.MustSetup 决定。
// 强制 Field 变参形态（Infow/Errorw/...）；禁止对外暴露链式 Fields API。
// 规范字段：trace_id、service、env（每次调用注入）。
type Logger struct {
	ctx context.Context
}

// WithContext 返回带 ctx 的 Logger；每次 Info/Error/... 调用时从 ctx 读取 trace_id（空串亦输出）。
// 注意：Logger 持有调用时的 ctx 引用；之后对原变量做 WithTimeout 等派生不会自动更新已持有的 ctx
// （context 不可变，此为预期行为）。需要新 deadline 时应 logger.WithContext(newCtx)。
func WithContext(ctx context.Context) Logger {
	if ctx == nil {
		ctx = context.Background()
	}
	return Logger{ctx: ctx}
}

func (l Logger) withTrace(fields []logx.LogField) []logx.LogField {
	tid := trace.FromContext(l.ctx) // 空 tid 仍追加 trace_id=""
	out := append([]logx.LogField{
		logx.Field("trace_id", tid),
		logx.Field("service", currentService()),
		logx.Field("env", currentEnv()),
	}, fields...)
	return currentRedactor().Redact(out)
}

func (l Logger) redactMsg(msg string) string {
	if r, ok := currentRedactor().(interface{ RedactMessage(string) string }); ok {
		return r.RedactMessage(msg)
	}
	// DefaultMaskRedactor：经 Field 路径统一处理 message
	redacted := currentRedactor().Redact([]logx.LogField{logx.Field("_msg", msg)})
	if len(redacted) == 1 {
		if s, ok := redacted[0].Value.(string); ok {
			return s
		}
	}
	return msg
}

// Info 变参 Field 形态。
func (l Logger) Info(msg string, fields ...logx.LogField) {
	logx.WithContext(l.ctx).Infow(l.redactMsg(msg), l.withTrace(fields)...)
}

// Infof 格式化便捷方法；内部仍走 Field 变参，并对 format 结果脱敏。
func (l Logger) Infof(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...))
}

// Error 变参 Field 形态。
func (l Logger) Error(msg string, fields ...logx.LogField) {
	logx.WithContext(l.ctx).Errorw(l.redactMsg(msg), l.withTrace(fields)...)
}

// Errorf 格式化便捷方法。
func (l Logger) Errorf(format string, args ...any) {
	l.Error(fmt.Sprintf(format, args...))
}

// Warn 变参 Field 形态（logx 无独立 Warn 级别，用 Infow + level_hint）。
func (l Logger) Warn(msg string, fields ...logx.LogField) {
	fields = append(fields, logx.Field("level_hint", "warn"))
	logx.WithContext(l.ctx).Infow(l.redactMsg(msg), l.withTrace(fields)...)
}

// Warnf 格式化便捷方法。
func (l Logger) Warnf(format string, args ...any) {
	l.Warn(fmt.Sprintf(format, args...))
}

// Debug 变参 Field 形态。
func (l Logger) Debug(msg string, fields ...logx.LogField) {
	logx.WithContext(l.ctx).Debugw(l.redactMsg(msg), l.withTrace(fields)...)
}

// Debugf 格式化便捷方法。
func (l Logger) Debugf(format string, args ...any) {
	l.Debug(fmt.Sprintf(format, args...))
}

// Slow 变参 Field 形态。
func (l Logger) Slow(msg string, fields ...logx.LogField) {
	logx.WithContext(l.ctx).Sloww(l.redactMsg(msg), l.withTrace(fields)...)
}
