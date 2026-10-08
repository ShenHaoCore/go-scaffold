# 本地一键启动（Windows PowerShell）
#
# 分层：
#   默认 Local：docker compose 起 PG/Redis → 迁移（可选）→ goreman 起 api+rpc
#   可选 Tunnel：走跳板机隧道连云库（云资源联调时用，非日常默认）
#
# 用法：
#   powershell -File scripts\dev-up.ps1                     # 本地标准启动
#   powershell -File scripts\dev-up.ps1 -Migrate            # 本地 + 迁移
#   powershell -File scripts\dev-up.ps1 -Tunnel -Migrate    # 云库联调
#   powershell -File scripts\dev-up.ps1 -SkipCompose        # 只起进程（compose 已起过）
#
# 与 `make dev` 的区别：本脚本会**等 PG 真正就绪**（轮询 5432）再往下走。
# 停服务：本窗口 Ctrl+C；停隧道：scripts\cloud\stop-tunnel.ps1；停中间件：docker compose down
# 说明见 scripts/cloud/README.md

param(
    [switch]$Tunnel,
    [switch]$Migrate,
    [switch]$SkipCompose,
    # 拓扑勿写死进仓：-Tunnel 时须给环境变量或显式传参
    [string]$JumpHost = $env:JUMP_HOST,
    [string]$JumpUser = $(if ($env:JUMP_USER) { $env:JUMP_USER } else { "ecs-user" }),
    [string]$RdsHost = $env:RDS_HOST,
    [string]$RedisHost = $env:REDIS_HOST
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

function Import-DotEnv([string]$Path) {
    if (-not (Test-Path $Path)) {
        Write-Error "未找到 .env。请先: Copy-Item .env.example .env"
    }
    Get-Content $Path | ForEach-Object {
        if ($_ -match '^\s*#' -or $_ -match '^\s*$') { return }
        $k, $v = $_.Split('=', 2)
        if ([string]::IsNullOrWhiteSpace($k)) { return }
        Set-Item -Path "Env:$($k.Trim())" -Value $v
    }
}

function Test-LocalPort([int]$Port) {
    $null -ne (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue)
}

function Ensure-GoBin {
    $goBin = Join-Path (go env GOPATH) "bin"
    if (Test-Path $goBin) {
        $env:Path = "$goBin;" + $env:Path
    }
}

function Ensure-Tool([string]$Name, [scriptblock]$Install) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Write-Host "正在安装 $Name ..." -ForegroundColor Cyan
        & $Install
    }
}

if (-not (Test-Path (Join-Path $Root ".env"))) {
    Write-Host "无 .env，从 .env.example 复制（请再检查其中的连接参数）" -ForegroundColor Yellow
    Copy-Item (Join-Path $Root ".env.example") (Join-Path $Root ".env")
}

Import-DotEnv (Join-Path $Root ".env")
if (-not $env:APP_ENV) { $env:APP_ENV = "dev" }
Ensure-GoBin

if ($Tunnel) {
    if ([string]::IsNullOrWhiteSpace($JumpHost) -or [string]::IsNullOrWhiteSpace($RdsHost) -or [string]::IsNullOrWhiteSpace($RedisHost)) {
        Write-Error "[tunnel] 须设置 JUMP_HOST / RDS_HOST / REDIS_HOST（或 -JumpHost / -RdsHost / -RedisHost）。拓扑勿写死进脚本。"
    }
    if (Test-LocalPort 15432) {
        Write-Host "[tunnel] 已在监听 15432" -ForegroundColor Green
    } else {
        Write-Host "[tunnel] 新窗口启动 SSH 隧道（云库联调）..." -ForegroundColor Cyan
        # 用字符串拼接而非 here-string：here-string 的结束标记必须顶格且独占一行，
        # 嵌在深缩进的 if 分支里既易错也难读。
        $tunnelCmd = "`$env:JUMP_HOST='$JumpHost'; `$env:JUMP_USER='$JumpUser'; " +
                     "`$env:RDS_HOST='$RdsHost'; `$env:REDIS_HOST='$RedisHost'; " +
                     "Set-Location '$Root'; " +
                     "powershell -NoProfile -File '.\scripts\cloud\start-tunnel.ps1'"
        Start-Process powershell -ArgumentList "-NoExit", "-NoProfile", "-Command", $tunnelCmd
        Write-Host "[tunnel] 等待 127.0.0.1:15432（请在隧道窗口完成 SSH）..."
        $ok = $false
        for ($i = 0; $i -lt 60; $i++) {
            Start-Sleep -Seconds 1
            if (Test-LocalPort 15432) { $ok = $true; break }
        }
        if (-not $ok) {
            Write-Error "[tunnel] 15432 未就绪。完成 SSH 后重试，或改用本地模式（去掉 -Tunnel）。"
        }
        Write-Host "[tunnel] 就绪" -ForegroundColor Green
    }
} elseif (-not $SkipCompose) {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Error "未找到 docker。请安装 Docker Desktop，或改用: powershell -File scripts\dev-up.ps1 -Tunnel"
    }
    Write-Host "[compose] docker compose up -d ..." -ForegroundColor Cyan
    docker compose up -d
    Write-Host "[compose] 等待 postgres:5432 ..."
    $ok = $false
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Seconds 1
        if (Test-LocalPort 5432) { $ok = $true; break }
    }
    if (-not $ok) {
        Write-Error "[compose] postgres 5432 未就绪。检查: docker compose ps"
    }
    Write-Host "[compose] 就绪" -ForegroundColor Green
}

if ($Migrate) {
    if ([string]::IsNullOrWhiteSpace($env:DB_DSN)) {
        Write-Error "DB_DSN 为空，请检查 .env"
    }
    Ensure-Tool "migrate" { go install -tags "postgres" github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1 }
    Write-Host "[migrate] up ..." -ForegroundColor Cyan
    & migrate -path ./migrations -database $env:DB_DSN up
}

Ensure-Tool "goreman" { go install github.com/mattn/goreman@v0.3.15 }

Write-Host "[app] goreman 启动 api+rpc（APP_ENV=$($env:APP_ENV)）..." -ForegroundColor Cyan
Write-Host "  curl.exe http://127.0.0.1:8080/health"
Write-Host "  curl.exe -i -H `"X-Trace-Id: demo`" http://127.0.0.1:8080/health"
Write-Host "  停止: Ctrl+C"
Write-Host ""

& goreman -f Procfile start
