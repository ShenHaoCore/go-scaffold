<!-- 规范文档：服务前缀登记唯一源。非代码，请勿删除。 -->

# 统一错误码规范

## 错误码结构

```
{服务前缀}{错误类型}{具体编号}
  ↑          ↑          ↑
 3位字母    1位数字    3位数字
```

| 段 | 位数 | 示例 | 说明 |
|----|------|------|------|
| 服务前缀 | 3 位字母 | COM /（业务服务自登记） | 来源服务标识；须中央登记 |
| 错误类型 | 1 位数字 | 1=系统, 2=参数, 3=业务, 4=鉴权, 5=限流 | 错误大类 |
| 具体编号 | 3 位数字 | 001~999 | 具体错误标识 |

## 服务前缀登记（唯一源）

本文件（脚手架仓库 `pkg/errors/error-code-spec.md`）为服务前缀**唯一登记源**。禁止在其他规范文件另建登记表；未登记不得擅自使用新前缀。

**本脚手架（micro-scaffold）仅实现 `COM*` 通用码**；业务服务 clone 后在此表登记自身前缀，并在本服务 `pkg/errors` 中定义业务码。脚手架主干**不**预置任何业务域前缀。

| 前缀 | 服务 / 用途 | 状态 |
|------|-------------|------|
| COM | 通用错误（跨服务） | 预留，业务服务不得占用 |

**维护者：** 各服务负责人在本文件自行登记（建议 2 工作日审阅期）

**申请步骤**

1. 查阅上表确认 3 字母前缀未被占用  
2. 提交 PR 写明：服务名、前缀、负责人  
3. 评审合入本表后，方可在该服务内分配具体编号  

**Pending 区**（申请中，须含日期；**30 天清理**；获批须移入正式表）

| 前缀 | 服务 / 用途 | 申请人 | 申请日期 | 状态 |
|------|-------------|--------|----------|------|
| — | — | — | — | — |

**冲突处理：** 后合入者调整前缀或编号；禁止并行私自占用。

## 错误类型定义

| 类型编码 | 名称 | 说明 | HTTP 状态码映射 |
|---------|------|------|----------------|
| 1 | 系统错误 | 内部异常、DB 连接失败、第三方超时 | 500 |
| 2 | 参数错误 | 请求参数格式/必填/范围不合法 | 400 |
| 3 | 业务错误 | 业务规则冲突、状态不满足前提 | 422 / 409 |
| 4 | 鉴权错误 | 未登录、无权限、Token 过期 | 401 / 403 |
| 5 | 限流错误 | 请求频率超限 | 429 |

## COM 通用码示例

| 错误码 | 含义 |
|--------|------|
| COM2001 | 参数 xxx 为必填 |
| COM2002 | 参数 xxx 格式不合法 |
| COM4001 | 未登录或 Token 已过期 |
| COM4002 | 无操作权限 |
| COM1001 | 服务内部错误（响应勿暴露堆栈；服务端须记原始 error） |
| COM5001 | 请求频率超限（**预留**；Phase 1 未实现限流中间件） |

业务服务在登记前缀后自行维护本服务业务码表（例如 `XXX3001`），**勿**把业务码写回脚手架主干。

## 登记方式（代码唯一真源）

上表是人读的登记簿；**机器读的真源是 `pkg/errors/spec.go` 的注册表**。两者必须同时更新。

```go
// 业务码：在自身服务的 pkg/errors 下集中登记（init 或显式调用均可）
func init() {
    bizerr.Register(bizerr.Spec{
        Code: "USR3001",          // 3 位前缀 + 类型位 + 3 位编号
        Kind: bizerr.KindBiz,     // 类型位；须与 Code 的第 4 位一致，否则 panic
        ZhCN: "账户余额不足",
        EnUS: "Insufficient balance",
    })

    // 个别码的 HTTP 状态需要偏离类型位默认值时才写 HTTP 字段，例如：
    // bizerr.Register(bizerr.Spec{Code: "COM4002", Kind: bizerr.KindAuth, HTTP: 403, ...})
}
```

派生规则（**不要在别处重复实现**）：

| 函数 | 依据 | 用途 |
|------|------|------|
| `HTTPStatus(code)` | 登记表覆盖值 → 类型位默认值 → 500 | HTTP 响应状态 |
| `KindOf(code)` / `ParseKind(code)` | Code 第 4 位 | 类型位判定 |
| `SpecOf(code)` / `RegisteredCodes()` | 登记表 | 查询与自检 |

- **形非法的码一律 fail-closed**：长度不足（`COM`）或第 4 位不是已定义类型位（`COMX001` / `COM6001`）时，`ParseKind` 返回 `KindSystem`、`HTTPStatus` 返回 500，不猜测、不 panic。
- **形合法但未登记的码**（如 `USR3001`）仍按类型位推导 HTTP（→ 422）；只有文案回退为原始 code。两者差别见 `pkg/errors/errors_test.go` 的 `TestHTTPStatus` 与 `spec_test.go` 的 `TestHTTPStatus_MalformedCodeFailsClosed`。
- `Translate` 在码未登记时回退为原始 code（至少不丢码）；模板缺占位符时回退模板原文并打 warn，不 panic。
- HTTP 侧与 gRPC 侧的类型位映射**只有一份**：gRPC 由 HTTP 状态码纯对照推导（`pkg/response/grpc.go` 的 `httpToGRPC`），不再各自解析类型位，避免新增业务码时两处规则漂移。
- `go test ./pkg/errors/...` 会校验：类型位与 Code 一致、内置码已登记、非法码降级行为。

## 统一响应格式

### HTTP 成功响应

```json
{
  "code": 0,
  "message": "success",
  "data": { ... },
  "trace_id": "..."
}
```

### HTTP 错误响应

```json
{
  "code": "COM2001",
  "message": "参数 xxx 为必填",
  "detail": null,
  "trace_id": "..."
}
```

说明：成功 `code` 为数字 `0`；失败 `code` 为字符串错误码；`trace_id` 与响应头 `X-Trace-Id`、日志字段一致。i18n 可用 `{{.Field}}`；渲染失败不得 panic。

### gRPC 错误

使用 gRPC 标准 status：HTTP 类型位映射 gRPC codes；业务错误码写入 `google.rpc.ErrorInfo.Reason` 与 trailing metadata `x-error-code`；`x-trace-id` 同步写入 trailer。实现见 `pkg/response.GRPCError`。

**服务端必须注册错误翻译拦截器**，否则以上全部无效：

```go
grpcx.RegisterServerInterceptors(srv)   // Recovery → Trace → Lang → Error
```

原因：gRPC 对 handler 返回的**非 status error** 一律降级为 `codes.Unknown`，并把 `err.Error()` 原文塞进 status message。少了这一环，`*BizError` 过网后变成 `code = Unknown desc = "COM2001: 参数 xxx 为必填"`——错误码退化成裸字符串，`ErrorInfo.Reason` 与 trailer 双双丢失，调用方再也无法结构化还原。拦截器放行 `io.EOF`（流正常结束信号）与已是 gRPC status 的错误，只翻译其余。

客户端统一走 `zrpc.WithDialOption(grpcx.DialOptions()...)`（见 `internal/svc` 的 `dialRpcClients`），由 `FromGRPCError` 还原为 `*BizError`；连通性类错误（`Unavailable` / `DeadlineExceeded`）保持原样，不伪造成 `COM1001`。
