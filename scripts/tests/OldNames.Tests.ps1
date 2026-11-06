$repoRoot = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path))

# The project is MapForge. The old name stays only where something else depends
# on it (de-fork design, "the rename rule"), or in history.
$allowedPaths = @(
    '^CHANGELOG\.md:',
    '^docs/superpowers/',
    '^scripts/tests/OldNames\.Tests\.ps1:'  # this test's own search pattern
)
$allowedText = @(
    'dungeondraft-mcp-bridge-agent',  # the mod folder Dungeondraft knows the mod by
    'brann-dev/dungeondraft-mcp',     # the credit to Brandon Florian's project
    'Name: "dungeondraft-mcp"'        # mcpserver's MCP serverInfo name
)

function Get-OldNameHits {
    $hits = & git -C $repoRoot grep -n -i -E "dungeondraft-mcp|dungeondraft_mcp"
    foreach ($hit in $hits) {
        $isAllowedPath = @($allowedPaths | Where-Object { $hit -match $_ }).Count -gt 0
        $rest = $hit
        foreach ($text in $allowedText) { $rest = $rest.Replace($text, "") }
        $stillNamesOldProject = $rest -match "(?i)dungeondraft[-_]mcp"
        if (-not $isAllowedPath -and $stillNamesOldProject) { $hit }
    }
}

Describe "The project name" {
    It "is MapForge everywhere except the kept identifiers" {
        $offending = @(Get-OldNameHits)
        ($offending -join "`n") | Should BeNullOrEmpty
    }
}
