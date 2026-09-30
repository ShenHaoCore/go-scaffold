package logger_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go-scaffold/pkg/logger"
	"go-scaffold/pkg/trace"

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

// 消息体脱敏：JSON 引号键、带空格或 %v 结构体形态、Basic/Cookie 头、DSN/Redis URL。
func TestDefaultMaskRedactor_MessagePatterns(t *testing.T) {
	cases := []struct {
		name string
		in   string
		gone string // 该子串不得出现在脱敏结果里
	}{
		{"JSON 引号键", `{"user":"u","password":"pw-LEAK-1"}`, "pw-LEAK-1"},
		{"引号内含空格的值", `password: "pw LEAK 2"`, "pw LEAK 2"},
		{"驼峰键（%v 结构体）", `{Endpoint:oss.aliyuncs.com AccessKeySecret:AK-LEAK-3}`, "AK-LEAK-3"},
		{"下划线键", `access_key_secret=AK-LEAK-4`, "AK-LEAK-4"},
		{"Authorization Basic", `headers=map[Authorization:[Basic dXNlcjpwd0xFQUs1]]`, "dXNlcjpwd0xFQUs1"},
		{"Authorization Bearer", `Authorization: Bearer tok-LEAK-6`, "tok-LEAK-6"},
		{"Cookie", `Cookie: sid=sid-LEAK-7; theme=dark`, "sid-LEAK-7"},
		{"Redis URL", `dial redis://:pw-LEAK-8@127.0.0.1:6379 failed`, "pw-LEAK-8"},
		{"DSN", `dial postgres://u:pw-LEAK-9@h:5432/db failed`, "pw-LEAK-9"},
	}
	r := logger.DefaultMaskRedactor{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := r.RedactMessage(c.in)
			require.NotContains(t, out, c.gone, "明文不得留在脱敏结果里")
			require.Contains(t, out, "***")
		})
	}
}

// 不含敏感形态的正文必须原样保留，脱敏器不得成为日志噪声源。
func TestDefaultMaskRedactor_KeepsCleanText(t *testing.T) {
	const in = `user=alice method=GET status=200 latency=12ms`
	require.Equal(t, in, logger.DefaultMaskRedactor{}.RedactMessage(in))
}

// 字段名变体：驼峰 / 下划线 / 连字符都必须被识别；非敏感键不得误伤。
func TestDefaultMaskRedactor_KeyVariants(t *testing.T) {
	r := logger.DefaultMaskRedactor{}
	for _, key := range []string{
		"password", "db_password", "db-pass", "pwd", "db_pass", "dbpass",
		"accessKeySecret", "ACCESS-KEY-SECRET", "access_key_secret", "x-api-key", "apiKey",
		"privateKey", "private_key", "cookie", "set-cookie", "sessionId",
		"authorization", "credential", "DB_DSN",
	} {
		out := r.Redact([]logx.LogField{logx.Field(key, "v")})
		require.Equal(t, "***", fmt.Sprint(out[0].Value), "key %q 应被掩码", key)
	}
	for _, key := range []string{"name", "status", "trace_id", "service", "env", "count", "endpoint"} {
		out := r.Redact([]logx.LogField{logx.Field(key, "v")})
		require.Equal(t, "v", fmt.Sprint(out[0].Value), "key %q 不应被掩码", key)
	}
}

// 非字符串字段值（error / struct）也要过扫描；无敏感内容时不改变字段类型。
func TestDefaultMaskRedactor_NonStringValues(t *testing.T) {
	type ossConf struct{ Endpoint, AccessKeyID, AccessKeySecret string }
	r := logger.DefaultMaskRedactor{}
	out := r.Redact([]logx.LogField{
		logx.Field("cfg", ossConf{
			Endpoint:        "oss-cn-shenzhen.aliyuncs.com",
			AccessKeyID:     "LTAI-id",
			AccessKeySecret: "AK-LEAK-struct",
		}),
		logx.Field("err", errors.New("dial postgres://u:pw-LEAK-err@h/db failed")),
		logx.Field("attempt", 3),
		logx.Field("ok", true),
	})
	require.NotContains(t, fmt.Sprint(out[0].Value), "AK-LEAK-struct", "结构体字段值须被扫描")
	require.NotContains(t, fmt.Sprint(out[1].Value), "pw-LEAK-err", "error 文案须被扫描")
	require.Equal(t, 3, out[2].Value, "无敏感形态时不得改变字段类型")
	require.Equal(t, true, out[3].Value, "无敏感形态时不得改变字段类型")
}
