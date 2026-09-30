package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-scaffold/pkg/cloud"
	"go-scaffold/pkg/env"
	"go-scaffold/pkg/logger"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
	"gopkg.in/yaml.v2"
)

// Config 服务配置。
type Config struct {
	rest.RestConf
	HealthCheckTimeout time.Duration      `json:",default=3s"`
	HealthProbe        HealthProbeConf    `json:",optional"`
	Auth               AuthConf           `json:",optional"`
	DB                 DBConf             `json:",optional"`
	RedisConf          RedisConf          `json:",optional"`
	OSS                OSSConf            `json:",optional"`
	MQ                 MQConf             `json:",optional"`
	SLS                SLSConf            `json:",optional"`
	Cloud              CloudConf          `json:",optional"`
	Etcd               EtcdConf           `json:",optional"`
	RpcServer          zrpc.RpcServerConf `json:",optional"`
	RpcClient          RpcClientConf      `json:",optional"`
}

// RpcClientConf 本进程作为 **gRPC 客户端** 时的下游配置。
//
// 与 RpcServer 的关系：RpcServer 决定「我这台怎么被别人调」，RpcClient 决定
// 「我怎么去调别人」。二者独立——HTTP 网关只填 RpcClient，RPC 服务只填 RpcServer，
// 双向都用则都填。Targets 为空时本进程不建立任何出站连接，各自可独立启动。
//
// yaml 示例：
//
//	RpcClient:
//	  Targets:
//	    user:
//	      Endpoints: ["127.0.0.1:9001"]        # 直连（开发/单实例）
//	    order:
//	      Target: "etcd://127.0.0.1:2379/order.rpc"   # 经 etcd 服务发现
type RpcClientConf struct {
	// BlockDial=true 时沿用 zrpc 默认的阻塞拨号：下游不可用则启动失败。
	// 默认 false（非阻塞）：下游不可用只体现为 health rpc:<name>=degraded 与
	// 首次调用报 Unavailable，不拖垮本进程启动——一个下游掉线不该让整个网关起不来。
	BlockDial bool `json:",default=false"`

	// Targets 按服务名索引的下游目标；值为 go-zero 原生 RpcClientConf，
	// 因此 Endpoints / Target / Etcd / Timeout / KeepaliveTime 等字段语义与 zrpc 完全一致。
	Targets map[string]zrpc.RpcClientConf `json:",optional"`
}

// HealthProbeConf HTTP /health 对外细节。
type HealthProbeConf struct {
	// ExposeDependencies=true 时返回各依赖 ok/degraded/skipped；false 仅整体 status（prod 推荐）。
	ExposeDependencies bool `json:",default=true"`
}

// AuthConf 鉴权模式：dev | require | sdk。
type AuthConf struct {
	Mode string `json:",default=dev"`
}

// DBConf 数据库配置（PostgreSQL 18）。
type DBConf struct {
	Driver     string `json:",default=postgres"`
	DataSource string
}

// RedisConf 单机 Redis。
// 阿里云自定义账号（含 Redis 5.0）：填 User + Pass，启动时组装为 AUTH user:pass（go-zero 无 Username 字段）。
type RedisConf struct {
	Host string `json:",optional"`
	Type string `json:",default=node"`
	User string `json:",optional"` // 云 ACL/账号名，如 scaffold_user
	Pass string `json:",optional"`
	Tls  bool   `json:",optional"` // 对应 go-zero RedisConf.Tls
}

// OSSConf 阿里云 OSS（Phase1：连通探活；业务上传走后续模块）。
type OSSConf struct {
	Endpoint        string `json:",optional"` // 如 oss-cn-shenzhen.aliyuncs.com 或开通回传外网 endpoint
	Bucket          string `json:",optional"`
	AccessKeyID     string `json:",optional"`
	AccessKeySecret string `json:",optional"`
}

// MQConf 阿里云 RocketMQ 5.x（Endpoint:8080）。
// 示例：Endpoint=rmq-cn-xxxxx-vpc.<region>.rmq.aliyuncs.com:8080 Topic=<topic> Group=GID_<group>。
type MQConf struct {
	Endpoint        string `json:",optional"`
	InstanceID      string `json:",optional"` // 空则从 Endpoint 推导
	Topic           string `json:",optional"` // 默认发送 Topic
	Group           string `json:",optional"` // 消费组，如 GID_<group>
	AccessKey       string `json:",optional"` // 实例用户名；VPC 免密可空
	AccessKeySecret string `json:",optional"`
}

