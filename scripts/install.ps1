<#
.SYNOPSIS
    Builds mcpserver, bridge_app, and dd-launch and installs them to a stable
    location outside the repo, so ~/.claude.json, DD's launch shortcut, and
    dd-launch's idea of where bridge_app lives never point back into a source
    checkout or a temp directory.

.DESCRIPTION
    Re-run this after every `git pull` or local edit — it IS the deploy step.
    The install directory is %LOCALAPPDATA%\DungeondraftBridge\bin\; all three
    binaries are stamped with a version derived from `git describe`, which
    mcpserver reports as its MCP server version and bridge_app reports at
    /health, so a stale-vs-fresh build mismatch is never a guessing game.

.EXAMPLE
    .\scripts\install.ps1
#>

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$installDir = Join-Path $env:LOCALAPPDATA "DungeondraftBridge\bin"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null

$version = (git -C $repoRoot describe --tags --always --dirty 2>$null)
if (-not $version) { $version = "dev" }
Write-Host "Installing version $version to $installDir"

function Build-Binary {
    param(
        [string]$ModuleDir,
        [string]$PackagePath,
        [string]$OutName,
        [string]$VersionLdflags = ""
    )
    $out = Join-Path $installDir $OutName
    Write-Host "  building $OutName ..."
    Push-Location $ModuleDir
    try {
        if ($VersionLdflags) {
            & go build -ldflags $VersionLdflags -o $out $PackagePath
        } else {
            & go build -o $out $PackagePath
        }
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $OutName" }
    } finally {
        Pop-Location
    }
}

# mcpserver and bridge_app each report their own version (MCP serverInfo,
# bridge_app's /health) — dd-launch is a short-lived one-shot, nothing reads
# its version, so it isn't stamped.
Build-Binary -ModuleDir (Join-Path $repoRoot "mcpserver") -PackagePath "./cmd/mcpserver" -OutName "mcpserver.exe" -VersionLdflags "-X main.version=$version"
Build-Binary -ModuleDir (Join-Path $repoRoot "bridge_app") -PackagePath "./cmd/bridge_app" -OutName "bridge_app.exe" -VersionLdflags "-X main.version=$version"
Build-Binary -ModuleDir (Join-Path $repoRoot "bridge_app") -PackagePath "./cmd/dd-launch" -OutName "dd-launch.exe"

Write-Host ""
Write-Host "Installed to $installDir :"
Get-ChildItem $installDir -Filter *.exe | ForEach-Object { Write-Host "  $($_.Name)" }
Write-Host ""
Write-Host "If ~/.claude.json's mcpServers.dungeondraft.command doesn't already point at"
Write-Host "  $installDir\mcpserver.exe"
Write-Host "update it and reconnect (/mcp) in Claude Code."
