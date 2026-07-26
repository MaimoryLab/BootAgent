[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $OneAgentArgs
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

$Python = Get-Command python -ErrorAction SilentlyContinue
if (-not $Python) {
    $Python = Get-Command py -ErrorAction SilentlyContinue
}
if (-not $Python) {
    Write-Error "Python 3.12+ is required to run the source launcher. Use the self-contained OneAgent package if Python is unavailable."
    exit 3
}

$env:PYTHONPATH = if ($env:PYTHONPATH) { "$RootDir;$env:PYTHONPATH" } else { $RootDir }
if ($Python.Name -eq "py.exe" -or $Python.Name -eq "py") {
    & $Python.Source -3 -c "import sys; raise SystemExit(0 if sys.version_info >= (3, 12) else 1)"
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Python 3.12+ is required to run the source launcher."
        exit 3
    }
    & $Python.Source -3 -m oneagent.cli @OneAgentArgs
} else {
    & $Python.Source -c "import sys; raise SystemExit(0 if sys.version_info >= (3, 12) else 1)"
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Python 3.12+ is required to run the source launcher."
        exit 3
    }
    & $Python.Source -m oneagent.cli @OneAgentArgs
}
exit $LASTEXITCODE
