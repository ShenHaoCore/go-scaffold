package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOSSEndpoint(t *testing.T) {
	ep, err := normalizeOSSEndpoint("oss-cn-shenzhen.aliyuncs.com")
	require.NoError(t, err)
	require.Equal(t, "https://oss-cn-shenzhen.aliyuncs.com", ep)

	ep, err = normalizeOSSEndpoint("https://oss-cn-shenzhen.aliyuncs.com")
	require.NoError(t, err)
	require.Equal(t, "https://oss-cn-shenzhen.aliyuncs.com", ep)

	_, err = normalizeOSSEndpoint("mybucket@0000000000000000.onaliyun.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "regional OSS endpoint")
}

func TestNormalizeOSSEndpoint_HTTPForbiddenInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("OSS_ALLOW_INSECURE", "")
	_, err := normalizeOSSEndpoint("http://oss-cn-shenzhen.aliyuncs.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "http endpoint forbidden")

	t.Setenv("OSS_ALLOW_INSECURE", "true")
	ep, err := normalizeOSSEndpoint("http://oss-cn-shenzhen.aliyuncs.com")
	require.NoError(t, err)
	require.Equal(t, "http://oss-cn-shenzhen.aliyuncs.com", ep)
}

func TestNewOSSPing_Incomplete(t *testing.T) {
	// 仅 AK / 缺 Endpoint·Bucket → 视为未配置（skipped），避免 Secret 半开启动 fail-fast
	p := NewOSSPing("https://oss.example.com", "b", "id", "")
	require.False(t, p.Configured())
	require.Error(t, p.Ping(nil))

	empty := NewOSSPing("", "", "", "")
	require.False(t, empty.Configured())

	onlyAK := NewOSSPing("", "", "id", "secret")
	require.False(t, onlyAK.Configured())

	bad := NewOSSPing("0000000000000000.onaliyun.com", "mybucket", "id", "secret")
	require.True(t, bad.Configured())
	require.Error(t, bad.Ping(nil))
}
