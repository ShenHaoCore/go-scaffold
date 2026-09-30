package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"micro-scaffold/internal/config"
	"micro-scaffold/pkg/env"
	"micro-scaffold/pkg/grpcx"
	"micro-scaffold/pkg/logger"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func main() {
	configDir := flag.String("f", "config", "config directory")
	flag.Parse()

	// APP_ENV：进程已 export 优先；否则由 config.Load 从 .env 读取；皆空则按 dev
	dir := *configDir
	if !filepath.IsAbs(dir) {
		if wd, err := os.Getwd(); err == nil {
			dir = filepath.Join(wd, dir)
		}
	}

	c, err := config.Load(dir)
	if err != nil {
		logger.SetRedactor(logger.DefaultMaskRedactor{})
		fmt.Fprintf(os.Stderr, "load config: %s\n", logger.RedactString(err.Error()))
		os.Exit(1)
	}
	logx.MustSetup(c.Log)
	logger.Configure(c.Log.ServiceName, env.AppEnv())
	logger.ApplyEnvRedactor(env.AppEnv())

	// Health / Recover / Trace：Load 已强制 Health=可配 + Middlewares.Recover/Trace=false；
	// 此处再关 Health，改由下方显式 Register + SetServingStatus。
	c.RpcServer.Health = false

	// 热更用独立 cancel；优雅退出交给 go-zero proc，勿抢先 Stop()/return。
	hotCtx, hotCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer hotCancel()
	if err := config.StartHotReload(hotCtx, c.Etcd); err != nil {
		fmt.Fprintf(os.Stderr, "hot-reload: %s\n", logger.RedactString(err.Error()))
		os.Exit(1)
	}

	appEnv := env.AppEnv()
	s := zrpc.MustNewServer(c.RpcServer, func(grpcServer *grpc.Server) {
		// 业务 RPC Register 由各服务自行添加；本处仅保留进程骨架。

		if appEnv != "prod" {
			reflection.Register(grpcServer)
		}

		hs := health.NewServer()
		hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
		grpc_health_v1.RegisterHealthServer(grpcServer, hs)
	})
	// Unary + Stream：Recovery → Trace → Lang → Error（与 HTTP 前三段一致，末尾补错误码翻译）
	grpcx.RegisterServerInterceptors(s)
	defer s.Stop()

	logx.Infof("Starting scaffold-rpc at %s APP_ENV=%s", c.RpcServer.ListenOn, env.AppEnv())
	if n := len(c.RpcClient.Targets); n > 0 {
		logx.Infof("rpc clients configured: %d target(s) (see RpcClient.Targets)", n)
	}
	s.Start()
}
