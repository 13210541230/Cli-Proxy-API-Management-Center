param(
    [ValidateSet('Red', 'Test', 'Build', 'Lint', 'All')]
    [string]$Mode = 'All'
)
$ErrorActionPreference = 'Continue'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $repo
$logs = Join-Path $repo 'logs'
New-Item -ItemType Directory -Force -Path $logs | Out-Null
$log = Join-Path $logs ('modeltrace-frontend-' + $Mode.ToLowerInvariant() + '.log')
if (Test-Path $log) { Remove-Item $log }
if ($Mode -in @('Red', 'Test', 'All')) {
    & node node_modules/vitest/vitest.mjs run src/services/api/modelTrace.test.ts src/components/quota/modelTracePresentation.test.ts src/components/quota/QuotaCard.modelTrace.test.tsx src/components/quota/CodexModelTraceSection.test.tsx 2>&1 | Tee-Object -FilePath $log -Append
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
if ($Mode -in @('Build', 'All')) {
    & node node_modules/typescript/bin/tsc 2>&1 | Tee-Object -FilePath $log -Append
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & node node_modules/vite/bin/vite.js build 2>&1 | Tee-Object -FilePath $log -Append
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
if ($Mode -in @('Lint', 'All')) {
    & node node_modules/eslint/bin/eslint.js . --ext ts,tsx --report-unused-disable-directives 2>&1 | Tee-Object -FilePath $log -Append
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
exit 0
