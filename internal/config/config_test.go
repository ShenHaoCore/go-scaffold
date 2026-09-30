package config_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-scaffold/internal/config"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

func TestLoadDefaults(t *testing.T) {
	logx.Disable()
	dir := filepath.Join("..", "..", "config")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("CLOUD_PREFER_INTERNAL", "")
	c, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, 3*time.Second, c.HealthCheckTimeout)
	require.Equal(t, "dev", c.Auth.Mode)
	require.Equal(t, "info", c.Log.Level, "全环境默认 info")
	require.Equal(t, "json", c.Log.Encoding)
	require.Equal(t, "cn-shenzhen", c.Cloud.Region)
	require.Equal(t, "172.16.0.0/16", c.Cloud.VPCCidr)
	require.False(t, c.Cloud.PreferInternalEndpoint, "dev 默认不强制内网 Endpoint")
	require.NotEmpty(t, c.Name)
	require.False(t, c.Middlewares.Recover, "HTTP 须禁用 go-zero 内置 Recover")
	require.False(t, c.Middlewares.Trace, "HTTP 须禁用 go-zero 内置 Trace，统一 pkg/trace")
	require.False(t, c.RpcServer.Middlewares.Recover)
	require.False(t, c.RpcServer.Middlewares.Trace)
}

func TestLoad_PreferInternalRewritesEndpoints(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "test")
	t.Setenv("CLOUD_PREFER_INTERNAL", "true")
	t.Setenv("OSS_ENDPOINT", "oss-cn-shenzhen.aliyuncs.com")
	t.Setenv("SLS_ENDPOINT", "cn-shenzhen.log.aliyuncs.com")
	dir := filepath.Join("..", "..", "config")
	c, err := config.Load(dir)
	require.NoError(t, err)
	require.True(t, c.Cloud.PreferInternalEndpoint)
	require.Equal(t, "oss-cn-shenzhen-internal.aliyuncs.com", c.OSS.Endpoint)
	require.Equal(t, "cn-shenzhen-internal.log.aliyuncs.com", c.SLS.Endpoint)
}

func TestLoad_ForcesBuiltinMiddlewareOffEvenIfYAMLTrue(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	dir := t.TempDir()
	const defaultYAML = `
Name: force-mw
Host: 0.0.0.0
Port: 8080
Middlewares:
  Recover: true
  Trace: true
RpcServer:
  Name: force-rpc
  ListenOn: 0.0.0.0:8081
  Middlewares:
    Recover: true
    Trace: true
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "default.yaml"),
		[]byte(strings.TrimSpace(defaultYAML)+"\n"),
		0o644,
	))

	c, err := config.Load(dir)
	require.NoError(t, err)

	// yaml 虽写 true，Load 末尾 applyBuiltinMiddlewarePolicy 须强制关闭
	require.False(t, c.Middlewares.Recover, "HTTP Recover")
	require.False(t, c.Middlewares.Trace, "HTTP Trace")
	require.False(t, c.RpcServer.Middlewares.Recover, "RPC Recover")
	require.False(t, c.RpcServer.Middlewares.Trace, "RPC Trace")
}

func TestApplyHotUpdateWhitelist(t *testing.T) {
	logx.Disable()
	dir := filepath.Join("..", "..", "config")
	t.Setenv("APP_ENV", "dev")
	_, err := config.Load(dir)
	require.NoError(t, err)

	config.ApplyHotUpdate(map[string]any{"Log.Level": "debug"})
	got := config.Get()
	require.Equal(t, "debug", got.Log.Level)

	config.ApplyHotUpdate(map[string]any{"Log.Level": "warn"})
	require.Equal(t, "info", config.Get().Log.Level, "warn 热更应规范化为 info")

	before := config.Get().Timeout
	config.ApplyHotUpdate(map[string]any{"Timeout": 9999})
	require.Equal(t, before, config.Get().Timeout, "Timeout must not hot-update")

	hostBefore := config.Get().RedisConf.Host
	config.ApplyHotUpdate(map[string]any{"RedisConf.Host": "evil:6379"})
	require.Equal(t, hostBefore, config.Get().RedisConf.Host, "RedisConf.Host must not hot-update")
}

func TestLoad_AppEnvTestUsesTestYAML(t *testing.T) {
	logx.Disable()
	dir := filepath.Join("..", "..", "config")
	t.Setenv("APP_ENV", "test")
	c, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "require", c.Auth.Mode, "test.yaml 须覆盖 default 的 Mode=dev")
}

func TestLoad_AppEnvFromProcessNotDotenvForYAML(t *testing.T) {
	// 进程已设 APP_ENV 时优先；.env 不得覆盖（godotenv 不覆盖已有变量）
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	dir := t.TempDir()
	const defaultYAML = `
Name: env-select
Host: 0.0.0.0
Port: 8080
Auth:
  Mode: dev
RpcServer:
  Name: env-rpc
  ListenOn: 0.0.0.0:8081
`
	const prodYAML = `
Auth:
  Mode: require
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(strings.TrimSpace(defaultYAML)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prod.yaml"), []byte(strings.TrimSpace(prodYAML)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_ENV=prod\n"), 0o644))

	c, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "dev", c.Auth.Mode, "须用进程 APP_ENV=dev 的 yaml，而非 .env 的 prod")
}