// SLSConf 阿里云日志服务（Writer + Ping；业务仍走 pkg/logger）。
// 示例：Project=<project> Logstore=<logstore> Endpoint=<region>-internal.log.aliyuncs.com。
type SLSConf struct {
	Endpoint        string `json:",optional"`
	Project         string `json:",optional"`
	Logstore        string `json:",optional"`
	AccessKeyID     string `json:",optional"`
	AccessKeySecret string `json:",optional"`
	Topic           string `json:",optional"`
	Source          string `json:",optional"`
}

// CloudConf 阿里云 VPC 统一约定（地域 / 网段 / 是否优先内网 Endpoint）。
// NetworkPolicy ipBlock 须与 VPCCidr 一致；PreferInternalEndpoint 在 VPC 内将 OSS/SLS 切到 -internal。
type CloudConf struct {
	Region                 string `json:",optional"` // 默认 cn-shenzhen
	VPCCidr                string `json:",optional"` // 默认 172.16.0.0/16
	PreferInternalEndpoint bool   `json:",optional"` // test/prod 建议 true；本地隧道可 false
}

// EtcdConf 配置中心；TLS 三件套齐全时走加密客户端，否则明文（prod 须 ETCD_ALLOW_INSECURE 或配 TLS）。
type EtcdConf struct {
	Hosts    []string `json:",optional"`
	Key      string   `json:",optional"`
	CertFile string   `json:",optional"`
	KeyFile  string   `json:",optional"`
	CAFile   string   `json:",optional"`
}

var (
	global   Config
	globalMu sync.RWMutex
)

// Load 强制顺序：
// 0) 若进程未设 APP_ENV，先 godotenv 载入 .env（不覆盖已有进程变量）再解析 APP_ENV
// 1) 合并 yaml：default.yaml → {APP_ENV}.yaml
// 2) 再 godotenv（幂等）写入其余键
// 3) applyEnvOverrides：环境变量覆盖结构体字段；prod 校验 DSN sslmode
func Load(configDir string) (*Config, error) {
	// 进程已 export 的 APP_ENV 优先；仅当未设置时用 .env 决定 yaml
	if strings.TrimSpace(os.Getenv("APP_ENV")) == "" {
		_ = godotenv.Load(filepath.Join(configDir, ".env"))
		_ = godotenv.Load()
	}
	envName, err := env.ResolveAppEnv()
	if err != nil {
		return nil, err
	}
	// logx.MustSetup 仅在 cmd/*/main 调用；此处 Infof 在 Setup 前亦可（避免 fmt.Printf 污染 stdout）
	logx.Infof("[config] APP_ENV=%s configDir=%s yaml=%s.yaml", envName, configDir, envName)

	merged := map[string]any{}
	defaultFile := filepath.Join(configDir, "default.yaml")
	if err := mergeYAMLFile(merged, defaultFile); err != nil {
		return nil, fmt.Errorf("load default.yaml: %w", err)
	}
	envFile := filepath.Join(configDir, envName+".yaml")
	if _, err := os.Stat(envFile); err == nil {
		if err := mergeYAMLFile(merged, envFile); err != nil {
			return nil, fmt.Errorf("load %s: %w", envFile, err)
		}
	}
	// 本地覆盖：config-local.yaml（可选，最后合并，放不进仓库的敏感项）
	localFile := filepath.Join(configDir, "config-local.yaml")
	if _, err := os.Stat(localFile); err == nil {
		if err := mergeYAMLFile(merged, localFile); err != nil {
			return nil, fmt.Errorf("load config-local.yaml: %w", err)
		}
		logx.Infof("[config] merged config-local.yaml")
	}

	raw, err := yaml.Marshal(normalize(merged))
	if err != nil {
		return nil, err
	}
	var c Config
	if err := conf.LoadFromYamlBytes(raw, &c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if c.HealthCheckTimeout == 0 {
		c.HealthCheckTimeout = 3 * time.Second
	}
	if c.Auth.Mode == "" {
		c.Auth.Mode = "dev"
	}
	if c.RpcServer.ListenOn == "" {
		c.RpcServer.ListenOn = "0.0.0.0:8081"
	}
	if c.RpcServer.Name == "" {
		c.RpcServer.Name = "scaffold-rpc"
	}

	// yaml 已选定；再加载 .env（不覆盖已有进程变量）
	_ = godotenv.Load(filepath.Join(configDir, ".env"))
	_ = godotenv.Load() // 仓库根 cwd 常见布局
	applyEnvOverrides(&c)

	if err := validateDataSourceSSL(c.DB.DataSource); err != nil {
		return nil, err
	}
	if err := validateRpcClient(&c); err != nil {
		return nil, err
	}
	if c.Log.ServiceName == "" {
		c.Log.ServiceName = "scaffold"
	}
	if c.Name == "" {
		c.Name = "scaffold-api"
	}
	normalizeLogConf(&c)
	applyCloudDefaults(&c)
	applyVPCEndpoints(&c)
	// 放在 Load 末尾：确保 yaml/env 无法把内置 Recover/Trace 重新打开
	applyBuiltinMiddlewarePolicy(&c)
	// logx.MustSetup 仅在 cmd/*/main 调用，本函数不 Setup

	globalMu.Lock()
	global = c
	globalMu.Unlock()
	return &c, nil
}

func mergeYAMLFile(dst map[string]any, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var src any
	if err := yaml.Unmarshal(b, &src); err != nil {
		return err
	}
	norm, ok := normalize(src).(map[string]any)
	if !ok {
		return fmt.Errorf("root must be a map: %s", path)
	}
	deepMerge(dst, norm)
	return nil
}

func normalize(v any) any {
	switch t := v.(type) {
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = normalize(val)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = normalize(val)
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	default:
		return v
	}
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if vmap, ok := v.(map[string]any); ok {
			if existing, ok := dst[k].(map[string]any); ok {
				deepMerge(existing, vmap)
				continue
			}
			dst[k] = vmap
			continue
		}
		dst[k] = v
	}
}

