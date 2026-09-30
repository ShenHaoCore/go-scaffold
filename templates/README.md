# goctl 模板说明（非运行时真源）

| 路径 | 用途 | 真源 |
|------|------|------|
| `templates/api/*` | `make gen` 生成 handler/types 时使用；`handler.tpl` 已定制统一响应 | `cmd/api/main.go` + `scripts/handwritten/routes.go.in` |
| `templates/rpc/*` | 供后续对齐；**禁止** `goctl rpc --zrpc_out` 覆盖本仓 | `cmd/rpc/main.go`（空 RPC 骨架） |
| `templates/model/*` | goctl model + sqlx 生成参考；运行时真源为 `internal/model`（sqlx） | `internal/model/`（业务自行加表） |
| `templates/kube/*` | goctl 参考件；**勿直接用于生产**（`deployment.tpl` / `job.tpl` 均有警告头；job 为合法 YAML；deployment 探针为 tcpSocket 示意） | `deploy/deployment.yaml`（rpc 探针为 exec `grpc_health_probe`） |

强制约定自检表（每条都有对应测名，改动后跑 `make test` 自证）：

| # | 约定 / 自证测名 |
|---|-----------------|
| 1 | gen 入口见根 README / `Makefile`（本仓固定 `main.api`） |
| 2 | gRPC Health：`SetServingStatus("", SERVING)` + 镜像内 `grpc_health_probe`（exec，见 `deploy/`） |
| 3 | Lang / `WithLang` / `NewFromContext` 同源 |
| 4 | prod 禁 Mode=dev；未知 APP_ENV fail-closed |
| 5 | `SoftDeleteRequiredTables` + `CheckSoftDeleteColumn(s)`；`TestNewServiceContext_SoftDeleteMissingFailFast` |
| 6 | `TestFromGRPCError_PreferErrorInfoOverMismatchTrailer` |
| 7 | HTTP/gRPC 链 `Recovery→Trace→Lang→Error`；`TestLoad_ForcesBuiltinMiddlewareOffEvenIfYAMLTrue`；`routes.go`；`TestRecoveryMiddleware_PanicReturnsCOM1001AndTraceID`；`TestRecoveryTraceChain_PanicKeepsSameTraceID`；`grpcx.RegisterServerInterceptors` / `ServerOptions`；`TestRegisterServerInterceptors_RegistersBothChains` |
| 8 | 错误码唯一真源：`TestHotFields_PathsExistOnConfigStruct`（配置键与结构体一致）、`pkg/errors/spec_test.go`（类型位 / HTTP 覆盖 / 非法码 fail-closed） |
| 9 | gRPC 错误码不丢：`TestE2E_Unary_ErrorCodeAndTrailerRoundTrip`（ErrorInfo.Reason + trailer + 客户端还原） |

`api/main.tpl` / `rpc/main.tpl` 内手动关 Recover/Trace 仅作参考；运行时以 `config.Load` → `applyBuiltinMiddlewarePolicy` 为准。
