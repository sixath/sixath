[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$LogLine,

    [switch]$AsJson
)

function Get-FieldValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Line,

        [Parameter(Mandatory = $true)]
        [string]$FieldName
    )

    $pattern = [regex]::Escape($FieldName) + '\((?<value>[^)]*)\)'
    $match = [regex]::Match($Line, $pattern)
    if ($match.Success) {
        return $match.Groups['value'].Value
    }

    return $null
}

$result = [pscustomobject]@{
    FlowId           = Get-FieldValue -Line $LogLine -FieldName 'flow_id'
    Uuid             = Get-FieldValue -Line $LogLine -FieldName 'uuid'
    Ugid             = Get-FieldValue -Line $LogLine -FieldName 'ugid'
    SourceArea       = Get-FieldValue -Line $LogLine -FieldName 'src_area_type'
    DestinationArea  = Get-FieldValue -Line $LogLine -FieldName 'dst_area_type'
    DoneUnionVersion = Get-FieldValue -Line $LogLine -FieldName 'done_union_version'
}

if (-not $result.FlowId -or -not $result.SourceArea -or -not $result.DestinationArea) {
    throw "Unable to parse flow_id/src_area_type/dst_area_type from the provided log line."
}

$output = [pscustomobject]@{
    FlowId           = $result.FlowId
    SourceArea       = $result.SourceArea
    DestinationArea  = $result.DestinationArea
    Route            = '{0} -> {1}' -f $result.SourceArea, $result.DestinationArea
    Uuid             = $result.Uuid
    Ugid             = $result.Ugid
    DoneUnionVersion = $result.DoneUnionVersion
}

if ($AsJson) {
    $output | ConvertTo-Json -Depth 3
    exit 0
}

$output
