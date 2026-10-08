$ErrorActionPreference = 'Continue'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $repo
$logs = Join-Path $repo 'logs'
New-Item -ItemType Directory -Force -Path $logs | Out-Null
& node scripts/modeltrace-browser-fixture.mjs 2>&1 | Tee-Object -FilePath (Join-Path $logs 'modeltrace-browser-fixture.log')
exit $LASTEXITCODE
