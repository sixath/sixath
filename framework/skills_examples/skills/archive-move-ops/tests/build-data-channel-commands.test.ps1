[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-data-channel-commands.ps1'
$managerLine = '{"L":"INFO","T":"2026-04-26T11:53:28.685+0800","M":"archive task created. flow_id(4_107g41jgxu3s) task_id(archive-task-123) uid(9664537)","LAPP":"archiver-manager"}'

$output = & $scriptPath -LogLine $managerLine -Area 4
if (-not $output) {
    throw 'Expected data-channel output.'
}

$required = @(
    '# task_id: archive-task-123',
    '# uid: 9664537',
    '# area: 4',
    '# host: 10.18.240.64',
    "ssh vrviu@10.18.240.64 ""grep -nH -C 2 -- 'archive-task-123' /opt/deploy_agent/log/deploy_agent*.log* 2>/dev/null""",
    "ssh vrviu@10.18.240.64 ""grep -nH -C 2 -- 'archive-task-123' /opt/deploy_server/log/deploy_server*.log* 2>/dev/null""",
    "ssh vrviu@10.18.240.64 ""grep -nH -C 2 -- '9664537' /data/storage_worker/logs/storage-worker*.log* 2>/dev/null"""
)

foreach ($snippet in $required) {
    if ($output -notmatch [regex]::Escape($snippet)) {
        throw "Expected output to contain: $snippet"
    }
}

$missingAreaFailed = $false
try {
    & $scriptPath -LogLine $managerLine | Out-Null
}
catch {
    $missingAreaFailed = $_.Exception.Message -match 'Unable to resolve data-channel area'
}

if (-not $missingAreaFailed) {
    throw 'Expected missing area to produce a clear error.'
}

Write-Host 'PASS build-data-channel-commands.test.ps1'
