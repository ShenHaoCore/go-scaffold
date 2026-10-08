# 停掉本机由 start-tunnel.ps1 建立的 SSH 隧道（PG / Redis / MQ）。
#
# 判定方式：扫描 ssh.exe 的命令行，命中 -L <本地端口>: 特征才杀 ——
# 不会误伤你连别的机器的普通 ssh 会话。
#
# 用法：powershell -File scripts/cloud/stop-tunnel.ps1

param(
    [int]$LocalPgPort = 15432,
    [int]$LocalRedisPort = 16379,
    [int]$LocalMqPort = 18080
)

# 这里刻意用 Continue：某个进程取 CommandLine 失败（权限/已退出）不该中断整轮清理
$ErrorActionPreference = "Continue"
$killed = 0
$ports = @($LocalPgPort, $LocalRedisPort, $LocalMqPort)

Get-CimInstance Win32_Process -Filter "Name='ssh.exe'" | ForEach-Object {
    $cmd = $_.CommandLine
    if ($null -eq $cmd) { return }
    $hit = $false
    foreach ($p in $ports) {
        if ($cmd -match "-L\s+${p}:") { $hit = $true; break }
    }
    if ($hit) {
        Write-Host "Stopping PID $($_.ProcessId)"
        Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        $killed++
    }
}

if ($killed -eq 0) {
    Write-Host "No ssh tunnel found for ports $($ports -join ' / ')"
} else {
    Write-Host "Stopped $killed process(es)"
}
