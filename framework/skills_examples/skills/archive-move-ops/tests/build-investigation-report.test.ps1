[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-investigation-report.ps1'
$fixturePath = Join-Path $PSScriptRoot 'fixtures\dispatch-log-sample.txt'

$output = & $scriptPath -TraceId 'trace-demo-123' -DispatchLogFile $fixturePath

if (-not $output) {
    throw 'Expected script to produce output.'
}

$requiredSnippets = @(
    'TraceId: trace-demo-123',
    'Dispatch hit:',
    'flow_id: 301_rqkkw0snhnmt',
    'route: 400 -> 301',
    'matched log:',
    'Source area manager:',
    'area: 400',
    'search policy: use src_uid only',
    'Destination area manager:',
    'area: 301',
    'Conclusion:',
    '[TODO] summarize current status',
    "ssh vrviu@10.11.240.104 ""grep -nH -C 2 -- '301_rqkkw0snhnmt' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null"""
)

foreach ($snippet in $requiredSnippets) {
    if ($output -notmatch [regex]::Escape($snippet)) {
        throw "Expected output to contain: $snippet"
    }
}

if ($output -match 'ssh vrviu@10\.77\.240\.104.*301_rqkkw0snhnmt') {
    throw 'Source-area report must not grep flow_id on source hosts.'
}

$outputSrc = & $scriptPath -TraceId 'trace-demo-123' -DispatchLogFile $fixturePath -SrcUid '154880308'
if ($outputSrc -notmatch [regex]::Escape("ssh vrviu@10.77.240.104 ""grep -nH -C 2 -- '154880308' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null""")) {
    throw 'Expected -SrcUid to emit source-area grep commands for the uid.'
}

Write-Host 'PASS build-investigation-report.test.ps1'
