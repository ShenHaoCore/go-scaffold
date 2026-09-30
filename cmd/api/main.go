package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go-scaffold/internal/config"
	"go-scaffold/internal/handler"
	"go-scaffold/internal/middleware"
	"go-scaffold/internal/svc"
	"go-scaffold/pkg/env"
	"go-scaffold/pkg/logger"
	"go-scaffold/pkg/response"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
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

	if err := middleware.CheckHealthProbeTokenForProd(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", logger.RedactString(err.Error()))
		os.Exit(1)
	}

	// 先组装依赖，避免 MustNewServer 后再 Exit 跳过 defer Stop
	svcCtx, err := svc.NewServiceContext(*c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "service context: %s\n", logger.RedactString(err.Error()))
		os.Exit(1)
	}
	// Start() 返回已在 HTTP Shutdown 排空之后，再关 DB
	defer svcCtx.Close()

	// 热更用独立 cancel；进程优雅退出交给 go-zero proc。
	// 勿在此对 SIGTERM 抢先 Stop()/return，否则会打断 HTTP Shutdown。
	hotCtx, hotCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer hotCancel()
	if err := config.StartHotReload(hotCtx, c.Etcd); err != nil {
		fmt.Fprintf(os.Stderr, "hot-reload: %s\n", logger.RedactString(err.Error()))
		hotCancel()
		svcCtx.Close()
		os.Exit(1)
	}

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	// 在注册路由前装好框架级错误钩子：否则 go-zero 内部走 httpx.Error 的失败
	// （如 Timeout 中间件）会用 http.Error 输出原始 error 文案 + 400，
	// 绕过 pkg/response 的统一响应体。边界见 response.InstallFrameworkErrorHandler。
	response.InstallFrameworkErrorHandler()

	handler.RegisterHandlers(server, svcCtx)

	logx.Infof("Starting scaffold-api at %s:%d APP_ENV=%s Auth.Mode=%s",
		c.Host, c.Port, env.AppEnv(), c.Auth.Mode)
	if bodyless := c.BodylessBuiltinMiddlewares(); len(bodyless) > 0 {
		logx.Infof("[warn] builtin middlewares with body-less error bodies: %v "+
			"(these bypass pkg/response; set Middlewares.<Name>=false in yaml to unify, losing that protection)",
			bodyless)
	}
	if names := svcCtx.RpcClients.Names(); len(names) > 0 {
		logx.Infof("rpc clients ready: %v (block=%t)", names, c.RpcClient.BlockDial)
	}
	server.Start()
}
