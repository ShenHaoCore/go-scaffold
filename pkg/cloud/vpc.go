// Package cloud 提供阿里云 VPC 相关约定（地域、网段、内网 Endpoint）。
// 配置真源见 config/*.yaml 的 Cloud 段与环境变量 CLOUD_*；NetworkPolicy 须与 VPCCidr 对齐。
package cloud

import (
	"net/url"
	"strings"
)

const (
	// DefaultRegion 测试/生产默认地域（深圳）。
	DefaultRegion = "cn-shenzhen"
	// DefaultVPCCidr 私有 VPC 常见网段（deploy NetworkPolicy 须与此对齐）。
	DefaultVPCCidr = "172.16.0.0/16"
)

// Defaults 返回未覆盖时的 Cloud 默认值。
func Defaults() (region, cidr string) {
	return DefaultRegion, DefaultVPCCidr
}

func isAlreadyPrivateAliyunHost(lower string) bool {
	return strings.Contains(lower, "-internal.") ||
		strings.Contains(lower, "-intranet.") ||
		strings.Contains(lower, ".dualstack.")
}

// PreferOSSInternal 将公网地域 Endpoint 转为 VPC 内网（-internal）。
// 仅当 Endpoint 地域与 region 一致时改写：跨地域的 oss-<mid>.aliyuncs.com 改写为
// 其它区的 -internal 域名在本 VPC 内不可达，须保持原样（避免启动 Ping 误连失败）。
// 已是 internal/intranet/dualstack / 自定义域名 / 空串则原样返回。
func PreferOSSInternal(endpoint, region string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return endpoint
	}
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		region = DefaultRegion
	}
	host, prefix, suffix := splitEndpointHost(endpoint)
	if host == "" {
		return endpoint
	}
	lower := strings.ToLower(host)
	if isAlreadyPrivateAliyunHost(lower) {
		return endpoint
	}
	// 仅改写纯公网形态 oss-<region>.aliyuncs.com（无额外子域/私网标记，且地域匹配）
	if strings.HasPrefix(lower, "oss-") && strings.HasSuffix(lower, ".aliyuncs.com") {
		mid := strings.TrimSuffix(strings.TrimPrefix(lower, "oss-"), ".aliyuncs.com")
		if mid != "" && !strings.Contains(mid, ".") &&
			!strings.Contains(mid, "internal") && !strings.Contains(mid, "intranet") &&
			mid == region {
			return prefix + "oss-" + mid + "-internal.aliyuncs.com" + suffix
		}
	}
	return endpoint
}

// PreferSLSInternal 将 SLS 公网 Endpoint 转为 VPC 内网。
// cn-shenzhen.log.aliyuncs.com → cn-shenzhen-internal.log.aliyuncs.com
// 仅当 Endpoint 地域与 region 一致时改写（与 PreferOSSInternal 同守卫：跨区 -internal
// 域名在本 VPC 不可达）；已是 -internal / -intranet / dualstack 则原样返回。
func PreferSLSInternal(endpoint, region string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return endpoint
	}
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		region = DefaultRegion
	}
	host, prefix, suffix := splitEndpointHost(endpoint)
	if host == "" {
		return endpoint
	}
	lower := strings.ToLower(host)
	if isAlreadyPrivateAliyunHost(lower) {
		return endpoint
	}
	// 仅 {region}.log.aliyuncs.com 且 region 段无点、无 private 标记
	if strings.HasSuffix(lower, ".log.aliyuncs.com") {
		reg := strings.TrimSuffix(lower, ".log.aliyuncs.com")
		if reg != "" && reg == region && !strings.Contains(reg, ".") &&
			!strings.Contains(reg, "internal") && !strings.Contains(reg, "intranet") {
			return prefix + reg + "-internal.log.aliyuncs.com" + suffix
		}
	}
	return endpoint
}

func splitEndpointHost(endpoint string) (host, prefix, suffix string) {
	lower := strings.ToLower(endpoint)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" {
			return endpoint, "", ""
		}
		host = u.Hostname()
		prefix = u.Scheme + "://"
		if u.Port() != "" {
			suffix = ":" + u.Port()
		}
		if u.Path != "" && u.Path != "/" {
			suffix += u.Path
		}
		// 保留 query：改写仅替换 host，不得丢参数（如带签名/参数的 Endpoint）
		if u.RawQuery != "" {
			suffix += "?" + u.RawQuery
		}
		return host, prefix, suffix
	}
	// host:port 或纯 host（可带路径/查询；改写仅替换 host）
	if i := strings.IndexAny(endpoint, "/?"); i >= 0 {
		return endpoint[:i], "", endpoint[i:]
	}
	return endpoint, "", ""
}
