# Thin forwarding layer to the Go CLI. It resolves a binary and invokes it; all
# argument parsing, validation and exit codes belong to cmd/oneagent.
#
# Kept for one release cycle so existing docs, CI jobs and user scripts keep
# working while the Go CLI becomes the only entry point. This wrapper no longer
# locates Python: Python is now only an external prerequisite of Aider's own
# installer, not of OneAgent.
#
# It deliberately does not build on demand. Callers run with a temporary HOME,
# and `go build` would write a module cache into it -- a side effect a wrapper
# has no business causing. Build once, then forward.
[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $OneAgentArgs
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

$Binary = $null
if ($env:ONEAGENT_CLI_BINARY) {
    $Binary = $env:ONEAGENT_CLI_BINARY
} else {
    $Local = Join-Path $RootDir "bin\oneagent.exe"
    if (Test-Path -LiteralPath $Local) {
        $Binary = $Local
    } else {
        $OnPath = Get-Command oneagent -ErrorAction SilentlyContinue
        if ($OnPath) {
            $Binary = $OnPath.Source
        }
    }
}

if (-not $Binary -or -not (Test-Path -LiteralPath $Binary)) {
    Write-Error "The OneAgent CLI was not found. Build it with: go build -o bin\oneagent.exe .\cmd\oneagent, or point ONEAGENT_CLI_BINARY at an existing binary."
    exit 3
}

& $Binary @OneAgentArgs
exit $LASTEXITCODE
