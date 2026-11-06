$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "..\ModJunction.ps1")

function New-TestRoot {
    $root = Join-Path ([System.IO.Path]::GetTempPath()) ("modjunction-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path (Join-Path $root "source") -Force | Out-Null
    return $root
}

function Get-JunctionTarget([string]$path) {
    return @((Get-Item $path).Target)[0]
}

Describe "Set-ModJunction" {
    It "creates the Mods folder and the junction when neither exists" {
        $root = New-TestRoot
        $mods = Join-Path $root "Mods"
        $source = Join-Path $root "source"

        Set-ModJunction -ModsDir $mods -Source $source

        $link = Join-Path $mods "dungeondraft-mcp-bridge-agent"
        (Get-Item $link).LinkType | Should Be "Junction"
        Get-JunctionTarget $link | Should Be $source
    }

    It "leaves a junction that already points at the source alone" {
        $root = New-TestRoot
        $mods = Join-Path $root "Mods"
        $source = Join-Path $root "source"
        Set-ModJunction -ModsDir $mods -Source $source

        { Set-ModJunction -ModsDir $mods -Source $source } | Should Not Throw

        Get-JunctionTarget (Join-Path $mods "dungeondraft-mcp-bridge-agent") | Should Be $source
    }

    It "replaces a junction that points somewhere else" {
        $root = New-TestRoot
        $mods = Join-Path $root "Mods"
        $old = Join-Path $root "old"
        New-Item -ItemType Directory -Path $old | Out-Null
        Set-Content -Path (Join-Path $old "keep.txt") -Value "old checkout"
        Set-ModJunction -ModsDir $mods -Source $old
        $source = Join-Path $root "source"

        Set-ModJunction -ModsDir $mods -Source $source

        Get-JunctionTarget (Join-Path $mods "dungeondraft-mcp-bridge-agent") | Should Be $source
        Test-Path (Join-Path $old "keep.txt") | Should Be $true
    }

    It "refuses a real folder in the junction's place and leaves it intact" {
        $root = New-TestRoot
        $mods = Join-Path $root "Mods"
        $real = Join-Path $mods "dungeondraft-mcp-bridge-agent"
        New-Item -ItemType Directory -Path $real -Force | Out-Null
        Set-Content -Path (Join-Path $real "mine.txt") -Value "a real mod copy"
        $source = Join-Path $root "source"

        { Set-ModJunction -ModsDir $mods -Source $source } | Should Throw

        Test-Path (Join-Path $real "mine.txt") | Should Be $true
        (Get-Item $real).LinkType | Should BeNullOrEmpty
    }
}
