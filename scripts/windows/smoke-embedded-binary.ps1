$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$binary = Join-Path $repo 'bin/cpa-manager.exe'
if (-not (Test-Path $binary)) { throw "Binary not found: $binary" }

$port = 18397
$dataDir = Join-Path $env:TEMP ("cpa-manager-smoke-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
$oldEnv = @{
  HTTP_ADDR = $env:HTTP_ADDR
  USAGE_DATA_DIR = $env:USAGE_DATA_DIR
  USAGE_DB_PATH = $env:USAGE_DB_PATH
  CPA_UPSTREAM_URL = $env:CPA_UPSTREAM_URL
  CPA_MANAGEMENT_KEY = $env:CPA_MANAGEMENT_KEY
}
$process = $null
try {
  $env:HTTP_ADDR = "127.0.0.1:$port"
  $env:USAGE_DATA_DIR = $dataDir
  $env:USAGE_DB_PATH = Join-Path $dataDir 'usage.sqlite'
  Remove-Item Env:CPA_UPSTREAM_URL -ErrorAction SilentlyContinue
  Remove-Item Env:CPA_MANAGEMENT_KEY -ErrorAction SilentlyContinue

  $process = Start-Process -FilePath $binary -WorkingDirectory $repo -PassThru -WindowStyle Hidden
  $panelUri = "http://127.0.0.1:$port/management.html"
  $healthUri = "http://127.0.0.1:$port/health"
  $ready = $false
  for ($attempt = 0; $attempt -lt 40; $attempt++) {
    Start-Sleep -Milliseconds 250
    try {
      $health = Invoke-WebRequest -Uri $healthUri -UseBasicParsing -TimeoutSec 2
      $panel = Invoke-WebRequest -Uri $panelUri -UseBasicParsing -TimeoutSec 2
      if ($health.StatusCode -eq 200 -and $panel.StatusCode -eq 200) {
        $html = [string]$panel.Content
        if ($html.Contains('<div id="root">') -and
            $html.Contains('local_runtime_title') -and
            $html.Contains('suite_update_now') -and
            $html.Length -gt 1000000) {
          $ready = $true
          Write-Host "HTTP health: $($health.StatusCode)"
          Write-Host "Embedded panel: $($panel.StatusCode), $($html.Length) bytes"
          Write-Host 'React root and embedded content verified.'
          break
        }
      }
    } catch {
      if ($process.HasExited) { throw "Binary exited early with code $($process.ExitCode)" }
    }
  }
  if (-not $ready) { throw 'Embedded binary did not serve a valid management panel in time.' }
} finally {
  if ($process -and -not $process.HasExited) {
    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    $process.WaitForExit(5000)
  }
  foreach ($name in $oldEnv.Keys) {
    $value = $oldEnv[$name]
    if ($null -eq $value) {
      Remove-Item "Env:$name" -ErrorAction SilentlyContinue
    } else {
      Set-Item "Env:$name" $value
    }
  }
  Remove-Item -Recurse -Force $dataDir -ErrorAction SilentlyContinue
}
