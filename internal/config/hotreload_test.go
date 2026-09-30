package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractSSLMode(t *testing.T) {
	require.Equal(t, "require", extractSSLMode("host=db user=u password=p dbname=x sslmode=require"))
	require.Equal(t, "verify-full", extractSSLMode("postgres://u:p@h/db?sslmode=require&sslmode=verify-full"))
	require.Equal(t, "require", extractSSLMode("postgres://u:p@h/db?sslmode=require"))
	// URL 形态：query 无 sslmode 时不回退关键字扫描（密码含 sslmode= 子串不得误报）
	require.Equal(t, "", extractSSLMode("postgres://u:psslmode=require@h/db"))
	require.Equal(t, "", extractSSLMode("postgres://u:p@h/db"))
	// URL 形态：空 authority 也按 URL 处理（不得误入关键字扫描）
	require.Equal(t, "require", extractSSLMode("postgres:///db?sslmode=require"))
	// 关键字形态：sslmode= 须为 token 边界（密码值含子串不得误报）
	require.Equal(t, "", extractSSLMode("host=db user=u password=xsslmode=require dbname=y"))
	require.Equal(t, "require", extractSSLMode("sslmode=require"))
	// 关键字形态：单引号包裹的值整体跳过（密码含引号内 sslmode= 不得误报 fail-open）
	require.Equal(t, "", extractSSLMode("host=db user=u password='x sslmode=require' dbname=y"))
	require.Equal(t, "disable", extractSSLMode("host=db user=u password='x sslmode=require' dbname=y sslmode=disable"))
	require.Equal(t, "require", extractSSLMode("host=db user=u dbname=y sslmode='require'"))
}

func TestVerifyHotReloadToken(t *testing.T) {
	t.Setenv("ETCD_HOTRELOAD_TOKEN", "")
	require.NoError(t, verifyHotReloadToken(map[string]any{}))

	t.Setenv("ETCD_HOTRELOAD_TOKEN", "tok")
	require.Error(t, verifyHotReloadToken(map[string]any{}))
	require.NoError(t, verifyHotReloadToken(map[string]any{"HotReloadToken": "tok"}))
	require.Error(t, verifyHotReloadToken(map[string]any{"HotReloadToken": "wrong"}))
}

func TestStartHotReload_RequiresTokenWheneverEtcdConfigured(t *testing.T) {
	t.Setenv("ETCD_HOTRELOAD_TOKEN", "")
	t.Setenv("ETCD_ALLOW_INSECURE", "true")
	for _, appEnv := range []string{"dev", "test", "prod"} {
		t.Setenv("APP_ENV", appEnv)
		err := StartHotReload(context.Background(), EtcdConf{Hosts: []string{"127.0.0.1:2379"}, Key: "micro-scaffold/config"})
		require.Error(t, err, "APP_ENV=%s", appEnv)
		require.Contains(t, err.Error(), "ETCD_HOTRELOAD_TOKEN")
	}
}

func TestEtcdClientTLS_Incomplete(t *testing.T) {
	_, err := etcdClientTLS(EtcdConf{CertFile: "a.crt"})
	require.Error(t, err)
	cfg, err := etcdClientTLS(EtcdConf{})
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestRestartRequiredKeysIn(t *testing.T) {
	norm := map[string]any{
		"Log":     map[string]any{"Level": "debug"},
		"DB":      map[string]any{"DataSource": "x"},
		"Timeout": 100,
		"Port":    8080,
		"Host":    "0.0.0.0",
		"RedisConf": map[string]any{
			"Host": "h",
		},
		"Etcd": map[string]any{
			"Hosts": []any{"127.0.0.1:2379"},
		},
		"RpcServer": map[string]any{
			"ListenOn": "0.0.0.0:8081",
		},
	}
	keys := restartRequiredKeysIn(norm)
	require.Contains(t, keys, "DB.DataSource")
	require.Contains(t, keys, "Timeout")
	require.Contains(t, keys, "Port")
	require.Contains(t, keys, "Host")
	require.Contains(t, keys, "RedisConf.Host")
	require.Contains(t, keys, "Etcd.Hosts")
	require.Contains(t, keys, "RpcServer.ListenOn")
	require.NotContains(t, keys, "Log.Level")

	keys2 := restartRequiredKeysIn(map[string]any{
		"Auth": map[string]any{"Mode": "sdk"},
	})
	require.Contains(t, keys2, "Auth.Mode")
}
