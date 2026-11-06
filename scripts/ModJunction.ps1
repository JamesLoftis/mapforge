$ModFolderName = "dungeondraft-mcp-bridge-agent"

# Links <ModsDir>\dungeondraft-mcp-bridge-agent to the mod in this checkout, so
# Dungeondraft loads the mod straight from the repo. A junction left pointing at
# another checkout is replaced; a real folder there is somebody's copy of the
# mod, so it is never deleted.
function Set-ModJunction {
    param(
        [Parameter(Mandatory = $true)][string]$ModsDir,
        [Parameter(Mandatory = $true)][string]$Source
    )
    New-Item -ItemType Directory -Force -Path $ModsDir | Out-Null
    $link = Join-Path $ModsDir $ModFolderName

    if (Test-Path -LiteralPath $link) {
        $existing = Get-Item -LiteralPath $link -Force
        if ($existing.LinkType -ne "Junction") {
            throw "$link is a real folder or file, not a junction; move it aside and re-run."
        }
        if (@($existing.Target)[0] -eq $Source) {
            return
        }
        # Deletes the link only; the folder it pointed at is untouched.
        [System.IO.Directory]::Delete($link)
    }
    New-Item -ItemType Junction -Path $link -Target $Source | Out-Null
}
