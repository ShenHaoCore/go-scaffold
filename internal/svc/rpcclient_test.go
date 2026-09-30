package svc_test

import (
	"context"
	"testing"

	"go-scaffold/internal/config"
	"go-scaffold/internal/svc"
	"go-scaffold/pkg/health"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

// 指向一个必定无人监听的端口：非阻塞拨号下不应失败，只是首次调用会 Unavailable。
const deadEndpoint = "127.0.0.1:1"

func TestDialRpcClients_EmptyTargetsBuildsNothing(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	clients, err := svc.DialRpcClientsForTest(config.Config{})
	require.NoError(t, err)
	require.Nil(t, clients, "未配 Targets 时不得建立任何出站连接")
	require.Nil(t, svc.RpcCheckersForTest(clients), "无客户端 → 无 health 探针")

	_, ok := clients.Conn("user")
	require.False(t, ok)
	require.Empty(t, clients.Names())
	clients.CloseAll() // nil map 上调用须安全
}

func TestDialRpcClients_NonBlockSucceedsAgainstDeadEndpoint(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	clients, err := svc.DialRpcClientsForTest(config.Config{
		RpcClient: config.RpcClientConf{
			Targets: map[string]zrpc.RpcClientConf{
				"user": {Endpoints: []string{deadEndpoint}},
			},
		},
	})
	require.NoError(t, err, "非阻塞拨号：下游未起也不该让构造函数失败")
	t.Cleanup(clients.CloseAll)

	conn, ok := clients.Conn("user")
	require.True(t, ok)
	require.NotNil(t, conn)
	_, ok = clients.Conn("order")
	require.False(t, ok, "未配置的服务名应返回 false，而不是空连接")
	require.Equal(t, []string{"user"}, clients.Names())

	checkers := svc.RpcCheckersForTest(clients)
	require.Len(t, checkers, 1)
	ck, ok := checkers["rpc:user"]
	require.True(t, ok, "health 条目名为 rpc:<服务名>")

	got := ck(context.Background())
	require.Equal(t, "rpc:user", got.Name)
	// 未发过请求时 gRPC 状态为 IDLE；把它当故障会造成大面积误报，故须为 ok
	require.Equal(t, health.StatusOK, got.Status)
}

func TestDialRpcClients_RejectsTargetWithoutLocator(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	// 无 Endpoints / Target / Etcd：zrpc 的 BuildTarget 会即刻报错（不等拨号超时）
	_, err := svc.DialRpcClientsForTest(config.Config{
		RpcClient: config.RpcClientConf{
			Targets: map[string]zrpc.RpcClientConf{
				"bad": {},
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `rpc client "bad"`)
}

func TestDialRpcClients_ErrorClosesAlreadyDialed(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	// 字母序 "aaa" 先建、"zzz" 后失败 → 已建立的连接必须被回收，不得泄漏
	clients, err := svc.DialRpcClientsForTest(config.Config{
		RpcClient: config.RpcClientConf{
			Targets: map[string]zrpc.RpcClientConf{
				"aaa": {Endpoints: []string{deadEndpoint}},
				"zzz": {},
			},
		},
	})
	require.Error(t, err)
	require.Nil(t, clients, "失败时不得返回半成品 map")
}

func TestRpcClients_NilConnIsSkipped(t *testing.T) {
	logx.Disable()
	t.Setenv("APP_ENV", "dev")

	checkers := svc.RpcCheckersForTest(svc.RpcClients{"user": nil})
	require.Len(t, checkers, 1)
	got := checkers["rpc:user"](context.Background())
	require.Equal(t, health.StatusSkipped, got.Status)
}
