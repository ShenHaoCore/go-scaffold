package logger_test

import (
	"context"
	"fmt"
	"testing"

	"micro-scaffold/pkg/logger"
	"micro-scaffold/pkg/trace"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestWithContextEmptyTraceStillWorks(t *testing.T) {
	logx.DisableStat()
	l := logger.WithContext(context.Background())
	require.NotPanics(t, func() {
		l.Info("probe", logx.Field("k", "v"))
		l.Infof("probe %s", "fmt")
	})
}

func TestWithTraceID(t *testing.T) {
	logx.DisableStat()
	ctx := trace.WithTrace(context.Background(), "tid-1")
	require.NotPanics(t, func() {
		logger.WithContext(ctx).Debug("hot-update-level-probe")
	})
}

func TestNoopRedactor(t *testing.T) {
	fields := []logx.LogField{logx.Field("a", 1)}
	out := logger.NoopRedactor{}.Redact(fields)
	require.Equal(t, fields, out)
}

func TestDefaultMaskRedactor_MasksSensitiveKeys(t *testing.T) {
	in := []logx.LogField{
		logx.Field("password", "secret123"),
		logx.Field("user_token", "abc"),
		logx.Field("name", "ok"),
	}
	out := logger.DefaultMaskRedactor{}.Redact(in)
	require.Equal(t, "***", fmt.Sprint(out[0].Value))
	require.Equal(t, "***", fmt.Sprint(out[1].Value))
	require.Equal(t, "ok", fmt.Sprint(out[2].Value))
}

func TestApplyEnvRedactor_ProdUsesMask(t *testing.T) {
	t.Cleanup(func() { logger.SetRedactor(logger.NoopRedactor{}) })
	logger.ApplyEnvRedactor("prod")
	_, ok := logger.CurrentRedactorForTest().(logger.DefaultMaskRedactor)
	require.True(t, ok)
	logger.ApplyEnvRedactor("test")
	_, ok = logger.CurrentRedactorForTest().(logger.DefaultMaskRedactor)
	require.True(t, ok)
	logger.ApplyEnvRedactor("dev")
	_, ok = logger.CurrentRedactorForTest().(logger.NoopRedactor)
	require.True(t, ok)
}

func TestRedactString_MasksDSN(t *testing.T) {
	t.Cleanup(func() { logger.SetRedactor(logger.NoopRedactor{}) })
	logger.SetRedactor(logger.DefaultMaskRedactor{})
	in := "dsn=postgres://u:secret@h/db"
	out := logger.RedactString(in)
	require.NotContains(t, out, "secret")
	require.Contains(t, out, "***")
}
