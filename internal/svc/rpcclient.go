package svc

import (
	"context"
	"fmt"
	"sort"

	"go-scaffold/internal/config"
	"go-scaffold/pkg/grpcx"
	"go-scaffold/pkg/health"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// RpcClients 本进程持有的下游 gRPC 连接，按服务名索引。
//
// 为什么存 *grpc.ClientConn 而不是 zrpc.Client：zrpc 的 Client 只是一层持有 conn 的壳，
// conn 自身即长连接本体，业务侧需要的是它（pb 生成的 NewXxxClient(conn) 直接吃 conn）。
type RpcClients map[string]*grpc.ClientConn

// Conn 取指定下游连接。返回 false 表示该目标未在 RpcClient.Targets 中配置——
// 调用方应据此降级，而不是把「没配」当成「连不上」。
func (m RpcClients) Conn(name string) (*grpc.ClientConn, bool) {
	c, ok := m[name]
	return c, ok && c != nil
}

// Names 返回已配置的下游服务名（升序），便于日志与 health 稳定输出。
func (m RpcClients) Names() []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CloseAll 释放全部连接；幂等，单条失败不阻断其余。
func (m RpcClients) CloseAll() {
	for _, c := range m {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			logx.Errorf("close rpc client: %v", err)
		}
	}
}

// dialRpcClients 依 RpcClient.Targets 建立出站连接。
//
// 默认非阻塞（BlockDial=false）：zrpc 原生是阻塞拨号，下游没起就不能启动本进程——
// 对网关来说这是不可接受的耦合，因此这里默认反转成非阻塞，让下游掉线只体现为
// 调用报 Unavailable + health rpc:<name>=degraded。
//
// 拦截器链经 grpcx.DialOptions() 注入。注意 grpc 的 WithChain*Interceptor 是累加语义，
// zrpc 自带的 trace/duration/prometheus/breaker 会位于外侧，grpcx 的 Trace/Lang/Error 位于内侧。
func dialRpcClients(c config.Config) (RpcClients, error) {
	if len(c.RpcClient.Targets) == 0 {
		return nil, nil
	}

	clients := make(RpcClients, len(c.RpcClient.Targets))
	names := make([]string, 0, len(c.RpcClient.Targets))
	for name := range c.RpcClient.Targets {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tc := c.RpcClient.Targets[name]
		dialOpts := grpcx.DialOptions()
		// zrpc.WithDialOption 只收单个 grpc.DialOption，逐个转
		opts := make([]zrpc.ClientOption, 0, len(dialOpts)+1)
		for _, o := range dialOpts {
			opts = append(opts, zrpc.WithDialOption(o))
		}
		if !c.RpcClient.BlockDial {
			opts = append(opts, zrpc.WithNonBlock())
		}

		cli, err := zrpc.NewClient(tc, opts...)
		if err != nil {
			clients.CloseAll()
			return nil, fmt.Errorf("rpc client %q: %w", name, err)
		}
		clients[name] = cli.Conn()

		locator := "target=" + tc.Target
		if len(tc.Endpoints) > 0 {
			locator = fmt.Sprintf("endpoints=%v", tc.Endpoints)
		}
		logx.Infof("rpc client dialed service=%s %s block=%t timeout=%dms", name, locator, c.RpcClient.BlockDial, tc.Timeout)
	}
	return clients, nil
}

// rpcCheckers 为每个下游生成一个 health 探针。
//
// 判据用连接状态而非发真实请求：探针要便宜且无副作用，而 gRPC 的连接状态本身就是
// 可用性的直接投影。IDLE / CONNECTING 视为正常——gRPC 空闲会主动回落到 IDLE，
// 首次调用再拉起，把它当故障会造成大面积误报。
func rpcCheckers(clients RpcClients) map[string]health.Checker {
	if len(clients) == 0 {
		return nil
	}
	out := make(map[string]health.Checker, len(clients))
	for name, conn := range clients {
		name := name
		conn := conn
		key := "rpc:" + name
		out[key] = func(_ context.Context) health.CheckResult {
			if conn == nil {
				return health.CheckResult{Name: key, Status: health.StatusSkipped, Message: "not configured"}
			}
			switch conn.GetState() {
			case connectivity.TransientFailure, connectivity.Shutdown:
				return health.CheckResult{Name: key, Status: health.StatusDegraded, Message: conn.GetState().String()}
			default:
				return health.CheckResult{Name: key, Status: health.StatusOK, Message: conn.GetState().String()}
			}
		}
	}
	return out
}
