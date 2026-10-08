# 经跳板机探活云 RDS：只跑一句 SELECT version()，确认「跳板机 -> 库」这段是通的。
#
# 必填：JUMP_HOST、RDS_HOST、DB_PASS（或用同名参数）
# 可选：JUMP_USER（默认 ecs-user）、DB_USER（默认 postgres）、DB_NAME（默认 postgres）
#
# 为什么绕这么一圈：RDS 只在 VPC 内可达，本机直连不通；经 ssh 到跳板机再连，
# 才能区分「网络不通」和「账号/库名错」。
#
# 口令传递方式（重要）：密码经 stdin 单行喂给远端的 `IFS= read -r PGPASSWORD`，
# **不进 argv、也不作为脚本正文**。进程列表与 shell history 里都看不到它。

param(
    [string]$JumpHost = $env:JUMP_HOST,
    [string]$JumpUser = $(if ($env:JUMP_USER) { $env:JUMP_USER } else { "ecs-user" }),
    [string]$RdsHost = $env:RDS_HOST,
    [string]$DbUser = $(if ($env:DB_USER) { $env:DB_USER } else { "postgres" }),
    [string]$DbName = $(if ($env:DB_NAME) { $env:DB_NAME } else { "postgres" }),
    [string]$DbPass = $env:DB_PASS
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($JumpHost)) { Write-Error "set JUMP_HOST or -JumpHost" }
if ([string]::IsNullOrWhiteSpace($RdsHost)) { Write-Error "set RDS_HOST or -RdsHost" }
if ([string]::IsNullOrWhiteSpace($DbPass)) { Write-Error "set DB_PASS (never hardcode in this script)" }

function Assert-SafeShellToken([string]$name, [string]$s) {
    if ($s -notmatch '^[A-Za-z0-9._:-]+$') {
        Write-Error "$name contains unsafe characters (allow A-Za-z0-9._:- only)"
    }
}

Assert-SafeShellToken "JUMP_HOST" $JumpHost
Assert-SafeShellToken "RDS_HOST" $RdsHost
Assert-SafeShellToken "DB_USER" $DbUser
Assert-SafeShellToken "DB_NAME" $DbName
Assert-SafeShellToken "JUMP_USER" $JumpUser

$target = "{0}@{1}" -f $JumpUser, $JumpHost
$remote = "IFS= read -r PGPASSWORD; export PGPASSWORD; psql -h $RdsHost -U $DbUser -d $DbName -c 'SELECT version();'; unset PGPASSWORD"

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = "ssh"
$psi.Arguments = "-o BatchMode=yes $target bash -c `"$remote`""
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UseShellExecute = $false
$p = [System.Diagnostics.Process]::Start($psi)
$p.StandardInput.WriteLine($DbPass)
$p.StandardInput.Close()
$stdout = $p.StandardOutput.ReadToEnd()
$stderr = $p.StandardError.ReadToEnd()
$p.WaitForExit()
if ($stdout) { Write-Host $stdout }
if ($stderr) { Write-Host $stderr }
exit $p.ExitCode
