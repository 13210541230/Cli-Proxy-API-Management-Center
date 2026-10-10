param(
    [ValidateSet('Focused', 'Full', 'Frontend', 'Embedded', 'Package', 'CrossCompile')]
    [string]$Phase = 'Focused'
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$logDir = Join-Path $repoRoot 'logs'
New-Item -ItemType Directory -Path $logDir -Force | Out-Null
$logPath = Join-Path $logDir "verify-suite-update-$Phase.log"
if (Test-Path $logPath) { Remove-Item $logPath -Force }

function Invoke-LoggedCommand {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $Executable @Arguments 2>&1 | Tee-Object -FilePath $logPath -Append
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($exitCode -ne 0) {
        throw "$Executable failed with exit code $exitCode"
    }
}

switch ($Phase) {
    'Focused' {
        Push-Location (Join-Path $repoRoot 'usage-service')
        try {
            Invoke-LoggedCommand 'go' @('test', './internal/update', './internal/httpapi', './cmd/cpa-updater', './cmd/cpa-manager')
        }
        finally {
            Pop-Location
        }
    }
    'Full' {
        Push-Location (Join-Path $repoRoot 'usage-service')
        try {
            Invoke-LoggedCommand 'go' @('test', './...')
        }
        finally {
            Pop-Location
        }
    }
    'Frontend' {
        Push-Location $repoRoot
        try {
            Invoke-LoggedCommand 'npm.cmd' @('test')
            Invoke-LoggedCommand 'npm.cmd' @('run', 'build')
        }
        finally {
            Pop-Location
        }
    }
    'Embedded' {
        Push-Location $repoRoot
        try {
            Invoke-LoggedCommand 'npm.cmd' @('run', 'build')
            Invoke-LoggedCommand 'python' @('bin/release/normalize-embedded-html.py', 'dist/index.html', 'usage-service/internal/httpapi/web/management.html')
            $buildDir = Join-Path $repoRoot 'build'
            New-Item -ItemType Directory -Path $buildDir -Force | Out-Null
            $commit = (& git rev-parse --short HEAD 2>$null)
            if (-not $commit) { $commit = 'none' }
            $buildDate = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
            $ldflags = "-s -w -X github.com/seakee/cpa-manager/usage-service/internal/buildinfo.Version=dev -X github.com/seakee/cpa-manager/usage-service/internal/buildinfo.Commit=$commit -X github.com/seakee/cpa-manager/usage-service/internal/buildinfo.BuildDate=$buildDate"
            $env:CGO_ENABLED = '0'
            Push-Location (Join-Path $repoRoot 'usage-service')
            try {
                Invoke-LoggedCommand 'go' @('build', '-trimpath', '-ldflags', $ldflags, '-o', (Join-Path $buildDir 'cpa-manager.exe'), './cmd/cpa-manager')
                Invoke-LoggedCommand 'go' @('build', '-trimpath', '-ldflags', $ldflags, '-o', (Join-Path $buildDir 'cpa-updater.exe'), './cmd/cpa-updater')
            }
            finally {
                Pop-Location
            }
        }
        finally {
            Pop-Location
        }
    }
    'CrossCompile' {
        $scratch = Join-Path $repoRoot ('build_tmp/suite-cross-compile-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $scratch -Force | Out-Null
        $oldGOOS = $env:GOOS
        $oldGOARCH = $env:GOARCH
        $oldCGO = $env:CGO_ENABLED
        try {
            $env:CGO_ENABLED = '0'
            Push-Location (Join-Path $repoRoot 'usage-service')
            try {
                foreach ($target in @('linux/amd64', 'darwin/arm64')) {
                    $parts = $target.Split('/')
                    $goos = $parts[0]
                    $goarch = $parts[1]
                    $env:GOOS = $goos
                    $env:GOARCH = $goarch
                    Invoke-LoggedCommand 'go' @('build', '-trimpath', '-o', (Join-Path $scratch "$goos-$goarch-cpa-manager"), './cmd/cpa-manager')
                    Invoke-LoggedCommand 'go' @('build', '-trimpath', '-o', (Join-Path $scratch "$goos-$goarch-cpa-updater"), './cmd/cpa-updater')
                }
            }
            finally {
                Pop-Location
            }
        }
        finally {
            $env:GOOS = $oldGOOS
            $env:GOARCH = $oldGOARCH
            $env:CGO_ENABLED = $oldCGO
            Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
        }
    }
    'Package' {
        $suiteRoot = (Resolve-Path (Join-Path $repoRoot '..\CLIProxyAPI')).Path
        $cpaPackage = (Resolve-Path (Join-Path $repoRoot '..\CLIProxyAPI-Suite_7.3.37_windows_amd64')).Path
        $packageScript = Join-Path $suiteRoot 'release/package-unified.sh'
        $scratch = Join-Path $repoRoot ('build_tmp/suite-package-verify-' + [Guid]::NewGuid().ToString('N'))
        $binDir = Join-Path $scratch 'bin'
        $outputDir = Join-Path $scratch 'output'
        New-Item -ItemType Directory -Path $binDir, $outputDir -Force | Out-Null
        try {
            $fakeCPA = Join-Path $binDir 'cli-proxy-api.exe'
            $fakeManager = Join-Path $binDir 'cpa-manager.exe'
            $fakeUpdater = Join-Path $binDir 'cpa-updater.exe'
            [IO.File]::WriteAllBytes($fakeCPA, [byte[]](0x4D, 0x5A, 0x01))
            [IO.File]::WriteAllBytes($fakeManager, [byte[]](0x4D, 0x5A, 0x02))
            [IO.File]::WriteAllBytes($fakeUpdater, [byte[]](0x4D, 0x5A, 0x03))
            Invoke-LoggedCommand 'bash' @($packageScript, '--version', 'v7.3.35', '--manager-version', '1.24.7', '--os', 'windows', '--arch', 'amd64', '--server-bin', $fakeCPA, '--usage-bin', $fakeManager, '--updater-bin', $fakeUpdater, '--output-dir', $outputDir)
            $archivePath = Join-Path $outputDir 'CLIProxyAPI-Suite_7.3.35_windows_amd64.zip'
            if (-not (Test-Path $archivePath)) { throw "Unified package not found: $archivePath" }
            $extractDir = Join-Path $scratch 'extracted'
            Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
            $packageDir = Join-Path $extractDir 'CLIProxyAPI-Suite_7.3.35_windows_amd64'
            $metadata = Get-Content -Raw (Join-Path $packageDir 'suite-version.json') | ConvertFrom-Json
            if ($metadata.cpaVersion -ne '7.3.35' -or $metadata.managerVersion -ne '1.24.7') { throw 'Suite version metadata mismatch.' }
            foreach ($file in @('start.bat', 'start.ps1', 'stop.bat', 'SUITE-README.md', 'SUITE-README_CN.md')) {
                if (-not (Test-Path (Join-Path $packageDir $file))) { throw "Package file is missing: $file" }
            }
            $tokens = $null
            $parseErrors = $null
            [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $packageDir 'start.ps1'), [ref]$tokens, [ref]$parseErrors) | Out-Null
            if ($parseErrors.Count -gt 0) { throw "Generated start.ps1 has a syntax error: $($parseErrors[0].Message)" }

            $linuxCPA = Join-Path $binDir 'cli-proxy-api'
            $linuxManager = Join-Path $binDir 'cpa-manager'
            $linuxUpdater = Join-Path $binDir 'cpa-updater'
            [IO.File]::WriteAllBytes($linuxCPA, [byte[]](0x7F, 0x45, 0x4C, 0x46))
            [IO.File]::WriteAllBytes($linuxManager, [byte[]](0x7F, 0x45, 0x4C, 0x46, 0x01))
            [IO.File]::WriteAllBytes($linuxUpdater, [byte[]](0x7F, 0x45, 0x4C, 0x46, 0x02))
            $linuxOutput = Join-Path $scratch 'linux-output'
            $linuxExtract = Join-Path $scratch 'linux-extracted'
            New-Item -ItemType Directory -Path $linuxOutput, $linuxExtract -Force | Out-Null
            Invoke-LoggedCommand 'bash' @($packageScript, '--version', 'v7.3.35', '--manager-version', '1.24.7', '--os', 'linux', '--arch', 'amd64', '--server-bin', $linuxCPA, '--usage-bin', $linuxManager, '--updater-bin', $linuxUpdater, '--server-package-dir', $cpaPackage, '--output-dir', $linuxOutput)
            $linuxArchive = Join-Path $linuxOutput 'CLIProxyAPI-Suite_7.3.35_linux_amd64.tar.gz'
            if (-not (Test-Path $linuxArchive)) { throw "Linux unified package not found: $linuxArchive" }
            $extractScript = Join-Path $scratch 'extract-tar.py'
            $extractScriptContents = @'
import sys
import tarfile
with tarfile.open(sys.argv[1], mode="r:gz") as archive:
    archive.extractall(path=sys.argv[2])
'@
            [IO.File]::WriteAllText($extractScript, $extractScriptContents, [Text.UTF8Encoding]::new($false))
            Invoke-LoggedCommand 'python' @($extractScript, $linuxArchive, $linuxExtract)
            $linuxPackageDir = Join-Path $linuxExtract 'CLIProxyAPI-Suite_7.3.35_linux_amd64'
            foreach ($file in @('start.sh', 'stop.sh', 'SUITE-README.md', 'SUITE-README_CN.md')) {
                if (-not (Test-Path (Join-Path $linuxPackageDir $file))) { throw "Linux package file is missing: $file" }
            }
            Invoke-LoggedCommand 'bash' @('-n', (Join-Path $linuxPackageDir 'start.sh'))
            Invoke-LoggedCommand 'bash' @('-n', (Join-Path $linuxPackageDir 'stop.sh'))
        }
        finally {
            Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
        }
    }
}

Write-Host "Verification phase $Phase passed. Log: $logPath"
