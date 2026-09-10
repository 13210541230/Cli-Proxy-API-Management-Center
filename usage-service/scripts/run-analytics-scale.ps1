$ErrorActionPreference = 'Stop'
$env:USAGE_SCALE_TEST = '1'
Set-Location (Split-Path -Parent $PSScriptRoot)
go test ./internal/analytics -run '^TestAnalyticsProductionScale$' -count=1 -v
exit $LASTEXITCODE
