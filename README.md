# micro-scaffold — 通用微服务脚手架

基于 go-zero。Go module：`micro-scaffold`；进程：`scaffold-api` / `scaffold-rpc`。仅 **health** 与框架能力，**无**业务域示例；业务向 SDK 由服务自行 `go get`（见 [integration-spec.md](./integration-spec.md)）。

引擎：**PostgreSQL 18.0**（pgx）+ 可选 Redis。错误码：`COM*`。

## 快速开始

首次：`make tools && make deps`（另需系统 `protoc`；Makefile 用 Git Bash / WSL）。

```bash
cp .env.example .env
make init    # deps + migrate 工具
make dev     # compose up +（有迁移才）migrate + goreman
```

```bash
curl -i -H "X-Trace-Id: scaffold-trace" http://127.0.0.1:8080/health
curl -i http://127.0.0.1:8080/health/live
```

分步：`make up` →（可选）`make migrate-up` → `make run-all`。镜像：`make docker` / `make docker-build`。

无 DSN：非 prod 可起（无业务表）；prod 必须配库。

## 新增 API

1. 在 `api/desc/` 加 `.api`，并在 `main.api` 中 `import`
2. `make gen`（入口 `main.api`；恢复 `scripts/handwritten/routes.go.in`；新 handler 须手工并入路由并同步 `.in`）
3. health **勿** import 进 `main.api`

RPC：业务自备 proto，用 `protoc` 更新 `api/pb/`。**禁止** `goctl rpc --zrpc_out` 覆盖 `cmd/rpc`。

```go
import bizerr "micro-scaffold/pkg/errors"
return nil, bizerr.NewFromContext(ctx, bizerr.CodeInvalidParam, map[string]any{"Field": "name"})
```

业务前缀自行登记（[`pkg/errors/error-code-spec.md`](./pkg/errors/error-code-spec.md)）；脚手架只留 `COM*`。

## gRPC

双进程各自独立：`scaffold-api`（HTTP 网关）与 `scaffold-rpc`（gRPC 服务）。两侧共用同一套错误码、trace、i18n 语义。

**服务端**：`cmd/rpc/main.go` 调 `grpcx.RegisterServerInterceptors(srv)`，Unary 与 Stream 链均为

```text
[go-zero 自带] → Recovery → Trace → Lang → Error → Handler
```

其中 `Error` 拦截器不可省——gRPC 对 handler 返回的非 status error 会降级为 `codes.Unknown` 并丢弃错误码（详见 [`error-code-spec.md`](./pkg/errors/error-code-spec.md) 的 gRPC 段）。`Trace`/`Recover` 由 `internal/config` 的 `applyBuiltinMiddlewarePolicy`（`Load` 末尾调用，非导出）强制关掉 go-zero 内置版本，避免双轨 trace 与 panic 文案泄漏。

**客户端**：在 `config/*.yaml` 的 `RpcClient.Targets` 里按下游服务名配置，`internal/svc.NewServiceContext` 自动装配为 `svcCtx.RpcClients`：

```yaml
RpcClient:
  BlockDial: false            # 默认非阻塞：下游没起不阻断本进程启动
  Targets:
    user:
      Endpoints: ["127.0.0.1:9001"]
    order:
      Target: "etcd://127.0.0.1:2379/order.rpc"
```

```go
conn, ok := svcCtx.RpcClients.Conn("user")   // ok=false 表示「没配」，不是「连不上」
cli := userpb.NewUserClient(conn)
```

每个 target 会额外注册一条 health 探针 `rpc:<服务名>`（读 gRPC 连接状态，IDLE/CONNECTING 不算故障）。

注：`cmd/rpc` 是空骨架、不装配 `ServiceContext`，故 `RpcClient.Targets` 在 rpc 进程内**不会**被拨号；rpc→rpc 出站需照 `cmd/api` 加 `svc.NewServiceContext(c)` 后取 `svcCtx.RpcClients`。

**边界**：`zrpc` 客户端传输层固定明文（`RpcClientConf` 无 TLS 字段），依赖 VPC 内网隔离；需要 TLS 须自行 `zrpc.WithTransportCredentials` 覆盖。服务端 Breaker / Shedding / Timeout 位于我们的 `Error` 拦截器**外侧**，它们产生的失败不带业务码，只能靠 yaml 关掉换取完全统一。`RpcClient.Targets` 是嵌套映射，**没有环境变量覆盖入口**（`applyEnvOverrides` 不处理它）——容器里改下游目标须挂载覆盖文件（`config/config-local.yaml`，最后合并）或重建镜像。

## 约定

| 项 | 说明 |
|----|------|
| 鉴权 | `dev` 放行；`require` = 非空 Bearer（非 JWT）；公网须等 `Mode=sdk` |
| 健康 | `/health` degraded→503；`/health/live` 存活；手写不走 gen；RPC 侧另有 `rpc:<服务名>` 探针 |
| 错误码 | 唯一真源是 `pkg/errors` 注册表（`Register` / `HTTPStatus`）；**形非法**的码 fail-closed 到 500，形合法但**未登记**的码仍按类型位推导 HTTP（仅文案回退为原始 code），见 `error-code-spec.md` |
| 配置 | `APP_ENV` 选 yaml；`DB_DSN` 仅 migrate；`ETCD_HOSTS` 热更（白名单见 `internal/config/hotkeys.go`） |
| ORM | go-zero model + sqlx；logic 只依赖 `internal/repo`；软删表登记 `SoftDeleteRequiredTables`（默认可空） |
| SDK | 本仓不引入业务 SDK；埋点等由业务 `go get`，契约见 `integration-spec.md` |
| HTTP 框架中间件 | go-zero 内置 Recover/Trace 已强制关闭（自有链负责）；Breaker/Shedding/MaxConns/MaxBytes 仍开启，命中时只写状态码不写 body，是**唯一**绕过统一响应体的路径（启动日志会列出）；框架自身抛出的其他错误已由 `response.InstallFrameworkErrorHandler()`（`cmd/api` 启动时安装）归一到统一响应体 |

## 目录

```text
micro-scaffold/
├── api/desc/、api/pb/
├── cmd/api、cmd/rpc
├── config/、migrations/、deploy/、templates/
├── internal/、pkg/、scripts/handwritten/
├── Procfile、Makefile、integration-spec.md
└── docker-compose.yml / docker-compose.full.yml
```

## 常用命令

| 命令 | 说明 |
|------|------|
| `make dev` | 一键本地起 |
| `make build` / `test` / `setup-lint` + `lint` | 构建 / 测试 / 静态检查（lint 需先 `make setup-lint` 装 golangci-lint，不再静默降级为 `go vet`） |
| `make migrate-up` / `migrate-down` | 迁移（须 `DB_DSN`） |
| `make print-config-key` | Etcd Key 提示 |
