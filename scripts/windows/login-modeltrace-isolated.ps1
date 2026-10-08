param(
    [ValidateSet('local', 'live')]
    [string]$Mode = 'local'
)
$ErrorActionPreference = 'Stop'
$profile = Join-Path 'D:\C_projects\CLIProxyAPI-Suite_7.3.12_windows_amd64\modeltrace-validation' $Mode
$key = [System.IO.File]::ReadAllText((Join-Path $profile 'management-key.txt')).Trim()
$session = 'modeltrace-isolated-' + $Mode
# Read the private test key inside this process; do not print it or copy a source management secret.
& agent-browser --session $session open 'http://127.0.0.1:18327/management.html#/login'
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& agent-browser --session $session wait --fn "Boolean(document.querySelector('input[type=password]') || document.querySelector('a[href*=quota]'))"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$stateJson = & agent-browser --session $session --json eval "Boolean(document.querySelector('input[type=password]'))"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if (($stateJson | ConvertFrom-Json).data.result) {
    & agent-browser --session $session fill 'input[type=password]' $key
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & agent-browser --session $session press Enter
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & agent-browser --session $session wait --url '**/management.html#/'
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
& agent-browser --session $session open 'http://127.0.0.1:18327/management.html#/quota'
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& agent-browser --session $session snapshot -i
exit $LASTEXITCODE
