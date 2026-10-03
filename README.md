# go-scaffold — 通用微服务脚手架

[![CI](https://github.com/ShenHaoCore/go-scaffold/actions/workflows/ci.yml/badge.svg)](https://github.com/ShenHaoCore/go-scaffold/actions/workflows/ci.yml)

基于 **go-zero v1.7.3** 的双进程脚手架，Go module 名 `go-scaffold`：

- **`scaffold-api`** —— HTTP 网关（`rest`，默认 `:8080`）
- **`scaffold-rpc`** —— gRPC 服务（`zrpc`，默认 `:8081`）

只提供 **health 与框架能力**，**不含任何业务域示例**——clone 下来即是干净起点。业务向 SDK 由各服务自行 `go get`，挂载契约见 [`integration-spec.md`](./integration-spec.md)。数据库为 **PostgreSQL 18（pgx）**，Redis 可选。

## 能力清单

| 能力 | 说明 |
|------|------|
| 统一错误码 | `{3位前缀}{类型位}{3位编号}`（如 `COM2001`）。`pkg/errors` 注册表是**唯一真源**，HTTP 与 gRPC 共用一份映射 |
| 统一响应体 | 成功 `{code:0, message, data, trace_id}`；失败 `{code:"COM2001", message, detail, trace_id}` |
| 全链路 Trace | 入站兼容 `X-Trace-Id` / `EagleEye-TraceID` / `X-Request-Id` / `X-B3-TraceId` / `traceparent`；响应回写 `X-Trace-Id` 并贯穿日志 |
| i18n | `Accept-Language` → `zh-CN` / `en-US`；错误文案支持 `{{.Field}}` 占位，缺占位符回退原文不 panic |
| HTTP 中间件链 | `Recovery → Trace → Lang → Auth → Agent → Handler` |
| gRPC 拦截器链 | `[go-zero 自带] → Recovery → Trace → Lang → Error → Handler`（Unary 与 Stream 一致） |
| 健康检查 | `/health`（readiness，`degraded`/`down` → 503）+ `/health/live`（liveness）；下游连接自动派生 `rpc:<服务名>` 探针 |
| 配置分层 + 热更 | `default.yaml → {APP_ENV}.yaml → config-local.yaml → 环境变量`；Etcd 热更白名单见 `internal/config/hotkeys.go` |
| 可选云组件 | PostgreSQL / Redis / 阿里云 OSS、RocketMQ 5.x、SLS，均「半配置 = skipped」，本地可零依赖启动 |
| 交付物 | Dockerfile（多阶段 + `grpc_health_probe` 校验和校验）、docker compose、K8s `deploy/`、GitHub Actions CI |

## 环境要求

| 依赖 | 版本 | 说明 |
|------|------|------|
| Go | 1.22+ | `go.mod` 声明 `go 1.22`，Dockerfile 同为 `golang:1.22` |
| Docker + Compose | — | `make dev` 用它起 PostgreSQL / Redis |
| `protoc` | 3.x | 仅生成 `api/pb/` 时需要，须**系统安装** |
| `make` | — | 配方是 POSIX shell，Windows 下用 Git Bash / WSL |

其余工具（`goctl` / `migrate` / `goreman` / `air` / `protoc-gen-*` / `golangci-lint`）由 `make tools` 安装。

## 快速开始

```bash
make tools && make deps     # 工具链 + 依赖（make gen 会校验 goctl 1.7.3 / go-zero v1.7.3）

cp .env.example .env        # 按需改 DB_* / REDIS_* 等；各项语义见文件内注释
make dev                    # compose up +（有 migrations/*.up.sql 才）migrate + goreman
```

`make dev` 即 `make up` →（可选）`make migrate-up` → `make run-all` 三步连跑；`make init` 是 `deps + setup-migrate`。

冒烟验证（响应头会回写你传入的 `X-Trace-Id`）：

```bash
curl -i -H "X-Trace-Id: scaffold-trace" http://127.0.0.1:8080/health
curl -i http://127.0.0.1:8080/health/live
```

**无库也能起**：非 prod 允许无 DSN（只是没有业务表），prod 强制配库。业务进程自己会读 `.env`（`internal/config.Load` 里 `godotenv.Load`，且不覆盖已有进程变量）。

镜像：`make docker`（用 `IMAGE=registry/scaffold:sha` 覆盖标签）。

## 配置

优先级从低到高，后者覆盖前者：

```text
config/default.yaml → config/{APP_ENV}.yaml → config/config-local.yaml → 环境变量 / .env
```

- `APP_ENV` 取值 `dev` / `test` / `prod`（`development`→`dev`、`production`→`prod`）；**未知值 fail-closed 报错**，不会静默落回 `default.yaml` 的 `Auth.Mode=dev`
- 进程已 `export APP_ENV` 时**以它为准**，`.env` 中的同名值被忽略；进程未设时才由 `.env` 的 `APP_ENV` 决定用哪份 yaml（再没有则 `dev`）
- 环境变量 → yaml 字段的映射见 `internal/config/applyEnvOverrides`；`AUTH_*` / `HEALTH_PROBE_*` / `*_ALLOW_INSECURE` 等安全开关**仅环境变量生效**（yaml 无对应字段）
- 全部可配项（含 OSS / RocketMQ / SLS / VPC / Etcd TLS）的注释示例见 [`.env.example`](./.env.example)
- `DB_DSN` **仅**给 `make migrate-*` 用；业务进程连库读 `DB_DSN_SQL`，或用 `DB_USER/DB_PASS/DB_HOST/DB_PORT/DB_NAME` 组装
- `RpcClient.Targets` 是嵌套映射，**无环境变量覆盖入口**：容器内改下游目标须挂 `config/config-local.yaml` 或重建镜像
- `make print-config-key` 打印 Etcd Key；热更受管的键见 `internal/config/hotkeys.go`

> `config-local.yaml`、`.env`、`deploy/secret.yaml` 已在 `.gitignore` 中，不会入库。

## 项目结构

```text
go-scaffold/
├── api/
│   ├── desc/              # .api 描述；main.api 是 make gen 的唯一入口
│   ├── pb/                # protobuf 产物（业务自备 proto，用 protoc 更新）
│   └── sdk-interfaces/    # 业务 SDK 挂载契约占位（只留接口，不含实现）
├── cmd/api、cmd/rpc       # 两个进程入口
├── config/                # default / dev / test / prod.yaml（config-local.yaml 本地覆盖）
├── deploy/                # K8s 清单 + secret 示例
├── internal/              # config、handler、logic、types、middleware、repo、model、svc
├── migrations/            # golang-migrate（*.up.sql / *.down.sql）
├── pkg/                   # auth、cloud、env、errors、grpcx、health、logger、response、secure、trace
├── scripts/handwritten/   # routes.go.in —— 路由真源，make gen 后被回填
├── templates/             # goctl 模板（非运行时真源，见 templates/README.md）
├── Procfile、Makefile、Dockerfile、docker-compose.yml / docker-compose.full.yml
└── integration-spec.md、.env.example、.golangci.yml、.github/workflows/ci.yml
```

## 开发指南

### 新增 HTTP API

1. 在 `api/desc/` 加 `.api`，并在 `main.api` 中 `import`
2. `make gen`（入口 `main.api`；生成后自动把 `scripts/handwritten/routes.go.in` 还原回 `internal/handler/routes.go`）
3. 新 handler 须**手工并入** `internal/handler/routes.go`，并同步 `scripts/handwritten/routes.go.in`——`make gen` 末尾会自检中间件链与 `/health` 路由是否还在

> health **勿** import 进 `main.api`——会被 `handler.tpl` 覆盖成恒 200，丢掉 `degraded→503`。

### 新增 gRPC 接口

业务自备 proto，用 `protoc` 更新 `api/pb/`。**禁止** `goctl rpc --zrpc_out` 覆盖 `cmd/rpc`。

### 返回业务错误

```go
import bizerr "go-scaffold/pkg/errors"

return nil, bizerr.NewFromContext(ctx, bizerr.CodeInvalidParam, map[string]any{"Field": "name"})
```

业务前缀先在 [`pkg/errors/error-code-spec.md`](./pkg/errors/error-code-spec.md) 登记后使用；脚手架只提供 `COM*`：

| 错误码 | 含义 |
|--------|------|
| `COM1001` | 服务内部错误（响应不暴露堆栈，服务端记原始 error） |
| `COM2001` / `COM2002` | 参数为必填 / 参数格式不合法 |
| `COM4001` / `COM4002` | 未登录或 Token 已过期 / 无操作权限 |
| `COM5001` | 请求频率超限（**预留**，Phase 1 未实现限流中间件） |

`pkg/errors` 注册表（`Register` / `HTTPStatus`）是唯一真源：**形非法**的码（长度不足，或第 4 位不是已定义类型位）fail-closed 到 500；**形合法但未登记**的码仍按类型位推导 HTTP（如 `USR3001` → 422），只有文案回退为原始 code。

## gRPC

双进程各自独立：`scaffold-api`（HTTP 网关）与 `scaffold-rpc`（gRPC 服务），两侧共用同一套错误码、trace、i18n 语义。

**服务端**：`cmd/rpc/main.go` 调 `grpcx.RegisterServerInterceptors(srv)`，Unary 与 Stream 链均为

```text
[go-zero 自带] → Recovery → Trace → Lang → Error → Handler
```

其中 `Error` 拦截器**不可省**——gRPC 会把 handler 返回的非 status error 降级为 `codes.Unknown` 并丢弃错误码（详见 [`error-code-spec.md`](./pkg/errors/error-code-spec.md) 的 gRPC 段）。`Trace`/`Recover` 由 `internal/config` 的 `applyBuiltinMiddlewarePolicy`（`Load` 末尾调用，非导出）强制关掉 go-zero 内置版本，避免双轨 trace 与 panic 文案泄漏。

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

**边界**：`zrpc` 客户端传输层固定明文（`RpcClientConf` 无 TLS 字段），依赖 VPC 内网隔离；需要 TLS 须自行 `zrpc.WithTransportCredentials` 覆盖。服务端 Breaker / Shedding / Timeout 位于我们的 `Error` 拦截器**外侧**，它们产生的失败不带业务码，只能靠 yaml 关掉换取完全统一。

## 健康检查

| 端点 | 语义 |
|------|------|
| `GET /health` | readiness。`ok` → 200，`degraded` / `down` → 503。默认返回依赖明细（`name` / `status`），但**不含** `message`；`HealthProbe.ExposeDependencies=false` 时只返回整体 `status` |
| `GET /health/live` | liveness，恒 200，不探测依赖 |

单项状态取值 `ok` / `degraded` / `down` / `skipped`；`skipped` 表示未配置（如 OSS 四项不齐、Redis `Host` 留空）。RPC 侧由 `cmd/rpc` 显式 `SetServingStatus("", SERVING)` 注册，配合镜像内的 `grpc_health_probe` 使用。

`HealthCheckTimeout`（默认 3s）须明显小于 `Rest Timeout`（默认 8s），否则 `/health` 会与 `TimeoutHandler` 竞态。prod 下 `/health` 须带 `X-Health-Probe-Token`；`HEALTH_PROBE_REQUIRE_TOKEN=true` 时本机回环也要带。

## 常用命令

| 命令 | 说明 |
|------|------|
| `make dev` | 一键本地起：compose +（有迁移才）migrate + goreman |
| `make up` / `down` | 只起 / 停 compose（PostgreSQL 5432、Redis 6379） |
| `make run-all` / `run-api` / `run-rpc` | goreman 双进程 / 单起某个进程（`APP_ENV` 可覆盖） |
| `make gen` | 从 `main.api` 生成 handler / types，并还原手写路由 |
| `make build` | 构建 `output/scaffold-api`、`output/scaffold-rpc` |
| `make test` / `test-race` / `test-coverage` | 单测（`-count=1`）/ 竞态检测（`-race`）/ 覆盖率 |
| `make setup-lint` + `lint` | 静态检查（需先装 `golangci-lint@v1.64.8`；缺工具**直接失败**，不再静默降级为 `go vet`） |
| `make migrate-up` / `migrate-down` / `migrate-force` | 迁移（须 `DB_DSN`；`force` 用于修 dirty 状态） |
| `make tools` | 安装 goctl / migrate / goreman / air / protoc-gen-* / golangci-lint |
| `make print-config-key` | 打印 Etcd Key 提示 |
| `make check-routes` | 校验 `routes.go` 与 `routes.go.in` 无漂移（CI 必过；漂移意味着手改的 routes.go 会被下次 `make gen` 静默覆盖） |
| `make clean` | 清 `output/` 与覆盖率产物 |

## CI

`.github/workflows/ci.yml`（本仓唯一 CI）。三个 job 并行，全部为**必过门禁**：

| job | 内容 |
|-----|------|
| `lint` | `gofmt -l`、`make check-go-zero check-templates check-routes`、golangci-lint（配置见 `.golangci.yml`） |
| `test` | `make test-race`（`APP_ENV=test`，含 `-race` + 覆盖率产物） |
| `build` | `make build` + 按 Dockerfile 同参（`CGO_ENABLED=0`）构建 linux/amd64 与 linux/arm64 |

Go 版本取自 `go.mod` 的 `go 1.22`（与 Dockerfile 对齐），不写死。`-race` 只能在这里跑——开发机是 windows/386，该平台不支持竞态检测。

`make lint` 默认必须 0 条：与 go-zero 配置 DSL 冲突的误报（`SA5008` 等）已在 `.golangci.yml` 的 `issues.exclude-rules` 里按「路径 + 文案」精确豁免，新增豁免必须写清理由。

## 约定与已知边界

**约定**

| 项 | 说明 |
|----|------|
| 鉴权 | `dev` 放行；`require` = 非空 Bearer（**非 JWT**，内部门闩）；公网须等 `Mode=sdk`（权限 SDK 待合并） |
| ORM | go-zero model + sqlx；logic 只依赖 `internal/repo`；软删表登记 `SoftDeleteRequiredTables`（默认可空） |
| SDK | 本仓不引入业务 SDK；埋点等由业务 `go get`，契约见 [`integration-spec.md`](./integration-spec.md) |
| 生成物 | `internal/handler/routes.go` 须逐字节等于 `scripts/handwritten/routes.go.in` —— 后者是真源，`make gen` 会用**前者覆盖后者**的方向回填；漂移由 `make check-routes` 在 CI 拦住（否则手改的 routes.go 会在下次 gen 时静默丢失）。`templates/` 非运行时真源 |

**已知边界**（有意保留，不是 bug）

- go-zero `rest` 内置 **Breaker / Shedding / MaxConns / MaxBytes** 命中时只写状态码、**不写 body**，是唯一绕过统一响应体的路径（启动日志会列出）；内置 Recover / Trace 已由 `internal/config` 强制关闭
- `rest` 的 404 走 `http.NotFoundHandler()`，不经统一错误处理
- `zrpc` **服务端**内置 Breaker / Shedding / Timeout 位于我们的 `Error` 拦截器**外侧** ⇒ 它们产生的失败不带业务码
- `zrpc` **客户端**传输层固定明文（无 TLS 字段），靠 VPC 内网隔离
- `cmd/rpc` 是空骨架、不装配 `ServiceContext` ⇒ `RpcClient.Targets` 在该进程内不生效

## 文档索引

| 文档 | 内容 |
|------|------|
| [`pkg/errors/error-code-spec.md`](./pkg/errors/error-code-spec.md) | 错误码规范 + 服务前缀登记表（唯一源）+ 统一响应格式 |
| [`integration-spec.md`](./integration-spec.md) | 业务 SDK 挂载契约（鉴权 / 埋点 / MQ / 云资源） |
| [`templates/README.md`](./templates/README.md) | goctl 模板说明与强制约定自检表 |
| [`migrations/README.md`](./migrations/README.md) | 迁移约定 |
| [`scripts/handwritten/README.md`](./scripts/handwritten/README.md) | 手写路由与 gen 回填机制 |
| [`.env.example`](./.env.example) | 全部环境变量与语义注释 |
