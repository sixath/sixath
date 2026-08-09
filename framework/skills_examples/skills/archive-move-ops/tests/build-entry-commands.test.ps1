[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-entry-commands.ps1'

$traceOutput = & $scriptPath -TraceId 'trace-demo-123'
if (-not $traceOutput) {
    throw 'Expected traceId mode to produce output.'
}

$traceRequired = @(
    '# mode: traceId',
    '# traceId: trace-demo-123',
    '# next: run these union-archiver-dispatch commands first',
    "ssh vrviu@10.19.240.104 ""grep -nH -C 2 -- 'trace-demo-123' /data/union/logs/union_archiver_dispatch/union-archiver-dispatch.log 2>/dev/null"""
)

foreach ($snippet in $traceRequired) {
    if ($traceOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected trace output to contain: $snippet"
    }
}

$traceDateOutput = & $scriptPath -TraceId 'trace-demo-123' -UnionDatePrefix '2026-04-20'
if (-not $traceDateOutput) {
    throw 'Expected traceId+date mode to produce output.'
}

$traceDateRequired = @(
    '# union date batch: 2026-04-20',
    '# next: search date-scoped compressed logs and filter Worker.startSyncDispatch() first',
    "ssh vrviu@10.19.240.104 ""zcat /data/union/logs/union_archiver_dispatch/union-archiver-dispatch-2026-04-20*.log.gz 2>/dev/null | grep -- 'trace-demo-123' | grep 'Worker.startSyncDispatch()'"""
)

foreach ($snippet in $traceDateRequired) {
    if ($traceDateOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected trace date output to contain: $snippet"
    }
}

$flowOutput = & $scriptPath -FlowId '4_107g41jgxu3s'
if (-not $flowOutput) {
    throw 'Expected flowId mode to produce output.'
}

$flowRequired = @(
    '# mode: flowId',
    '# flow_id: 4_107g41jgxu3s',
    '# inferred area: 4',
    '# note: flow_id mode can only infer the prefixed area; use dispatch logs to confirm the full src -> dst route',
    "ssh vrviu@10.18.240.104 ""grep -nH -C 2 -- '4_107g41jgxu3s' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null"""
)

foreach ($snippet in $flowRequired) {
    if ($flowOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected flow output to contain: $snippet"
    }
}

$flowDateOutput = & $scriptPath -FlowId '4_107g41jgxu3s' -UnionDatePrefix '2026-04-20'
if (-not $flowDateOutput) {
    throw 'Expected flowId+date mode to produce output.'
}

$flowDateRequired = @(
    '# mode: flowId',
    '# flow_id: 4_107g41jgxu3s',
    '# union date batch: 2026-04-20',
    '# next: search date-scoped compressed union logs and filter Worker.startSyncDispatch() first',
    "ssh vrviu@10.19.240.104 ""zcat /data/union/logs/union_archiver_dispatch/union-archiver-dispatch-2026-04-20*.log.gz 2>/dev/null | grep -- '4_107g41jgxu3s' | grep 'Worker.startSyncDispatch()'"""
)

foreach ($snippet in $flowDateRequired) {
    if ($flowDateOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected flow date output to contain: $snippet"
    }
}

Write-Host 'PASS build-entry-commands.test.ps1'
