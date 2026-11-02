<#
.SYNOPSIS
    Builds the provider and installs it into Terraform's implied local mirror
    directory, so `terraform init` finds it automatically -- no dev_overrides,
    no .terraformrc editing, works from any working directory forever.

.DESCRIPTION
    Terraform automatically checks %APPDATA%\terraform.d\plugins as a local
    mirror for any provider source address it can't resolve via a real
    registry (see the CLI config docs' "Implied Local Mirror Directories").
    Since registry.terraform.io/jamesloftis/dungeondraft isn't a published
    registry provider, dropping the binary in the conventional path there is
    all `terraform init` needs -- this script IS the deploy step; re-run it
    after every edit, then `terraform init -upgrade` once per Terraform
    working directory to pick up a version change.

    The version here must match internal/ddprovider/provider.go's `version`
    const (read automatically below) -- Terraform selects/validates the
    provider by the version encoded in the install path/filename, not just
    whatever the binary reports at runtime.

.EXAMPLE
    .\scripts\install.ps1
#>

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$providerFile = Join-Path $repoRoot "internal\ddprovider\provider.go"

$versionLine = Select-String -Path $providerFile -Pattern 'version\s*=\s*"([^"]+)"' | Select-Object -First 1
if (-not $versionLine) { throw "Could not find the version const in $providerFile" }
$version = $versionLine.Matches[0].Groups[1].Value

$host_ = "registry.terraform.io"
$namespace = "jamesloftis"
$type_ = "dungeondraft"
$osArch = "windows_amd64"

$mirrorDir = Join-Path $env:APPDATA "terraform.d\plugins\$host_\$namespace\$type_\$version\$osArch"
New-Item -ItemType Directory -Force -Path $mirrorDir | Out-Null

$outFile = Join-Path $mirrorDir "terraform-provider-$type_`_v$version.exe"
Write-Host "Building terraform-provider-$type_ v$version ..."
Push-Location $repoRoot
try {
    & go build -o $outFile .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

Write-Host "Installed to $outFile"
Write-Host ""
Write-Host "Any .tf using:"
Write-Host "  required_providers { dungeondraft = { source = `"$host_/$namespace/$type_`" } }"
Write-Host "now finds this build via terraform init with no CLI config changes."
Write-Host "Bumped the version const? Run this again, then 'terraform init -upgrade'"
Write-Host "in each working directory that uses this provider."
