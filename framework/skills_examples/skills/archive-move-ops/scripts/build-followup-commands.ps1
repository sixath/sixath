[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$LogLine,

    [string]$SrcUid,

    [int]$Context = 2
)

$parseScriptPath = Join-Path $PSScriptRoot 'parse-dispatch-log.ps1'
$searchScriptPath = Join-Path $PSScriptRoot 'build-search-commands.ps1'

try {
    $parsed = & $parseScriptPath -LogLine $LogLine
}
catch {
    throw "Failed to parse dispatch log line. $($_.Exception.Message)"
}

try {
    $destinationCommands = & $searchScriptPath `
        -Service 'archiver-manager' `
        -Keyword $parsed.FlowId `
        -Areas @($parsed.DestinationArea) `
        -Context $Context
}
catch {
    throw "Failed to build destination archiver-manager commands. $($_.Exception.Message)"
}

if ($SrcUid) {
    try {
        $sourceCommands = & $searchScriptPath `
            -Service 'archiver-manager' `
            -Keyword $SrcUid `
            -Areas @($parsed.SourceArea) `
            -Context $Context
    }
    catch {
        throw "Failed to build source archiver-manager commands. $($_.Exception.Message)"
    }
}
else {
    $sourceCommands = @(
        '# 源区域 archiver-manager：检索词必须是 src_uid（建议对同一数值尝试裸数字与 src_uid=<id> 两种 grep），禁止使用 flow_id。'
        '# 未传入 -SrcUid：请先在下方「目标区域」用 flow_id 命中 archiver-manager，从 param JSON 提取 src_uid，再带 -SrcUid 重新运行本脚本。'
        ('# 手动示例: .\scripts\build-search-commands.ps1 -Service archiver-manager -Keyword ''<src_uid>'' -Areas {0}' -f $parsed.SourceArea)
    ) -join [Environment]::NewLine
}

@(
    '# flow_id: {0}' -f $parsed.FlowId
    '# route: {0} -> {1}' -f $parsed.SourceArea, $parsed.DestinationArea
    '# next: destination area uses flow_id; source area uses src_uid only (see below)'
    ''
    ('# --- source archiver-manager / area {0} ---' -f $parsed.SourceArea)
    $sourceCommands
    ''
    ('# --- destination archiver-manager / area {0} ---' -f $parsed.DestinationArea)
    $destinationCommands
) -join [Environment]::NewLine
