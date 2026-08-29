$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location $repo

Write-Host '== Frontend single-file build =='
& npm run build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$dist = Join-Path $repo 'dist/index.html'
$embedded = Join-Path $repo 'usage-service/internal/httpapi/web/management.html'
$binary = Join-Path $repo 'bin/cpa-manager.exe'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $binary) | Out-Null
$distContent = [System.IO.File]::ReadAllText($dist)
$normalizedContent = $distContent.Replace("`r`n", "`n")
[System.IO.File]::WriteAllText($embedded, $normalizedContent, [System.Text.UTF8Encoding]::new($false))

Write-Host '== Embedded binary build =='
$env:CGO_ENABLED = '0'
Push-Location (Join-Path $repo 'usage-service')
try {
  & go build -trimpath -ldflags '-s -w' -o $binary ./cmd/cpa-manager
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
Write-Host "dist/index.html (normalized): $($distBytes.Length) bytes SHA256 $distHash"
Write-Host "management.html: $((Get-Item $embedded).Length) bytes SHA256 $embeddedHash"
Write-Host "cpa-manager.exe: $((Get-Item $binary).Length) bytes SHA256 $binaryHash"
if ($distHash -ne $embeddedHash) { Write-Error 'Embedded HTML hash mismatch'; exit 1 }
Write-Host 'Embedded source hash verified.'
