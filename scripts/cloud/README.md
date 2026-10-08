# 云资源本地联调（跳板机隧道）

> **日常开发请先走标准本地流程**：`make dev`，或 `scripts/dev-up.sh` / `scripts/dev-up.ps1`。
> 本文只描述**可选**的那条路：跳板机隧道 → 云上的 RDS / Redis / MQ。

云上的库和中间件在内网（VPC）里，本机直连不通，所以需要一条 SSH 隧道把远端端口映射到本地。

## 前提

1. 能 SSH 登录跳板机（`JUMP_HOST` / `JUMP_USER`，以及你自己的公钥已在跳板机 authorized_keys 里）。
2. 已拿到云资源的账号口令与**内网**域名（RDS / Redis）。
3. 跳板机到 RDS / Redis 的网络可达（安全组放行）。

## 步骤

```powershell
# 1. 设拓扑（勿写死进任何文件）
$env:JUMP_HOST  = "<跳板机公网 IP 或域名>"
$env:JUMP_USER  = "ecs-user"
$env:RDS_HOST   = "<pgm-xxxx.pg.rds.aliyuncs.com>"
$env:REDIS_HOST = "<r-xxxx.redis.rds.aliyuncs.com>"

# 2. 开隧道（保持这个窗口不关）
cd <仓库根目录>
powershell -File scripts\cloud\start-tunnel.ps1

# 3. 另开一个终端：把 .env 指向本地映射端口
Copy-Item .env.example .env
#   编辑 .env：DB_HOST=127.0.0.1  DB_PORT=15432（Redis 用 16379）
#   口令填真实值，不要提交

# 4. 起服务（跳过 compose，因为库在云上不在本地）
powershell -File scripts\dev-up.ps1 -Migrate -SkipCompose
```

验收：`curl http://127.0.0.1:8080/health`（依赖应显示 `ok`）。
可选：先单独验一下「跳板机 → 库」这段通不通 —— `powershell -File scripts\cloud\test-rds.ps1`。

停隧道：`powershell -File scripts/cloud/stop-tunnel.ps1`
停本地中间件：`docker compose down`

## 端口映射

| 资源 | 本地端口 | 远端 |
|------|----------|------|
| PostgreSQL (RDS) | `15432` | `5432` |
| Redis | `16379` | `6379` |
| RocketMQ | `18080` | `8080`（不设 `MQ_HOST` / `MQ_ENDPOINT` 则跳过） |

OSS / SLS 不走隧道：它们有公网地域 Endpoint。

## 说明

- 隧道脚本**只转发端口，不携带口令**。口令仍由业务进程从 `.env` 读取。
- `start-tunnel.ps1` 会先校验 `JUMP_HOST` / `*_HOST` 的字符合集（仅 `A-Za-z0-9._:-`），
  防止环境变量被误设成带空格或分号的内容注入到 ssh 命令行。
- `test-rds.ps1` 把口令经 **stdin** 喂给远端 `psql`，不进 argv、也不写进脚本正文 ——
  进程列表和 shell history 里都查不到。
- 这套脚本是 Windows PowerShell 版。Linux / macOS 上等价命令是
  `ssh -N -L 15432:<rds-host>:5432 -L 16379:<redis-host>:6379 <jump-user>@<jump-host>`。
