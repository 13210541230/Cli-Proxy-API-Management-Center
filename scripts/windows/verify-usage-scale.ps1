$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$usageService = Join-Path $repo 'usage-service'
$logDir = Join-Path $repo '.ccg/plan/usage-analytics-absorption/artifacts/verification'
$log = Join-Path $logDir 'usage-scale.log'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
Set-Location $usageService
$env:USAGE_SCALE_TEST = '1'
& go test -count=1 -timeout=30m -run '^TestAnalyticsProductionScale$' -v ./internal/analytics 2>&1 | Tee-Object -FilePath $log
$exitCode = $LASTEXITCODE
Remove-Item Env:USAGE_SCALE_TEST -ErrorAction SilentlyContinue
exit $exitCode
