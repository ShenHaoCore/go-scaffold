package cloud_test

import (
	"testing"

	"go-scaffold/pkg/cloud"

	"github.com/stretchr/testify/require"
)

func TestPreferOSSInternal(t *testing.T) {
	require.Equal(t, "oss-cn-shenzhen-internal.aliyuncs.com",
		cloud.PreferOSSInternal("oss-cn-shenzhen.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "https://oss-cn-shenzhen-internal.aliyuncs.com",
		cloud.PreferOSSInternal("https://oss-cn-shenzhen.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "oss-cn-shenzhen-internal.aliyuncs.com",
		cloud.PreferOSSInternal("oss-cn-shenzhen-internal.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "", cloud.PreferOSSInternal("", "cn-shenzhen"))
	// 已是私网 / dualstack：不得再插 -internal
	require.Equal(t, "oss-cn-shenzhen-intranet.aliyuncs.com",
		cloud.PreferOSSInternal("oss-cn-shenzhen-intranet.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "oss-cn-shenzhen.dualstack.aliyuncs.com",
		cloud.PreferOSSInternal("oss-cn-shenzhen.dualstack.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "127.0.0.1", cloud.PreferOSSInternal("127.0.0.1", "cn-shenzhen"))
	// 跨地域公网 Endpoint：不得改写为其它区的 -internal（本 VPC 不可达）
	require.Equal(t, "oss-cn-hangzhou.aliyuncs.com",
		cloud.PreferOSSInternal("oss-cn-hangzhou.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "https://oss-cn-hangzhou.aliyuncs.com",
		cloud.PreferOSSInternal("https://oss-cn-hangzhou.aliyuncs.com", "cn-shenzhen"))
	// 带 query 的 Endpoint：改写 host 时须保留 query
	require.Equal(t, "oss-cn-shenzhen-internal.aliyuncs.com?x=1",
		cloud.PreferOSSInternal("oss-cn-shenzhen.aliyuncs.com?x=1", "cn-shenzhen"))
	require.Equal(t, "https://cn-shenzhen-internal.log.aliyuncs.com/path?x=1",
		cloud.PreferSLSInternal("https://cn-shenzhen.log.aliyuncs.com/path?x=1", "cn-shenzhen"))
}

func TestPreferSLSInternal(t *testing.T) {
	require.Equal(t, "cn-shenzhen-internal.log.aliyuncs.com",
		cloud.PreferSLSInternal("cn-shenzhen.log.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "cn-shenzhen-internal.log.aliyuncs.com",
		cloud.PreferSLSInternal("cn-shenzhen-internal.log.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "https://cn-shenzhen-internal.log.aliyuncs.com",
		cloud.PreferSLSInternal("https://cn-shenzhen.log.aliyuncs.com", ""))
	// 控制台常见私网：不得改写成 *-internal
	require.Equal(t, "cn-shenzhen-intranet.log.aliyuncs.com",
		cloud.PreferSLSInternal("cn-shenzhen-intranet.log.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "cn-shenzhen.dualstack.log.aliyuncs.com",
		cloud.PreferSLSInternal("cn-shenzhen.dualstack.log.aliyuncs.com", "cn-shenzhen"))
	require.Equal(t, "cn-hangzhou-internal.log.aliyuncs.com",
		cloud.PreferSLSInternal("cn-hangzhou.log.aliyuncs.com", "cn-hangzhou"))
	// 跨地域：与 OSS 同守卫，不得改写为其它区的 -internal（本 VPC 不可达）
	require.Equal(t, "cn-hangzhou.log.aliyuncs.com",
		cloud.PreferSLSInternal("cn-hangzhou.log.aliyuncs.com", "cn-shenzhen"))
}