func applyEnvOverrides(c *Config) {
	if v := strings.TrimSpace(os.Getenv("AUTH_MODE")); v != "" {
		c.Auth.Mode = strings.ToLower(v)
	}
	if v := os.Getenv("DB_DSN_SQL"); v != "" {
		c.DB.DataSource = v
	} else if user := os.Getenv("DB_USER"); user != "" {
		pass := os.Getenv("DB_PASS")
		host := getenv("DB_HOST", "127.0.0.1")
		port := getenv("DB_PORT", "5432")
		name := getenv("DB_NAME", "scaffold")
		c.DB.DataSource = formatPostgresDSN(user, pass, host, port, name, resolveDBSSLMode())
	}
	if v := os.Getenv("REDIS_HOST"); v != "" {
		c.RedisConf.Host = v
	}
	if v := strings.TrimSpace(os.Getenv("REDIS_TYPE")); v != "" {
		c.RedisConf.Type = v
	}
	if v := os.Getenv("REDIS_USER"); v != "" {
		c.RedisConf.User = v
	}
	if v := os.Getenv("REDIS_PASS"); v != "" {
		c.RedisConf.Pass = v
	}
	if v := os.Getenv("REDIS_TLS"); v != "" {
		c.RedisConf.Tls = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("OSS_ENDPOINT"); v != "" {
		c.OSS.Endpoint = v
	}
	if v := os.Getenv("OSS_BUCKET"); v != "" {
		c.OSS.Bucket = v
	}
	if v := os.Getenv("OSS_ACCESS_KEY_ID"); v != "" {
		c.OSS.AccessKeyID = v
	}
	if v := os.Getenv("OSS_ACCESS_KEY_SECRET"); v != "" {
		c.OSS.AccessKeySecret = v
	}
	if v := os.Getenv("MQ_ENDPOINT"); v != "" {
		c.MQ.Endpoint = v
	}
	if v := os.Getenv("MQ_INSTANCE"); v != "" {
		c.MQ.InstanceID = v
	}
	if v := os.Getenv("MQ_TOPIC"); v != "" {
		c.MQ.Topic = v
	}
	if v := os.Getenv("MQ_GROUP"); v != "" {
		c.MQ.Group = v
	}
	if v := os.Getenv("MQ_ACCESS_KEY"); v != "" {
		c.MQ.AccessKey = v
	}
	if v := os.Getenv("MQ_ACCESS_KEY_SECRET"); v != "" {
		c.MQ.AccessKeySecret = v
	}
	if v := os.Getenv("SLS_ENDPOINT"); v != "" {
		c.SLS.Endpoint = v
	}
	if v := os.Getenv("SLS_PROJECT"); v != "" {
		c.SLS.Project = v
	}
	if v := os.Getenv("SLS_LOGSTORE"); v != "" {
		c.SLS.Logstore = v
	}
	if v := os.Getenv("SLS_ACCESS_KEY_ID"); v != "" {
		c.SLS.AccessKeyID = v
	}
	if v := os.Getenv("SLS_ACCESS_KEY_SECRET"); v != "" {
		c.SLS.AccessKeySecret = v
	}
	if v := os.Getenv("SLS_TOPIC"); v != "" {
		c.SLS.Topic = v
	}
	if v := os.Getenv("SLS_SOURCE"); v != "" {
		c.SLS.Source = v
	}
	if v := os.Getenv("ETCD_HOSTS"); v != "" {
		c.Etcd.Hosts = splitCSV(v)
	}
	if v := os.Getenv("ETCD_CERT_FILE"); v != "" {
		c.Etcd.CertFile = v
	}
	if v := os.Getenv("ETCD_KEY_FILE"); v != "" {
		c.Etcd.KeyFile = v
	}
	if v := os.Getenv("ETCD_CA_FILE"); v != "" {
		c.Etcd.CAFile = v
	}
	if v := os.Getenv("HEALTH_EXPOSE_DEPENDENCIES"); v != "" {
		c.HealthProbe.ExposeDependencies = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("CLOUD_REGION")); v != "" {
		c.Cloud.Region = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUD_VPC_CIDR")); v != "" {
		c.Cloud.VPCCidr = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUD_PREFER_INTERNAL")); v != "" {
		c.Cloud.PreferInternalEndpoint = strings.EqualFold(v, "true") || v == "1"
	}
}

// resolveDBSSLMode：DB_SSLMODE 优先；未设时 prod→verify-full，其余→disable。
// 云 RDS 若无可用 CA，可显式 DB_SSLMODE=require。
func resolveDBSSLMode() string {
	if v := strings.TrimSpace(os.Getenv("DB_SSLMODE")); v != "" {
		return strings.ToLower(v)
	}
	if env.IsProd() {
		return "verify-full"
	}
	return "disable"
}

// formatPostgresDSN 组装 pgx / golang-migrate 可用的 postgres URL。
// 特殊字符用户名/密码经 url.UserPassword 转义；亦可直接设 DB_DSN_SQL。
func formatPostgresDSN(user, pass, host, port, dbname, sslmode string) string {
	if sslmode == "" {
		sslmode = "disable"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + dbname,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

// validateDataSourceSSL：prod 禁止 sslmode=disable/allow/prefer/空（明文）。
func validateDataSourceSSL(dsn string) error {
	if !env.IsProd() || strings.TrimSpace(dsn) == "" {
		return nil
	}
	mode := extractSSLMode(dsn)
	switch mode {
	case "require", "verify-ca", "verify-full":
		return nil
	case "", "disable", "allow", "prefer":
		return fmt.Errorf("DB DataSource sslmode=%q forbidden when APP_ENV=prod (set DB_SSLMODE=require|verify-full or use DB_DSN_SQL with TLS)", mode)
	default:
		return fmt.Errorf("DB DataSource unsupported sslmode=%q", mode)
	}
}

// validateRpcClient 校验下游 gRPC 目标表。
//
//   - 服务名为空：写不出有意义的日志与 health 条目，直接拒；
//   - 三项定位信息（Endpoints / Target / Etcd）全空：zrpc 的 BuildTarget 会在
//     拨号时才失败，错误信息埋在启动几秒后，不如在 Load 处一次说清；
//   - 多项并存：zrpc 的优先级是 Endpoints > Target > Etcd，静默生效容易误配，只告警。
//
// prod 额外提示：zrpc 客户端传输层固定 insecure（RpcClientConf 无 TLS 字段），
// 依赖 VPC 内网隔离；需要 TLS 须自行 zrpc.WithTransportCredentials 覆盖。
func validateRpcClient(c *Config) error {
	if env.IsProd() && len(c.RpcClient.Targets) > 0 {
		logWarnf("rpc client: %d target(s) dialed plaintext (zrpc transport is insecure; rely on VPC isolation)", len(c.RpcClient.Targets))
	}
	for name, tc := range c.RpcClient.Targets {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("RpcClient.Targets has an empty service name")
		}
		hasEndpoints := len(tc.Endpoints) > 0
		hasTarget := strings.TrimSpace(tc.Target) != ""
		hasEtcd := len(tc.Etcd.Hosts) > 0
		if !hasEndpoints && !hasTarget && !hasEtcd {
			return fmt.Errorf("RpcClient.Targets.%s: need one of Endpoints / Target / Etcd", name)
		}
		if (hasEndpoints && hasTarget) || (hasTarget && hasEtcd) || (hasEndpoints && hasEtcd) {
			logWarnf("rpc client %s: multiple locators set (Endpoints/Target/Etcd); zrpc resolves Endpoints > Target > Etcd", name)
		}
	}
	return nil
}

// extractSSLMode 支持 URL query（重复键取最后一个）与 libpq 关键字 DSN（host=.. sslmode=..）。
// URL 形态只认 query 里的 sslmode，不回退关键字扫描：避免密码含 "sslmode=" 子串时被误读，
// 令 prod 明文校验被绕过（fail-open）。关键字形态引号感知：单引号包裹的值整体跳过，
// 且 sslmode= 前须为空白/';'/串首（token 边界），密码值中的 "sslmode=" 不得误报。
func extractSSLMode(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		if raw := u.RawQuery; raw != "" {
			var last string
			for _, part := range strings.Split(raw, "&") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 && strings.EqualFold(kv[0], "sslmode") {
					last = kv[1]
				}
			}
			if last != "" {
				return strings.ToLower(strings.TrimSpace(last))
			}
		}
		return ""
	}
	// keyword-value：引号感知扫描，如 …… sslmode=require ……
	lower := strings.ToLower(dsn)
	const key = "sslmode="
	for i := 0; i < len(lower); i++ {
		if lower[i] == '\'' {
			// 跳过单引号包裹的值（其中任何 key=value 均非连接参数）
			j := i + 1
			for j < len(lower) && lower[j] != '\'' {
				if lower[j] == '\\' {
					j++
				}
				j++
			}
			i = j
			continue
		}
		if strings.HasPrefix(lower[i:], key) && (i == 0 || isDSNTokenBoundary(lower[i-1])) {
			rest := lower[i+len(key):]
			end := len(rest)
			for j, ch := range rest {
				if ch == ' ' || ch == ';' {
					end = j
					break
				}
			}
			val := strings.TrimSpace(rest[:end])
			return strings.Trim(val, "'")
		}
	}
	return ""
}

