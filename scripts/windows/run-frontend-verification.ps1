$ErrorActionPreference = 'Continue'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$logDir = Join-Path $repo '.ccg/plan/usage-analytics-absorption/artifacts/verification'
$log = Join-Path $logDir 'frontend-final.log'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
Set-Location $repo
& npm run type-check 2>&1 | Tee-Object -FilePath $log
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& npm test 2>&1 | Tee-Object -FilePath $log -Append
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& npm run build 2>&1 | Tee-Object -FilePath $log -Append
$exitCode = $LASTEXITCODE
exit $exitCode
