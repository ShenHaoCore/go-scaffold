{{/*
  警告：本模板仅供 goctl 参考，不是运行时真源。
  真实入口：cmd/api/main.go（config.Load 多 yaml、热更、svc 错误处理）。
  禁止用 goctl 生成结果直接覆盖 cmd/api/main.go。
  HTTP 中间件链在 scripts/handwritten/routes.go.in → make gen 回写。
  go-zero 内置 Recover/Trace 由 config.Load → applyBuiltinMiddlewarePolicy 关闭。
  真源入口用 -f config（目录）；下方 etc/*.yaml 仅为 goctl 默认示意，勿照抄。
*/}}
package main

import (
	"flag"
	"fmt"

	{{.importPackages}}
)

var configFile = flag.String("f", "etc/{{.serviceName}}.yaml", "goctl default; real entry uses -f config")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 若沿用本模板：须关闭内置 Recover/Trace，并自行挂 Recovery→Trace→Lang→Auth→Agent
	c.Middlewares.Recover = false
	c.Middlewares.Trace = false

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	ctx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, ctx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
