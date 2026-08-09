[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-followup-commands.ps1'
$sampleLine = 'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)'

$outputNoSrc = & $scriptPath -LogLine $sampleLine

if (-not $outputNoSrc) {
    throw "Expected script to produce output."
}

$requiredWhenNoSrc = @(
    '# route: 400 -> 301',
    '# flow_id: 301_rqkkw0snhnmt',
    '禁止使用 flow_id',
    '# --- destination archiver-manager / area 301 ---',
    "ssh vrviu@10.11.240.104 ""grep -nH -C 2 -- '301_rqkkw0snhnmt' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null"""
)

foreach ($snippet in $requiredWhenNoSrc) {
    if ($outputNoSrc -notmatch [regex]::Escape($snippet)) {
        throw "Expected output to contain: $snippet"
    }
}

if ($outputNoSrc -match 'ssh vrviu@10\.77\.240\.104.*301_rqkkw0snhnmt') {
    throw 'Source-area hosts must not use flow_id as the grep keyword.'
}

$outputWithSrc = & $scriptPath -LogLine $sampleLine -SrcUid '154880308'

$requiredWithSrc = @(
    '# --- source archiver-manager / area 400 ---',
    "ssh vrviu@10.77.240.104 ""grep -nH -C 2 -- '154880308' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null""",
    "ssh vrviu@10.11.240.104 ""grep -nH -C 2 -- '301_rqkkw0snhnmt' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null"""
)

foreach ($snippet in $requiredWithSrc) {
    if ($outputWithSrc -notmatch [regex]::Escape($snippet)) {
        throw "Expected output (with -SrcUid) to contain: $snippet"
    }
}

Write-Host 'PASS build-followup-commands.test.ps1'
