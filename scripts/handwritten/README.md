# handwritten

| 文件 | 用途 |
|------|------|
| `routes.go.in` | HTTP 链 Recovery→Trace→Lang→Auth→Agent + `/health*`；`make gen` 后复制为 `internal/handler/routes.go` |

改路由时先改 `routes.go.in`，再跑 `make gen`（或手动复制到 `internal/handler/routes.go`）。

**新增业务 API：** 在 `api/desc/` 添加 `.api` 并 import 到 `main.api`，`make gen` 后把新 handler 注册进 `routes.go.in`（health 仍手写、勿被 goctl 覆盖）。脚手架本身不包含业务示例。
