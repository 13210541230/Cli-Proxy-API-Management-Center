$ErrorActionPreference = 'Continue'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$logDir = Join-Path $repo '.ccg/plan/usage-analytics-absorption/artifacts/verification'
$log = Join-Path $logDir 'frontend-lint-final.log'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
Set-Location $repo
& npm run lint 2>&1 | Tee-Object -FilePath $log
$exitCode = $LASTEXITCODE
exit $exitCode