func isDSNTokenBoundary(c byte) bool {
	return c == ' ' || c == '\t' || c == ';'
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyBuiltinMiddlewarePolicy 关闭与自有链路冲突的 go-zero 内置中间件：
//   - Recover：自有 internal/middleware.RecoveryMiddleware 与 pkg/grpcx 负责，
//     保留会双写响应体、且可能把 panic 文案泄漏进响应；
//   - Trace：自有 pkg/trace + TraceMiddleware 负责，保留会出现两条并行 trace 链路。
//
// 其余内置中间件仍按 go-zero 默认开启（rest.MiddlewaresConf 各项 default=true）。
// 其中 Breaker / Shedding / MaxConns / MaxBytes 命中时直接 w.WriteHeader、不写 body，
// 因此**不会**走 pkg/response 的统一响应体；要完全统一只能在 yaml 里显式关掉，
// 代价是失去对应保护。取舍见 README「框架内置中间件边界」。
func applyBuiltinMiddlewarePolicy(c *Config) {
	c.Middlewares.Recover = false
	c.Middlewares.Trace = false
	c.RpcServer.Middlewares.Recover = false
	c.RpcServer.Middlewares.Trace = false
}

// BodylessBuiltinMiddlewares 返回仍开启、且失败响应为「裸状态码 + 空 body」的内置中间件。
// 供启动日志显式提示：这些路径上的失败不遵守统一响应契约。
func (c *Config) BodylessBuiltinMiddlewares() []string {
	var out []string
	if c.Middlewares.Breaker {
		out = append(out, "Breaker(503)")
	}
	if c.Middlewares.Shedding {
		out = append(out, "Shedding(503)")
	}
	if c.Middlewares.MaxConns {
		out = append(out, "MaxConns(503)")
	}
	if c.Middlewares.MaxBytes {
		out = append(out, "MaxBytes(413)")
	}
	return out
}

// normalizeLogConf：全环境默认 info + JSON 规范字段；空 Level 填 info。
func normalizeLogConf(c *Config) {
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = "info"
	}
	if strings.TrimSpace(c.Log.Encoding) == "" {
		c.Log.Encoding = "json"
	}
	if strings.TrimSpace(c.Log.TimeFormat) == "" {
		c.Log.TimeFormat = "2006-01-02T15:04:05.000Z07:00"
	}
}

