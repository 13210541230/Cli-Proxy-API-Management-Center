$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$usageService = Join-Path $repo 'usage-service'
$logDir = Join-Path $repo '.ccg/plan/usage-analytics-absorption/artifacts/verification'
$log = Join-Path $logDir 'go-test-all.log'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
Set-Location $usageService
& go test -count=1 ./... 2>&1 | Tee-Object -FilePath $log
$exitCode = $LASTEXITCODE
exit $exitCode
