$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $repo
$version = if ($env:VERSION) { $env:VERSION } else { 'dev' }
$commit = (& git rev-parse --short HEAD 2>$null)
if (-not $commit) { $commit = 'none' }
$buildDate = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')

Write-Host '== Frontend single-file build =='
& npm run build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$dist = Join-Path $repo 'dist/index.html'
$embedded = Join-Path $repo 'usage-service/internal/httpapi/web/management.html'
$binary = Join-Path $repo 'bin/cpa-manager.exe'
$updater = Join-Path $repo 'bin/cpa-updater.exe'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $binary) | Out-Null
$distContent = [System.IO.File]::ReadAllText($dist)
$normalizedContent = $distContent.Replace("`r`n", "`n")
$hiddenCodePoints = @(0x0002, 0x0018, 0x001F, 0x007F, 0x0080, 0x0085, 0x009F, 0x061C, 0x200B, 0x200E, 0x200F, 0x202D, 0x202E, 0x2066, 0x2067, 0x2069, 0xFEFF)
foreach ($codePoint in $hiddenCodePoints) {
  $normalizedContent = $normalizedContent.Replace([string][char]$codePoint, ('\u{0:X4}' -f $codePoint))
}
[System.IO.File]::WriteAllText($embedded, $normalizedContent, [System.Text.UTF8Encoding]::new($false))

Write-Host '== Embedded binary build =='
$env:CGO_ENABLED = '0'
Push-Location (Join-Path $repo 'usage-service')
try {
  $ldflags = "-s -w -X github.com/seakee/cpa-manager/usage-service/internal/buildinfo.Version=$version -X github.com/seakee/cpa-manager/usage-service/internal/buildinfo.Commit=$commit -X github.com/seakee/cpa-manager/usage-service/internal/buildinfo.BuildDate=$buildDate"
  & go build -trimpath -ldflags $ldflags -o $binary ./cmd/cpa-manager
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
  & go build -trimpath -ldflags '-s -w' -o $updater ./cmd/cpa-updater
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
  Pop-Location
}
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$distBytes = [System.Text.Encoding]::UTF8.GetBytes($normalizedContent)
$embeddedHash = (Get-FileHash -Algorithm SHA256 $embedded).Hash
$sha256 = [System.Security.Cryptography.SHA256]::Create()
try {
  $distHash = ([BitConverter]::ToString($sha256.ComputeHash($distBytes))).Replace('-', '')
} finally {
  $sha256.Dispose()
}
$binaryHash = (Get-FileHash -Algorithm SHA256 $binary).Hash
$updaterHash = (Get-FileHash -Algorithm SHA256 $updater).Hash
Write-Host "dist/index.html (normalized): $($distBytes.Length) bytes SHA256 $distHash"
Write-Host "management.html: $((Get-Item $embedded).Length) bytes SHA256 $embeddedHash"
Write-Host "cpa-manager.exe: $((Get-Item $binary).Length) bytes SHA256 $binaryHash"
Write-Host "cpa-updater.exe: $((Get-Item $updater).Length) bytes SHA256 $updaterHash"
if ($distHash -ne $embeddedHash) { Write-Error 'Embedded HTML hash mismatch'; exit 1 }
Write-Host 'Embedded source hash verified.'