func applyCloudDefaults(c *Config) {
	reg, cidr := cloud.Defaults()
	if strings.TrimSpace(c.Cloud.Region) == "" {
		c.Cloud.Region = reg
	}
	if strings.TrimSpace(c.Cloud.VPCCidr) == "" {
		c.Cloud.VPCCidr = cidr
	}
}

// applyVPCEndpoints：PreferInternalEndpoint 时将 OSS/SLS 切到 VPC 内网域名。
func applyVPCEndpoints(c *Config) {
	if !c.Cloud.PreferInternalEndpoint {
		return
	}
	c.OSS.Endpoint = cloud.PreferOSSInternal(c.OSS.Endpoint, c.Cloud.Region)
	c.SLS.Endpoint = cloud.PreferSLSInternal(c.SLS.Endpoint, c.Cloud.Region)
}

// Get 返回当前配置快照（Hosts/Endpoints 等 slice 深拷贝，避免调用方改写全局）。
func Get() Config {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return cloneConfig(global)
}

func cloneConfig(src Config) Config {
	c := src
	if src.Etcd.Hosts != nil {
		c.Etcd.Hosts = append([]string(nil), src.Etcd.Hosts...)
	}
	if src.RpcServer.Etcd.Hosts != nil {
		c.RpcServer.Etcd.Hosts = append([]string(nil), src.RpcServer.Etcd.Hosts...)
	}
	if src.RpcClient.Targets != nil {
		c.RpcClient.Targets = make(map[string]zrpc.RpcClientConf, len(src.RpcClient.Targets))
		for name, tc := range src.RpcClient.Targets {
			if tc.Endpoints != nil {
				tc.Endpoints = append([]string(nil), tc.Endpoints...)
			}
			if tc.Etcd.Hosts != nil {
				tc.Etcd.Hosts = append([]string(nil), tc.Etcd.Hosts...)
			}
			c.RpcClient.Targets[name] = tc
		}
	}
	return c
}

// ApplyHotUpdate 见 hotkeys.go（与 restartRequiredKeysIn 共用 hotFields 真源）。

// logWarnf logx 无独立 Warn 级别，统一用 Infow 风格前缀便于检索；消息经 RedactString。
func logWarnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logx.Infof("[warn] %s", logger.RedactString(msg))
}

func parseLevel(s string) (uint32, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return logx.DebugLevel, true
	case "info", "warn", "warning":
		// logx 无独立 Warn；热更传 warn 时落到 Info（与 pkg/logger.Warn 的 level_hint 策略一致）
		return logx.InfoLevel, true
	case "error", "severe":
		return logx.ErrorLevel, true
	default:
		return logx.InfoLevel, false
	}
}

func normalizeLogLevelName(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "warn", "warning":
		return "info"
	case "severe":
		return "error"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}
