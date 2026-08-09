[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-uid-error-commands.ps1'
$managerLine = '2026-04-20 INFO recieve request. url(http://nj-4-archiver-manager-zj.migufun.com/v1/group/dispatch) param({"uuid":112680062,"ugid":1124,"done_union_version":164,"src_area_type":301,"src_uid":6364047,"src_gid":1124,"src_done_area_version":140,"dest_uid":8980218,"dest_gid":1124,"flow_id":"4_107g41jgxu3s","mode":1})'

$sourceOutput = & $scriptPath -LogLine $managerLine -Side Source
if (-not $sourceOutput) {
    throw 'Expected source side output.'
}

$sourceRequired = @(
    '# side: Source',
    '# uid field: src_uid',
    '# uid: 6364047',
    '# area: 301',
    "ssh vrviu@10.11.240.104 ""grep -nH -C 2 -- '6364047' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null | grep -i -C 2 -- 'ERROR'"""
)

foreach ($snippet in $sourceRequired) {
    if ($sourceOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected source output to contain: $snippet"
    }
}

$destinationOutput = & $scriptPath -LogLine $managerLine -Side Destination
if (-not $destinationOutput) {
    throw 'Expected destination side output.'
}

$destinationRequired = @(
    '# side: Destination',
    '# uid field: dest_uid',
    '# uid: 8980218',
    '# area: 4',
    "ssh vrviu@10.18.240.104 ""grep -nH -C 2 -- '8980218' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null | grep -i -C 2 -- 'ERROR'"""
)

foreach ($snippet in $destinationRequired) {
    if ($destinationOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected destination output to contain: $snippet"
    }
}

$escapedManagerLine = '2026-04-20 INFO recieve request. param({\"src_area_type\":301,\"src_uid\":6364047,\"dest_uid\":8980218,\"flow_id\":\"4_107g41jgxu3s\"})'
$escapedOutput = & $scriptPath -LogLine $escapedManagerLine -Side Destination
if ($escapedOutput -notmatch [regex]::Escape('# uid: 8980218')) {
    throw 'Expected escaped JSON-ish log line to extract destination uid.'
}

$shellEscapedManagerLine = '2026-04-20 INFO recieve request. param({\src_area_type\:301,\src_uid\:6364047,\dest_uid\:8980218,\flow_id\:\4_107g41jgxu3s\})'
$shellEscapedOutput = & $scriptPath -LogLine $shellEscapedManagerLine -Side Destination
if ($shellEscapedOutput -notmatch [regex]::Escape('# uid: 8980218')) {
    throw 'Expected shell-escaped log line to extract destination uid.'
}

Write-Host 'PASS build-uid-error-commands.test.ps1'
