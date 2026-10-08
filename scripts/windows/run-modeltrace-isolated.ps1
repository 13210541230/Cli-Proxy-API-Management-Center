param(
    [ValidateSet('local', 'live')]
    [string]$Mode = 'local',
    [switch]$PrepareOnly
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$deployment = Join-Path 'D:\C_projects\CLIProxyAPI-Suite_7.3.12_windows_amd64\modeltrace-validation' $Mode
$binary = 'D:\C_projects\CLIProxyAPI\build\cpa-modeltrace-verify.exe'
Set-Location $repo
if (Get-NetTCPConnection -State Listen -LocalPort 18327 -ErrorAction SilentlyContinue) {
    throw 'Port 18327 is occupied. Refusing to modify an active profile or stop an unrelated process.'
}
& python scripts/prepare-modeltrace-isolation.py --mode $Mode --binary $binary
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if ($PrepareOnly) { exit 0 }
Set-Location $deployment
$env:MANAGEMENT_STATIC_PATH = Join-Path $deployment 'static'
$env:WRITABLE_PATH = $deployment
$ErrorActionPreference = 'Continue'
& .\cli-proxy-api-modeltrace.exe -config .\config.yaml 2>&1 | Tee-Object -FilePath (Join-Path $deployment 'logs\server.log')
exit $LASTEXITCODE
