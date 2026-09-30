package model

import (
	"testing"
	"time"

	"micro-scaffold/pkg/logger"

	"github.com/stretchr/testify/require"
)

func TestNewSLSWriter_Empty(t *testing.T) {
	w := NewSLSWriter(SLSOptions{})
	require.False(t, w.Configured())
	require.Error(t, w.Ping(nil))
	_, err := w.Write([]byte("x"))
	require.Error(t, err)
}

func TestNewSLSWriter_Incomplete(t *testing.T) {
	w := NewSLSWriter(SLSOptions{
		Endpoint:    "cn-shenzhen-internal.log.aliyuncs.com",
		Project:     "myproject",
		Logstore:    "mylogstore",
		AccessKeyID: "id",
		// missing secret
	})
	require.False(t, w.Configured())
}

func TestNormalizeSLSEndpoint(t *testing.T) {
	ep, err := normalizeSLSEndpoint("cn-shenzhen-internal.log.aliyuncs.com")
	require.NoError(t, err)
	require.Equal(t, "https://cn-shenzhen-internal.log.aliyuncs.com", ep)
	ep, err = normalizeSLSEndpoint("https://cn-shenzhen-internal.log.aliyuncs.com")
	require.NoError(t, err)
	require.Equal(t, "https://cn-shenzhen-internal.log.aliyuncs.com", ep)
}

func TestNormalizeSLSEndpoint_HTTPForbiddenInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("SLS_ALLOW_INSECURE", "")
	_, err := normalizeSLSEndpoint("http://cn-shenzhen.log.aliyuncs.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "http endpoint forbidden")

	t.Setenv("SLS_ALLOW_INSECURE", "true")
	ep, err := normalizeSLSEndpoint("http://cn-shenzhen.log.aliyuncs.com")
	require.NoError(t, err)
	require.Equal(t, "http://cn-shenzhen.log.aliyuncs.com", ep)

	// 未配置 SLS_ALLOW_INSECURE 时 prod 建 writer 须带 initErr（探活失败而非静默明文）
	t.Setenv("SLS_ALLOW_INSECURE", "")
	w := NewSLSWriter(SLSOptions{
		Endpoint:        "http://cn-shenzhen.log.aliyuncs.com",
		Project:         "myproject",
		Logstore:        "mylogstore",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
	})
	require.True(t, w.Configured())
	require.Error(t, w.Ping(nil))
}

func TestSLSWriter_CloseNilSafe(t *testing.T) {
	var w *SLSWriter
	require.NoError(t, w.Close())
	require.Nil(t, NewSLSLogxWriter(nil))
	require.Nil(t, NewSLSLogxWriter(NewSLSWriter(SLSOptions{})))
}

func TestBuildSLSContents_Schema(t *testing.T) {
	logger.Configure("scaffold", "test")
	contents := buildSLSContents("info", "hello", map[string]string{
		"trace_id":   "tid-1",
		"service":    "pms",
		"env":        "test",
		"caller":     "foo.go:1",
		"level_hint": "warn",
	})
	m := map[string]string{}
	for _, c := range contents {
		require.NotNil(t, c.Key)
		require.NotNil(t, c.Value)
		k, v := *c.Key, *c.Value
		_, dup := m[k]
		require.False(t, dup, "duplicate SLS key %q", k)
		m[k] = v
	}
	require.Equal(t, "warn", m["level"], "level_hint=warn must promote level")
	require.Equal(t, "hello", m["content"])
	require.NotContains(t, m, "message")
	require.Equal(t, "pms", m["service"])
	require.Equal(t, "test", m["env"])
	require.Equal(t, "tid-1", m["trace_id"])
	require.Equal(t, "foo.go:1", m["caller"])
	require.NotContains(t, m, "level_hint")
	require.NotEmpty(t, m["@timestamp"])
	_, err := time.Parse("2006-01-02T15:04:05.000Z07:00", m["@timestamp"])
	require.NoError(t, err)
}

func TestBuildSLSContents_MessageAliasToContent(t *testing.T) {
	contents := buildSLSContents("info", "body", map[string]string{"message": "override"})
	m := map[string]string{}
	for _, c := range contents {
		m[*c.Key] = *c.Value
	}
	require.Equal(t, "override", m["content"])
	require.NotContains(t, m, "message")
}

func TestSLSLogxWriter_CloseDoesNotCloseSharedWriter(t *testing.T) {
	w := NewSLSWriter(SLSOptions{
		Endpoint:        "cn-shenzhen-internal.log.aliyuncs.com",
		Project:         "myproject",
		Logstore:        "mylogstore",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
	})
	require.True(t, w.Configured())
	lx := NewSLSLogxWriter(w)
	require.NotNil(t, lx)
	require.NoError(t, lx.Close())
	// logx Stop 后 svc 仍应能 Close 真正的 SLS 客户端
	require.NoError(t, w.Close())
}

func TestNewSLSWriter_ConfiguredShape(t *testing.T) {
	w := NewSLSWriter(SLSOptions{
		Endpoint:        "cn-shenzhen-internal.log.aliyuncs.com",
		Project:         "myproject",
		Logstore:        "mylogstore",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
	})
	require.True(t, w.Configured())
	require.NoError(t, w.Close())
	require.Error(t, w.Ping(nil))
}