func TestLoad_AppEnvFromDotenvWhenProcessUnset(t *testing.T) {
	logx.Disable()
	_ = os.Unsetenv("APP_ENV")

	dir := t.TempDir()
	const defaultYAML = `
Name: env-dotenv
Host: 0.0.0.0
Port: 8080
Auth:
  Mode: dev
RpcServer:
  Name: env-rpc
  ListenOn: 0.0.0.0:8081
`
	const testYAML = `
Auth:
  Mode: require
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(strings.TrimSpace(defaultYAML)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(strings.TrimSpace(testYAML)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_ENV=test\n"), 0o644))

	c, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "require", c.Auth.Mode, "进程未设 APP_ENV 时须采纳 .env")
}

func TestEnvOverrides_PostgresDSNRoundTripSpecialChars(t *testing.T) {
	logx.Disable()
	dir := filepath.Join("..", "..", "config")
	_ = os.Unsetenv("DB_DSN_SQL")
	_ = os.Unsetenv("DB_SSLMODE")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DB_USER", "u@me")
	t.Setenv("DB_PASS", "p@ss:word/1")
	t.Setenv("DB_HOST", "db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "scaffold")
	c, err := config.Load(dir)
	require.NoError(t, err)

	u, err := url.Parse(c.DB.DataSource)
	require.NoError(t, err)
	require.Equal(t, "postgres", u.Scheme)
	require.Equal(t, "u@me", u.User.Username())
	pass, ok := u.User.Password()
	require.True(t, ok)
	require.Equal(t, "p@ss:word/1", pass)
	require.Equal(t, "db:5432", u.Host)
	require.Equal(t, "/scaffold", u.Path)
	require.Equal(t, "disable", u.Query().Get("sslmode"))
}

func TestLoad_ProdRejectsDisableSSL(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "prod")
	_ = os.Unsetenv("DB_SSLMODE")
	t.Setenv("DB_USER", "scaffold")
	t.Setenv("DB_PASS", "x")
	t.Setenv("DB_HOST", "db")
	t.Setenv("DB_NAME", "scaffold")
	t.Setenv("DB_SSLMODE", "disable")

	dir := filepath.Join("..", "..", "config")
	_, err := config.Load(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sslmode")
}

func TestLoad_ProdAcceptsKeywordDSNRequire(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "prod")
	t.Setenv("AUTH_ALLOW_PHASE1_GATE", "true")
	_ = os.Unsetenv("DB_USER")
	_ = os.Unsetenv("DB_PASS")
	t.Setenv("DB_DSN_SQL", "host=db user=scaffold password=x dbname=scaffold sslmode=require")
	dir := filepath.Join("..", "..", "config")
	c, err := config.Load(dir)
	require.NoError(t, err)
	require.Contains(t, c.DB.DataSource, "sslmode=require")
}

func TestLoad_RpcClientTargets(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	dir := t.TempDir()
	const defaultYAML = `
Name: rpc-client
Host: 0.0.0.0
Port: 8080
RpcServer:
  Name: env-rpc
  ListenOn: 0.0.0.0:8081
RpcClient:
  Targets:
    user:
      Endpoints:
        - 127.0.0.1:9001
    order:
      Target: "etcd://127.0.0.1:2379/order.rpc"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(strings.TrimSpace(defaultYAML)+"\n"), 0o644))

	c, err := config.Load(dir)
	require.NoError(t, err)

	require.False(t, c.RpcClient.BlockDial, "默认非阻塞拨号：下游掉线不得拖垮启动")
	require.Len(t, c.RpcClient.Targets, 2)
	require.Equal(t, []string{"127.0.0.1:9001"}, c.RpcClient.Targets["user"].Endpoints)
	require.Equal(t, "etcd://127.0.0.1:2379/order.rpc", c.RpcClient.Targets["order"].Target)

	// map 值里的 struct 也必须拿到 go-zero 默认值（Middlewares 各项 default=true），
	// 否则 zrpc 自带的 duration/prometheus/breaker 拦截器会全部静默关闭。
	for name, tc := range c.RpcClient.Targets {
		require.Equal(t, int64(2000), tc.Timeout, "target %s Timeout 默认", name)
		require.True(t, tc.Middlewares.Trace, "target %s Middlewares.Trace 默认", name)
		require.True(t, tc.Middlewares.Duration, "target %s Middlewares.Duration 默认", name)
		require.True(t, tc.Middlewares.Breaker, "target %s Middlewares.Breaker 默认", name)
	}
}

func TestLoad_RpcClientRejectsEmptyLocator(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	dir := t.TempDir()
	const defaultYAML = `
Name: rpc-client
Host: 0.0.0.0
Port: 8080
RpcServer:
  Name: env-rpc
  ListenOn: 0.0.0.0:8081
RpcClient:
  Targets:
    ghost: {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(strings.TrimSpace(defaultYAML)+"\n"), 0o644))

	_, err := config.Load(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "RpcClient.Targets.ghost")
}

func TestGet_ClonesRpcClientTargets(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	dir := t.TempDir()
	const defaultYAML = `
Name: rpc-client
Host: 0.0.0.0
Port: 8080
RpcServer:
  Name: env-rpc
  ListenOn: 0.0.0.0:8081
RpcClient:
  Targets:
    user:
      Endpoints:
        - 127.0.0.1:9001
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.yaml"), []byte(strings.TrimSpace(defaultYAML)+"\n"), 0o644))
	_, err := config.Load(dir)
	require.NoError(t, err)

	// Get() 返回快照：调用方改写 slice/map 不得污染全局
	snap := config.Get()
	snap.RpcClient.Targets["user"].Endpoints[0] = "evil:9999"
	snap.RpcClient.Targets["injected"] = snap.RpcClient.Targets["user"]

	fresh := config.Get()
	require.Equal(t, []string{"127.0.0.1:9001"}, fresh.RpcClient.Targets["user"].Endpoints)
	require.Len(t, fresh.RpcClient.Targets, 1)
}
