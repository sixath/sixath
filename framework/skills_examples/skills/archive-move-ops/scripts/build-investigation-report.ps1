[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$TraceId,

    [Parameter(Mandatory = $true)]
    [string]$DispatchLogFile,

    [string]$SrcUid,

    [int]$Context = 2
)

$parseScriptPath = Join-Path $PSScriptRoot 'parse-dispatch-log.ps1'
$searchScriptPath = Join-Path $PSScriptRoot 'build-search-commands.ps1'
$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath

if (-not (Test-Path -LiteralPath $DispatchLogFile)) {
    throw "Dispatch log file not found: $DispatchLogFile"
}

$matchedLine = Get-Content -LiteralPath $DispatchLogFile | Where-Object {
    $_ -like "*$TraceId*" -and $_ -like '*Worker.startSyncDispatch()*'
} | Select-Object -First 1

if (-not $matchedLine) {
    throw "No Worker.startSyncDispatch() line found for traceId: $TraceId"
}

try {
    $parsed = & $parseScriptPath -LogLine $matchedLine
}
catch {
    throw "Failed to parse matched dispatch line. $($_.Exception.Message)"
}

try {
    if ($SrcUid) {
        $sourceCommands = & $searchScriptPath `
            -Service 'archiver-manager' `
            -Keyword $SrcUid `
            -Areas @($parsed.SourceArea) `
            -Context $Context
    }
    else {
        $sourceCommands = @(
            '# 源区域 archiver-manager：必须使用 src_uid 作为检索词，禁止使用 flow_id。'
            '# 请先用下方「目标区域」命令以 flow_id 命中后，从 param JSON 读取 src_uid，再运行本脚本并传入 -SrcUid。'
            ('# 手动: .\scripts\build-search-commands.ps1 -Service archiver-manager -Keyword ''<src_uid>'' -Areas {0}' -f $parsed.SourceArea)
        ) -join [Environment]::NewLine
    }

    $destinationCommands = & $searchScriptPath `
        -Service 'archiver-manager' `
        -Keyword $parsed.FlowId `
        -Areas @($parsed.DestinationArea) `
        -Context $Context
}
catch {
    throw "Failed to build manager search commands. $($_.Exception.Message)"
}

$hostsByArea = $environment.Services['archiver-manager'].HostsByArea
$sourceHosts = ($hostsByArea["$($parsed.SourceArea)"] -join ', ')
$destinationHosts = ($hostsByArea["$($parsed.DestinationArea)"] -join ', ')

@(
    'TraceId: {0}' -f $TraceId
    ''
    'Dispatch hit:'
    'dispatch log file: {0}' -f $DispatchLogFile
    'matched log: {0}' -f $matchedLine
    'flow_id: {0}' -f $parsed.FlowId
    'route: {0} -> {1}' -f $parsed.SourceArea, $parsed.DestinationArea
    'uuid: {0}' -f $parsed.Uuid
    'ugid: {0}' -f $parsed.Ugid
    ''
    'Source area manager:'
    'area: {0}' -f $parsed.SourceArea
    'hosts searched: {0}' -f $sourceHosts
    'search policy: use src_uid only (not flow_id); dispatch uuid is not guaranteed to equal src_uid'
    'commands:'
    $sourceCommands
    'findings: [TODO] summarize source area findings'
    ''
    'Destination area manager:'
    'area: {0}' -f $parsed.DestinationArea
    'hosts searched: {0}' -f $destinationHosts
    'commands:'
    $destinationCommands
    'findings: [TODO] summarize destination area findings'
    ''
    'Conclusion:'
    '[TODO] summarize current status'
    '[TODO] propose next step'
) -join [Environment]::NewLine
