$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$usageService = Join-Path $repo 'usage-service'
$logDir = Join-Path $repo '.ccg/plan/usage-analytics-absorption/artifacts/verification'
$log = Join-Path $logDir 'go-race.log'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
Set-Location $usageService
& go test -race -count=1 ./internal/store ./internal/rollup ./internal/collector ./internal/analytics 2>&1 | Tee-Object -FilePath $log
$exitCode = $LASTEXITCODE
exit $exitCode
