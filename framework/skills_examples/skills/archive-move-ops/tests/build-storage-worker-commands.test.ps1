[CmdletBinding()]
param()

$scriptPath = Join-Path $PSScriptRoot '..\scripts\build-storage-worker-commands.ps1'
$storageLine = '{"L":"DEBUG","T":"2026-04-26T11:53:28.685+0800","M":"StorageWorker.Export(). uid(9664537) gid(1506) rpid(2000) dscid(1) dir(/data/volume_resource_point/2048249069968441344) errcode(0) err(<nil>)","LAPP":"archiver-manager","LPID":1,"LFILE":"gopublic/utils.go","LLINE":275,"LIP":""}'

$manualOutput = & $scriptPath `
    -LogLine $storageLine `
    -StorageHosts '10.1.2.3' `
    -LogDirectory '/data/storage_worker/logs/'

if (-not $manualOutput) {
    throw 'Expected manual storage-worker output.'
}

$manualRequired = @(
    '# uid: 9664537',
    '# gid: 1506',
    '# dscid: 1',
    '# hosts source: explicit -StorageHosts',
    "ssh vrviu@10.1.2.3 ""grep -nH -C 2 -- '9664537' /data/storage_worker/logs/storage-worker.log 2>/dev/null | grep -- '1506'"""
)

foreach ($snippet in $manualRequired) {
    if ($manualOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected manual output to contain: $snippet"
    }
}

$configOutput = & $scriptPath -LogLine $storageLine -Area 4
if (-not $configOutput) {
    throw 'Expected configured area+dscid output.'
}

$configRequired = @(
    '# dscid: 1',
    '# area: 4',
    '# hosts source: environment.psd1 area+dscid mapping',
    "ssh vrviu@10.18.101.240 ""grep -nH -C 2 -- '9664537' /data/storage_worker/logs/storage-worker.log 2>/dev/null | grep -- '1506'"""
)

foreach ($snippet in $configRequired) {
    if ($configOutput -notmatch [regex]::Escape($snippet)) {
        throw "Expected config output to contain: $snippet"
    }
}

$missingAreaFailed = $false
try {
    & $scriptPath -LogLine $storageLine | Out-Null
}
catch {
    $missingAreaFailed = $_.Exception.Message -match 'Area is required to resolve storage-worker hosts for dscid 1'
}

if (-not $missingAreaFailed) {
    throw 'Expected missing area to produce a clear error.'
}

Write-Host 'PASS build-storage-worker-commands.test.ps1'
