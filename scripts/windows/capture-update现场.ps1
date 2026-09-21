# capture-update现场.ps1 — 快速轮询 sidecar/锁/staging/进程，写日志到当前目录
# 用法：.\scripts\windows\capture-update现场.ps1 -SuiteDir "D:\C_projects\CLIProxyAPI-Suite_7.2.158_windows_amd64"
param(
  [Parameter(Mandatory=$true)]
  [string]$SuiteDir,
  [int]$IntervalMs = 100,
  [int]$TimeoutSec = 180
)
$ErrorActionPreference = 'SilentlyContinue'
$deadline = (Get-Date).AddSeconds($TimeoutSec)
$log = Join-Path $SuiteDir "update-repro-$(Get-Date -Format 'yyyyMMdd-HHmmss').log"
"capture started: $(Get-Date -o)" | Out-File $log -Encoding utf8
$suite = $SuiteDir
$sidecar = Join-Path $suite 'data\usage.sqlite.update.json'
$lockFile = Join-Path $suite 'data\usage.sqlite.update.json.lock'
$cpaLock = Join-Path $suite 'cli-proxy-api.exe.cpa-manager.lock'
$tempBase = Join-Path $env:TEMP 'cpa-manager-update-*'

function Snap($note='') {
    $ts = Get-Date -Format 'HH:mm:ss.fff'
    $lines = @("[$ts] $note")
    # sidecar
    if (Test-Path $sidecar) {
        $lines += "SIDECAR: " + (Get-Content $sidecar -Raw -ErrorAction SilentlyContinue)
    } else {
        $lines += "SIDECAR: <missing>"
    }
    # lock
    if (Test-Path $lockFile) {
        $lines += "LOCK: " + (Get-Content $lockFile -Raw -ErrorAction SilentlyContinue)
    } else {
        $lines += "LOCK: <missing>"
    }
    # manager lock
    if (Test-Path $cpaLock) {
        $lines += "CPA_LOCK: " + (Get-Content $cpaLock -Raw -ErrorAction SilentlyContinue)
    } else {
        $lines += "CPA_LOCK: <missing>"
    }
    # processes
    $procs = Get-Process | Where-Object {$_.ProcessName -match 'cpa-manager|cli-proxy-api|cpa-updater'} |
             Select-Object Id,ProcessName,StartTime | Format-Table -AutoSize | Out-String
    $lines += "PROCS: " + ($procs -replace "`n"," | ")
    # temp staging dirs
    $stagingDirs = Get-ChildItem -Path $env:TEMP -Directory -Filter 'cpa-manager-update-*' -ErrorAction SilentlyContinue
    $lines += "STAGING_DIRS: " + (($stagingDirs | ForEach-Object { $_.FullName }) -join ', ')
    # staged binaries if exists
    foreach ($d in $stagingDirs) {
        $extracted = Join-Path $d.FullName 'extracted'
        if (Test-Path $extracted) {
            $files = Get-ChildItem $extracted -Recurse -File -Filter '*.exe' -ErrorAction SilentlyContinue |
                     Select-Object FullName,Length | Format-Table -AutoSize | Out-String
            $lines += "STAGED_EXES($($d.Name)): " + ($files -replace "`n"," | ")
        }
    }
    ($lines -join "`n") | Out-File $log -Append -Encoding utf8
}
Snap "INIT"
while ((Get-Date) -lt $deadline) {
    # detect state change by reading sidecar quickly
    $state = ''
    if (Test-Path $sidecar) {
        try { $j = Get-Content $sidecar -Raw | ConvertFrom-Json; $state = $j.state } catch {}
    }
    if ($state -ne '') { Snap "state=$state" }
    Start-Sleep -Milliseconds $IntervalMs
}
Snap "END"
Write-Host "capture saved: $log"
