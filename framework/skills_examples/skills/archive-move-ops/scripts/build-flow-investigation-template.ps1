[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$FlowId,

    [Parameter(Mandatory = $true)]
    [string]$UnionDatePrefix,

    [int]$Context = 2
)

$entryScriptPath = Join-Path $PSScriptRoot 'build-entry-commands.ps1'

if ($FlowId -notmatch '^(?<area>\d+)_') {
    throw "Unable to infer area from flow_id: $FlowId"
}

$inferredArea = $Matches['area']

try {
    $entryOutput = & $entryScriptPath `
        -FlowId $FlowId `
        -UnionDatePrefix $UnionDatePrefix `
        -Context $Context
}
catch {
    throw "Failed to build entry commands. $($_.Exception.Message)"
}

@(
    'FlowId: {0}' -f $FlowId
    'inferred area: {0}' -f $inferredArea
    ''
    'Union dispatch search:'
    '[TODO] fill src_area_type from union dispatch hit'
    '[TODO] fill dst_area_type from union dispatch hit'
    '[TODO] paste the matched Worker.startSyncDispatch() line here'
    ''
    'Archiver manager search:'
    '[TODO] summarize source area findings'
    '[TODO] summarize destination area findings'
    '[TODO] paste the source-side manager hit, then run build-uid-error-commands.ps1 -Side Source -LogLine ...'
    '[TODO] paste the destination-side manager hit, then run build-uid-error-commands.ps1 -Side Destination -LogLine ...'
    ''
    'UID error search:'
    '[TODO] fill src_uid from source-side manager log'
    '[TODO] fill dst_uid or dest_uid from destination-side manager log'
    '[TODO] summarize UID-related ERROR lines'
    '[TODO] if StorageWorker.Export() appears, run build-storage-worker-commands.ps1 with that log line and the current area'
    ''
    'Storage-worker search:'
    '[TODO] fill area used for storage-worker lookup'
    '[TODO] fill dscid from StorageWorker.Export()'
    '[TODO] fill uid and gid from StorageWorker.Export()'
    '[TODO] summarize storage-worker findings'
    ''
    'Commands:'
    $entryOutput
    ''
    'Conclusion:'
    '[TODO] summarize current status'
    '[TODO] propose next step'
) -join [Environment]::NewLine
