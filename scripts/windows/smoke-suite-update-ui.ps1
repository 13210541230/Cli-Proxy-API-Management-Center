param(
    [ValidateSet('Start', 'Stop')]
    [string]$Action = 'Start'
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$scratch = Join-Path $repoRoot 'build_tmp/suite-ui-smoke'

if ($Action -eq 'Stop') {
    $updater = Get-ChildItem -Path (Join-Path $scratch 'extracted') -Filter 'cpa-updater.exe' -File -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty FullName
    if (-not (Test-Path $updater)) { return }
    & $updater --stop
    if ($LASTEXITCODE -ne 0) { throw "Suite stop failed with exit code $LASTEXITCODE" }
    Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
    Write-Host 'Isolated suite UI smoke processes and scratch files removed.'
    return
}

if (Test-Path $scratch) { throw "Smoke directory already exists; stop or inspect it first: $scratch" }
$binDir = Join-Path $scratch 'bin'
$outputDir = Join-Path $scratch 'output'
$extractDir = Join-Path $scratch 'extracted'
New-Item -ItemType Directory -Path $binDir, $outputDir, $extractDir -Force | Out-Null

$managerBinary = Join-Path $repoRoot 'build/cpa-manager.exe'
$updaterBinary = Join-Path $repoRoot 'build/cpa-updater.exe'
$cpaSourceRoot = (Resolve-Path (Join-Path $repoRoot '..\CLIProxyAPI')).Path
$cpaPackage = (Resolve-Path (Join-Path $repoRoot '..\CLIProxyAPI-Suite_7.3.37_windows_amd64')).Path
$cpaExample = Join-Path $cpaSourceRoot 'config.example.yaml'
$cpaBinary = Join-Path $cpaPackage 'cli-proxy-api.exe'
$packageScript = Join-Path $cpaSourceRoot 'release/package-unified.sh'
foreach ($path in @($managerBinary, $updaterBinary, $cpaExample, $cpaBinary, $packageScript)) {
    if (-not (Test-Path $path -PathType Leaf)) { throw "Required smoke artifact is missing: $path" }
}

$packagedCPA = Join-Path $binDir 'cli-proxy-api.exe'
$packagedManager = Join-Path $binDir 'cpa-manager.exe'
$packagedUpdater = Join-Path $binDir 'cpa-updater.exe'
Copy-Item -LiteralPath $cpaBinary -Destination $packagedCPA
Copy-Item -LiteralPath $managerBinary -Destination $packagedManager
Copy-Item -LiteralPath $updaterBinary -Destination $packagedUpdater
& bash @(
    $packageScript,
    '--version', '7.3.37',
    '--manager-version', 'dev',
    '--os', 'windows',
    '--arch', 'amd64',
    '--server-bin', $packagedCPA,
    '--usage-bin', $packagedManager,
    '--updater-bin', $packagedUpdater,
    '--server-package-dir', $cpaPackage,
    '--output-dir', $outputDir
)
if ($LASTEXITCODE -ne 0) { throw "Unified package creation failed with exit code $LASTEXITCODE" }
$archivePath = Join-Path $outputDir 'CLIProxyAPI-Suite_7.3.37_windows_amd64.zip'
if (-not (Test-Path $archivePath)) { throw "Unified package not found: $archivePath" }
Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
$suiteRoot = Join-Path $extractDir 'CLIProxyAPI-Suite_7.3.37_windows_amd64'
$configPath = Join-Path $suiteRoot 'config.yaml'
$config = [IO.File]::ReadAllText((Join-Path $suiteRoot 'config.example.yaml'))
$config = $config -replace '(?m)^host:.*$', 'host: "127.0.0.1"'
$config = $config -replace '(?m)^port:\s*\d+\s*$', 'port: 18419'
$config = $config -replace '(?m)^auth-dir:.*$', 'auth-dir: "auths"'
$config = $config -replace '(?m)^  secret-key:.*$', '  secret-key: "suite-ui-smoke-key"'
$config = $config -replace '(?m)^  disable-control-panel:.*$', '  disable-control-panel: true'
[IO.File]::WriteAllText($configPath, $config, [Text.UTF8Encoding]::new($false))

$oldEnv = @{
    HTTP_ADDR = $env:HTTP_ADDR
    USAGE_DATA_DIR = $env:USAGE_DATA_DIR
    USAGE_DB_PATH = $env:USAGE_DB_PATH
    CPA_UPSTREAM_URL = $env:CPA_UPSTREAM_URL
    CPA_MANAGEMENT_KEY = $env:CPA_MANAGEMENT_KEY
}
try {
    $env:HTTP_ADDR = '127.0.0.1:18420'
    $env:USAGE_DATA_DIR = Join-Path $scratch 'data'
    $env:USAGE_DB_PATH = Join-Path $scratch 'data/usage.sqlite'
    $env:CPA_UPSTREAM_URL = 'http://127.0.0.1:18419'
    $env:CPA_MANAGEMENT_KEY = ''
    New-Item -ItemType Directory -Path $env:USAGE_DATA_DIR -Force | Out-Null

    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $suiteRoot 'start.ps1') --config $configPath
    if ($LASTEXITCODE -ne 0) { throw "Generated suite launcher failed with exit code $LASTEXITCODE" }

    $statePath = Join-Path $suiteRoot '.suite-runtime.json'
    if (-not (Test-Path $statePath)) { throw 'Launcher did not write .suite-runtime.json.' }
    $state = Get-Content -Raw $statePath | ConvertFrom-Json
    if (-not $state.cpa.pid -or -not $state.manager.pid -or $state.configPath -ne [IO.Path]::GetFullPath($configPath)) {
        throw 'Launcher runtime state does not contain both process IDs and the selected config path.'
    }
    if ($state.managerArguments -notcontains '--no-start-cpa') { throw 'Launcher did not disable Manager CPA auto-start.' }

    $cpaReady = $false
    $managerReady = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        Start-Sleep -Milliseconds 500
        try {
            $cpa = Invoke-WebRequest -Uri 'http://127.0.0.1:18419/healthz' -UseBasicParsing -TimeoutSec 2
            if ($cpa.StatusCode -eq 200) { $cpaReady = $true }
        } catch { }
        try {
            $manager = Invoke-WebRequest -Uri 'http://127.0.0.1:18420/health' -UseBasicParsing -TimeoutSec 2
            $panel = Invoke-WebRequest -Uri 'http://127.0.0.1:18420/management.html' -UseBasicParsing -TimeoutSec 2
            if ($manager.StatusCode -eq 200 -and $panel.StatusCode -eq 200) { $managerReady = $true }
        } catch { }
        if ($cpaReady -and $managerReady) { break }
    }
    if (-not $cpaReady -or -not $managerReady) { throw 'Launched CPA and Manager did not both become healthy.' }
    Write-Host 'Launcher verified: both services healthy, selected config saved, Manager auto-start suppressed.'
    Write-Host 'CPA: http://127.0.0.1:18419'
    Write-Host 'Manager panel: http://127.0.0.1:18420/management.html'
    Write-Host "When browser verification is complete, run this script with -Action Stop. Scratch: $scratch"
} catch {
    $updater = Join-Path $suiteRoot 'cpa-updater.exe'
    if (Test-Path $updater) {
        & $updater --stop 2>$null
    }
    Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
    throw
} finally {
    foreach ($name in $oldEnv.Keys) {
        if ($null -eq $oldEnv[$name]) { Remove-Item "Env:$name" -ErrorAction SilentlyContinue }
        else { Set-Item "Env:$name" $oldEnv[$name] }
    }
}
