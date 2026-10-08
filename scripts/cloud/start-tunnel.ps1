# SSH local forward: 本机 -> 跳板机 -> 云 RDS / Redis / RocketMQ
#
# 用途：云上的数据库/中间件在内网（VPC）里，本机直连不通。开一条 SSH 隧道把
# 远端端口映射到本地，业务进程连 127.0.0.1:<本地端口> 即可，无需改代码。
#
# 拓扑一律用环境变量传入 —— 本文件不得出现密码或真实主机名。
#
#   $env:JUMP_HOST  = "<跳板机公网 IP 或域名>"
#   $env:JUMP_USER  = "ecs-user"                  # 可选，默认 ecs-user
#   $env:RDS_HOST   = "<pgm-xxx.pg.rds.aliyuncs.com>"
#   $env:REDIS_HOST = "<r-xxx.redis.rds.aliyuncs.com>"
#   $env:MQ_HOST    = "<rmq-xxx-vpc.cn-xxx.rmq.aliyuncs.com>"   # 可选，不开就不传
#   powershell -File scripts/cloud/start-tunnel.ps1
#
# OSS / SLS 不走本隧道（走公网地域 Endpoint，或由采集侧上报）。
#
# 停隧道：Ctrl+C；或 powershell -File scripts/cloud/stop-tunnel.ps1
#
# 安全说明：
#   - 只做端口转发，不传输密码；数据库口令仍由业务进程经 .env 提供。
#   - JUMP_* / *_HOST 会先过白名单校验（仅允许 A-Za-z0-9._:- ），避免被注入
#     到 ssh 命令行里 —— 这些值来自环境变量，可能被误设成带空格/分号的内容。

param(
    [string]$JumpHost = $env:JUMP_HOST,
    [string]$JumpUser = $(if ($env:JUMP_USER) { $env:JUMP_USER } else { "ecs-user" }),
    [int]$LocalPgPort = 15432,
    [int]$LocalRedisPort = 16379,
    [int]$LocalMqPort = 18080,
    [string]$RdsHost = $env:RDS_HOST,
    [int]$RdsPort = 5432,
    [string]$RedisHost = $env:REDIS_HOST,
    [int]$RedisPort = 6379,
    # 可从 MQ_ENDPOINT 拆出 host，也可显式给 MQ_HOST
    [string]$MqHost = $(if ($env:MQ_HOST) { $env:MQ_HOST } elseif ($env:MQ_ENDPOINT -match '^([^:/]+)') { $Matches[1] } else { "" }),
    [int]$MqPort = 8080,
    [switch]$SkipMq
)

$ErrorActionPreference = "Stop"

function Assert-SafeShellToken([string]$name, [string]$s) {
    if ([string]::IsNullOrWhiteSpace($s)) {
        Write-Error "set $name"
    }
    if ($s -notmatch '^[A-Za-z0-9._:-]+$') {
        Write-Error "$name contains unsafe characters (allow A-Za-z0-9._:- only)"
    }
}

Assert-SafeShellToken "JUMP_HOST" $JumpHost
Assert-SafeShellToken "JUMP_USER" $JumpUser
Assert-SafeShellToken "RDS_HOST" $RdsHost
Assert-SafeShellToken "REDIS_HOST" $RedisHost
$useMq = -not $SkipMq -and -not [string]::IsNullOrWhiteSpace($MqHost)
if ($useMq) {
    Assert-SafeShellToken "MQ_HOST" $MqHost
}

function Test-LocalPort([int]$Port) {
    $null -ne (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue)
}

foreach ($pair in @(
    @{ Port = $LocalPgPort; Name = "PG" },
    @{ Port = $LocalRedisPort; Name = "Redis" }
)) {
    if (Test-LocalPort $pair.Port) {
        Write-Host "WARN: local port $($pair.Port) ($($pair.Name)) already in use" -ForegroundColor Yellow
    }
}
if ($useMq -and (Test-LocalPort $LocalMqPort)) {
    Write-Host "WARN: local port $LocalMqPort (MQ) already in use" -ForegroundColor Yellow
}

$target = "{0}@{1}" -f $JumpUser, $JumpHost
$fwdPg = "{0}:{1}:{2}" -f $LocalPgPort, $RdsHost, $RdsPort
$fwdRedis = "{0}:{1}:{2}" -f $LocalRedisPort, $RedisHost, $RedisPort

Write-Host "Jump : $target"
Write-Host "PG   : 127.0.0.1:$LocalPgPort -> ${RdsHost}:$RdsPort"
Write-Host "Redis: 127.0.0.1:$LocalRedisPort -> ${RedisHost}:$RedisPort"
$sshArgs = @(
    "-N",
    "-o", "ExitOnForwardFailure=yes",
    "-o", "ServerAliveInterval=30",
    "-o", "ServerAliveCountMax=3",
    "-L", $fwdPg,
    "-L", $fwdRedis
)
if ($useMq) {
    $fwdMq = "{0}:{1}:{2}" -f $LocalMqPort, $MqHost, $MqPort
    Write-Host "MQ   : 127.0.0.1:$LocalMqPort -> ${MqHost}:$MqPort"
    $sshArgs += @("-L", $fwdMq)
} else {
    Write-Host "MQ   : (skipped — set MQ_HOST or MQ_ENDPOINT to enable)"
}
Write-Host "OSS/SLS: no tunnel (public / collector-side)"
Write-Host "Keep this window open; Ctrl+C to stop."
Write-Host ""

& ssh @sshArgs $target
