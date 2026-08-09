[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$LogLine,

    [string]$Area,

    [string[]]$DataChannelHosts,

    [int]$Context = 2
)

$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User
$serviceInfo = $environment.Services['data-channel']

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

function Get-FirstFieldValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Line,

        [Parameter(Mandatory = $true)]
        [string[]]$FieldNames
    )

    foreach ($fieldName in $FieldNames) {
        $value = Get-FieldValue -Line $Line -FieldName $fieldName
        if ($value) {
            return [pscustomobject]@{
                Field = $fieldName
                Value = $value
            }
        }
    }

    return [pscustomobject]@{
        Field = $FieldNames[0]
        Value = $null
    }
}

function Escape-SingleQuotedShellValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value
    )

    return $Value -replace "'", "'""'""'"
}

function Resolve-Hosts {
    param(
        [string[]]$ExplicitHosts,
        [string]$Area
    )

    if ($ExplicitHosts -and $ExplicitHosts.Count -gt 0) {
        return [pscustomobject]@{
            Source = 'explicit -DataChannelHosts'
            Hosts = $ExplicitHosts
        }
    }

    if (-not $Area) {
        throw 'Unable to resolve data-channel area. Pass -Area for the manager side you are investigating, or pass -DataChannelHosts.'
    }

    $configuredHosts = $serviceInfo.HostsByArea["$Area"]
    if ($configuredHosts -and $configuredHosts.Count -gt 0) {
        return [pscustomobject]@{
            Source = 'environment.psd1 area mapping'
            Hosts = $configuredHosts
        }
    }

    throw "No data-channel hosts configured for area $Area. Add Services['data-channel'].HostsByArea['$Area'] or pass -DataChannelHosts."
}

function New-GrepCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$MachineHost,

        [Parameter(Mandatory = $true)]
        [string]$SearchTerm,

        [Parameter(Mandatory = $true)]
        [string]$PathPattern,

        [Parameter(Mandatory = $true)]
        [int]$SearchContext
    )

    $escapedTerm = Escape-SingleQuotedShellValue -Value $SearchTerm
    $sshTarget = if ($sshUser) { '{0}@{1}' -f $sshUser, $MachineHost } else { $MachineHost }
    return 'ssh {0} "grep -nH -C {1} -- ''{2}'' {3} 2>/dev/null"' -f $sshTarget, $SearchContext, $escapedTerm, $PathPattern
}

$taskInfo = Get-FirstFieldValue -Line $LogLine -FieldNames @('task_id', 'task-id', 'taskId', 'taskid')
$uidInfo = Get-FirstFieldValue -Line $LogLine -FieldNames @('uid', 'src_uid', 'dst_uid', 'dest_uid')

if (-not $taskInfo.Value) {
    throw 'Unable to extract task_id from the archiver-manager log line.'
}

if (-not $uidInfo.Value) {
    throw 'Unable to extract uid from the archiver-manager log line.'
}

$resolvedHosts = Resolve-Hosts -ExplicitHosts $DataChannelHosts -Area $Area
$serverId = if ($Area -and $serviceInfo.ServerIdByArea) { $serviceInfo.ServerIdByArea["$Area"] } else { $null }

$output = New-Object System.Collections.Generic.List[string]
[void]$output.Add('# task_id field: {0}' -f $taskInfo.Field)
[void]$output.Add('# task_id: {0}' -f $taskInfo.Value)
[void]$output.Add('# uid field: {0}' -f $uidInfo.Field)
[void]$output.Add('# uid: {0}' -f $uidInfo.Value)
if ($Area) {
    [void]$output.Add('# area: {0}' -f $Area)
}
if ($serverId) {
    [void]$output.Add('# data-channel server id: {0}' -f $serverId)
}
[void]$output.Add('# hosts source: {0}' -f $resolvedHosts.Source)
[void]$output.Add('# next: search data-channel deploy logs by task_id and storage-worker logs by uid')
[void]$output.Add('')

foreach ($machineHost in $resolvedHosts.Hosts) {
    [void]$output.Add('# host: {0}' -f $machineHost)

    foreach ($target in $serviceInfo.DeployLogTargets) {
        $pathPattern = '{0}{1}' -f $target.Directory, $target.Pattern
        [void]$output.Add('# {0}: task_id search' -f $target.Name)
        [void]$output.Add((New-GrepCommand -MachineHost $machineHost -SearchTerm $taskInfo.Value -PathPattern $pathPattern -SearchContext $Context))
    }

    foreach ($directory in $serviceInfo.StorageWorker.LogDirectories) {
        $pathPattern = '{0}{1}' -f $directory, $serviceInfo.StorageWorker.Pattern
        [void]$output.Add('# storage-worker: uid search')
        [void]$output.Add((New-GrepCommand -MachineHost $machineHost -SearchTerm $uidInfo.Value -PathPattern $pathPattern -SearchContext $Context))
    }

    [void]$output.Add('')
}

$output -join [Environment]::NewLine
