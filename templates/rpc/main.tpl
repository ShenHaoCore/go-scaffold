{{/*
  警告：本模板仅供 goctl 参考，不是运行时真源。
  真实入口：cmd/rpc/main.go（config.Load、热更、gRPC Health、RegisterServerInterceptors）。
  Phase 1 禁止 goctl rpc --zrpc_out 覆盖 cmd/rpc。
  克隆到其他 module 时把下方 "micro-scaffold/pkg/grpcx" 改为本模块路径。
  真源入口用 -f config（目录）；下方 etc/*.yaml 仅为 goctl 默认示意，勿照抄。
*/}}
package main

import (
	"flag"
	"fmt"

	{{.imports}}

	"micro-scaffold/pkg/grpcx"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/{{.serviceName}}.yaml", "goctl default; real entry uses -f config")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	// 禁用 go-zero 内置 Health / Recover / Trace；Health 下方显式注册，链走 pkg/grpcx。
	// goctl 单 RPC 常嵌入 RpcServerConf（下方 c.Health）；本仓双进程真源见 cmd/rpc：c.RpcServer.Health / Middlewares。
	c.Health = false
	c.Middlewares.Recover = false
	c.Middlewares.Trace = false

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
{{range .serviceNames}}		{{.Pkg}}.Register{{.GRPCService}}Server(grpcServer, {{.ServerPkg}}.New{{.Service}}Server(ctx))
{{end}}
		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
		hs := health.NewServer()
		hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
		grpc_health_v1.RegisterHealthServer(grpcServer, hs)
	})
	// Unary + Stream：Recovery → Trace → Lang（实施方案 §4.11）
	grpcx.RegisterServerInterceptors(s)
	defer s.Stop()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
