<#
.SYNOPSIS
    Installs everything MapForge needs: mcpserver, bridge_app and dd-launch,
    the Terraform provider, and the link that lets Dungeondraft load the mod.

.DESCRIPTION
    Re-run this after every `git pull` or local edit: it IS the deploy step,
    and it is safe to run again.

    - mcpserver, bridge_app and dd-launch go to
      %LOCALAPPDATA%\DungeondraftBridge\bin\, outside the repo, so
      ~/.claude.json, the Dungeondraft shortcut and dd-launch never point into
      a source checkout. mcpserver and bridge_app are stamped with a version
      from `git describe`, reported in MCP serverInfo and at /health.
    - The provider goes to Terraform's implied local mirror,
      %APPDATA%\terraform.d\plugins\registry.terraform.io\jamesloftis\dungeondraft\<version>\windows_amd64,
      which `terraform init` checks with no CLI config. The version comes from
      provider/internal/ddprovider/provider.go; after changing it, run
      `terraform init -upgrade` in each Terraform folder that uses the provider.
    - <ModsDir>\dungeondraft-mcp-bridge-agent becomes a junction to this
      checkout's mod/dungeondraft-mcp-bridge-agent. A junction to another
      checkout is replaced; a real folder there is left alone and the script
      stops.

    Close every Claude Code session first: each one runs mcpserver.exe, and
    Windows will not overwrite a running program.

.PARAMETER ModsDir
    Dungeondraft's mods folder. Defaults to %APPDATA%\Dungeondraft\Mods.

.EXAMPLE
    .\scripts\install.ps1

.EXAMPLE
    .\scripts\install.ps1 -ModsDir D:\Dungeondraft\Mods
#>
param(
    [string]$ModsDir = (Join-Path $env:APPDATA "Dungeondraft\Mods")
)

$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "ModJunction.ps1")

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
        [string]$OutFile,
        [string]$VersionLdflags = ""
    )
    Write-Host "  building $(Split-Path -Leaf $OutFile) ..."
    Push-Location $ModuleDir
    try {
        if ($VersionLdflags) {
            & go build -ldflags $VersionLdflags -o $OutFile $PackagePath
        } else {
            & go build -o $OutFile $PackagePath
        }
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $OutFile" }
    } finally {
        Pop-Location
    }
}

function Get-ProviderVersion {
    $providerSource = Join-Path $repoRoot "provider\internal\ddprovider\provider.go"
    $versionLine = Select-String -Path $providerSource -Pattern 'version\s*=\s*"([^"]+)"' | Select-Object -First 1
    if (-not $versionLine) { throw "Could not find the version const in $providerSource" }
    return $versionLine.Matches[0].Groups[1].Value
}

# mcpserver and bridge_app each report their own version (MCP serverInfo,
# bridge_app's /health); dd-launch is a short-lived one-shot that nothing asks,
# so it isn't stamped.
Build-Binary -ModuleDir (Join-Path $repoRoot "mcpserver") -PackagePath "./cmd/mcpserver" -OutFile (Join-Path $installDir "mcpserver.exe") -VersionLdflags "-X main.version=$version"
Build-Binary -ModuleDir (Join-Path $repoRoot "bridge_app") -PackagePath "./cmd/bridge_app" -OutFile (Join-Path $installDir "bridge_app.exe") -VersionLdflags "-X main.version=$version"
Build-Binary -ModuleDir (Join-Path $repoRoot "bridge_app") -PackagePath "./cmd/dd-launch" -OutFile (Join-Path $installDir "dd-launch.exe")

# Terraform selects the provider by the version in this path and file name, not
# by what the binary reports.
$providerVersion = Get-ProviderVersion
$mirrorDir = Join-Path $env:APPDATA "terraform.d\plugins\registry.terraform.io\jamesloftis\dungeondraft\$providerVersion\windows_amd64"
New-Item -ItemType Directory -Force -Path $mirrorDir | Out-Null
$providerExe = Join-Path $mirrorDir "terraform-provider-dungeondraft_v$providerVersion.exe"
Build-Binary -ModuleDir (Join-Path $repoRoot "provider") -PackagePath "." -OutFile $providerExe

$modSource = Join-Path $repoRoot "mod\dungeondraft-mcp-bridge-agent"
Set-ModJunction -ModsDir $ModsDir -Source $modSource

Write-Host ""
Write-Host "Installed to $installDir :"
Get-ChildItem $installDir -Filter *.exe | ForEach-Object { Write-Host "  $($_.Name)" }
Write-Host "Provider v$providerVersion installed to $providerExe"
Write-Host "Mod linked: $(Join-Path $ModsDir 'dungeondraft-mcp-bridge-agent') -> $modSource"
Write-Host ""
Write-Host "If ~/.claude.json's mcpServers.dungeondraft.command doesn't already point at"
Write-Host "  $installDir\mcpserver.exe"
Write-Host "update it and reconnect (/mcp) in Claude Code."
Write-Host "Changed the provider version? Run 'terraform init -upgrade' in each Terraform folder that uses it."
