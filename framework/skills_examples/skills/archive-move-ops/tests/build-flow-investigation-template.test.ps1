[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-flow-investigation-template.ps1'

$output = & $scriptPath -FlowId '4_107g41jgxu3s' -UnionDatePrefix '2026-04-20'

if (-not $output) {
    throw 'Expected script to produce output.'
}

$requiredSnippets = @(
    'FlowId: 4_107g41jgxu3s',
    'Union dispatch search:',
    'Archiver manager search:',
    'inferred area: 4',
    '[TODO] fill src_area_type from union dispatch hit',
    '[TODO] fill dst_area_type from union dispatch hit',
    '[TODO] summarize source area findings',
    '[TODO] summarize destination area findings',
    '[TODO] summarize current status',
    "ssh vrviu@10.19.240.104 ""zcat /data/union/logs/union_archiver_dispatch/union-archiver-dispatch-2026-04-20*.log.gz 2>/dev/null | grep -- '4_107g41jgxu3s' | grep 'Worker.startSyncDispatch()'""",
    "ssh vrviu@10.18.240.104 ""grep -nH -C 2 -- '4_107g41jgxu3s' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null"""
)

foreach ($snippet in $requiredSnippets) {
    if ($output -notmatch [regex]::Escape($snippet)) {
        throw "Expected output to contain: $snippet"
    }
}

Write-Host 'PASS build-flow-investigation-template.test.ps1'
