[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$LogLine,

    [Parameter(Mandatory = $true)]
    [ValidateSet('Source', 'Destination')]
    [string]$Side,

    [string]$Area,

    [int]$Context = 2
)

$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User
$serviceInfo = $environment.Services['archiver-manager']

function Get-FieldValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Line,

        [Parameter(Mandatory = $true)]
        [string]$FieldName
    )

    $normalizedLine = $Line -replace '\\\"', '"'
    $patterns = @(
        ('"{0}"\s*:\s*"?(?<value>[^",}}\s]+)"?' -f [regex]::Escape($FieldName)),
        ('\\?{0}\\?\s*:\s*\\?"?(?<value>[^",}}\s\\]+)\\?"?' -f [regex]::Escape($FieldName)),
        ('{0}\((?<value>[^)]*)\)' -f [regex]::Escape($FieldName)),
        ('{0}=(?<value>[^&\s\\n]+)' -f [regex]::Escape($FieldName))
    )

    foreach ($pattern in $patterns) {
        $match = [regex]::Match($normalizedLine, $pattern)
        if ($match.Success) {
            return $match.Groups['value'].Value
        }
    }

    return $null
}

function Resolve-Uid {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Line,

        [Parameter(Mandatory = $true)]
        [string]$LogSide
    )

    if ($LogSide -eq 'Source') {
        return [pscustomobject]@{
            Field = 'src_uid'
            Value = Get-FieldValue -Line $Line -FieldName 'src_uid'
        }
    }

    foreach ($field in @('dst_uid', 'dest_uid')) {
        $value = Get-FieldValue -Line $Line -FieldName $field
        if ($value) {
            return [pscustomobject]@{
                Field = $field
                Value = $value
            }
        }
    }

    return [pscustomobject]@{
        Field = 'dst_uid'
        Value = $null
    }
}

function Resolve-Area {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Line,

        [Parameter(Mandatory = $true)]
        [string]$LogSide,

        [string]$ExplicitArea
    )

    if ($ExplicitArea) {
        return $ExplicitArea
    }

    if ($LogSide -eq 'Source') {
        $sourceArea = Get-FieldValue -Line $Line -FieldName 'src_area_type'
        if ($sourceArea) {
            return $sourceArea
        }
    }

    $flowId = Get-FieldValue -Line $Line -FieldName 'flow_id'
    if ($flowId -and $flowId -match '^(?<area>\d+)_') {
        return $Matches['area']
    }

    return $null
}

function Escape-SingleQuotedShellValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value
    )

    return $Value -replace "'", "'""'""'"
}

function New-ErrorSearchCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$MachineHost,

        [Parameter(Mandatory = $true)]
        [string]$Directory,

        [Parameter(Mandatory = $true)]
        [string]$CurrentPattern,

        [Parameter(Mandatory = $true)]
        [string]$HistoryPattern,

        [Parameter(Mandatory = $true)]
        [string]$Uid,

        [Parameter(Mandatory = $true)]
        [int]$SearchContext
    )

    $escapedUid = Escape-SingleQuotedShellValue -Value $Uid
    $currentLog = '{0}{1}' -f $Directory, $CurrentPattern
    $historyLogs = '{0}{1}' -f $Directory, $HistoryPattern
    $sshTarget = if ($sshUser) { '{0}@{1}' -f $sshUser, $MachineHost } else { $MachineHost }

    return @(
        ('# host: {0}' -f $MachineHost),
        ('ssh {0} "grep -nH -C {1} -- ''{2}'' {3} 2>/dev/null | grep -i -C {1} -- ''ERROR''"' -f $sshTarget, $SearchContext, $escapedUid, $currentLog),
        ('ssh {0} "zgrep -nH -C {1} -- ''{2}'' {3} 2>/dev/null | grep -i -C {1} -- ''ERROR''"' -f $sshTarget, $SearchContext, $escapedUid, $historyLogs)
    )
}

$uidInfo = Resolve-Uid -Line $LogLine -LogSide $Side
if (-not $uidInfo.Value) {
    throw "Unable to extract UID for side $Side from the provided archiver-manager log line."
}

$resolvedArea = Resolve-Area -Line $LogLine -LogSide $Side -ExplicitArea $Area
if (-not $resolvedArea) {
    throw "Unable to resolve area for side $Side. Pass -Area explicitly."
}

$hosts = $serviceInfo.HostsByArea["$resolvedArea"]
if (-not $hosts) {
    throw "No archiver-manager hosts configured for area $resolvedArea."
}

$output = New-Object System.Collections.Generic.List[string]
[void]$output.Add('# side: {0}' -f $Side)
[void]$output.Add('# uid field: {0}' -f $uidInfo.Field)
[void]$output.Add('# uid: {0}' -f $uidInfo.Value)
[void]$output.Add('# area: {0}' -f $resolvedArea)
[void]$output.Add('# next: search archiver-manager ERROR logs with this UID')
[void]$output.Add('')

foreach ($machineHost in $hosts) {
    foreach ($directory in $serviceInfo.LogDirectories) {
        foreach ($line in (New-ErrorSearchCommand -MachineHost $machineHost -Directory $directory -CurrentPattern $serviceInfo.CurrentLogPattern -HistoryPattern $serviceInfo.HistoryLogPattern -Uid $uidInfo.Value -SearchContext $Context)) {
            [void]$output.Add($line)
        }
        [void]$output.Add('')
    }
}

$output -join [Environment]::NewLine
