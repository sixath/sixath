[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$LogLine,

    [string]$Area,

    [string[]]$StorageHosts,

    [string]$LogDirectory,

    [int]$Context = 2
)

$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User
$serviceInfo = $environment.Services['storage-worker']

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
        [string]$Area,
        [Parameter(Mandatory = $true)]
        [string]$Dscid
    )

    if ($ExplicitHosts -and $ExplicitHosts.Count -gt 0) {
        return [pscustomobject]@{
            Source = 'explicit -StorageHosts'
            Hosts  = $ExplicitHosts
        }
    }

    if (-not $Area) {
        throw "Area is required to resolve storage-worker hosts for dscid $Dscid. Pass -Area or use -StorageHosts."
    }

    $hostsByArea = $serviceInfo.HostsByAreaDscid["$Area"]
    if (-not $hostsByArea) {
        throw "No storage-worker dscid mapping configured for area $Area. Add Services['storage-worker'].HostsByAreaDscid['$Area'] or pass -StorageHosts."
    }

    $configuredHosts = $hostsByArea["$Dscid"]
    if ($configuredHosts -and $configuredHosts.Count -gt 0) {
        return [pscustomobject]@{
            Source = 'environment.psd1 area+dscid mapping'
            Hosts  = $configuredHosts
        }
    }

    throw "No storage-worker hosts configured for area $Area dscid $Dscid. Add Services['storage-worker'].HostsByAreaDscid['$Area']['$Dscid'] or pass -StorageHosts."
}

function New-StorageWorkerSearchCommand {
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
        [string]$Gid,

        [Parameter(Mandatory = $true)]
        [int]$SearchContext
    )

    $escapedUid = Escape-SingleQuotedShellValue -Value $Uid
    $escapedGid = Escape-SingleQuotedShellValue -Value $Gid
    $currentLog = '{0}{1}' -f $Directory, $CurrentPattern
    $historyLogs = '{0}{1}' -f $Directory, $HistoryPattern
    $sshTarget = if ($sshUser) { '{0}@{1}' -f $sshUser, $MachineHost } else { $MachineHost }

    return @(
        ('# host: {0}' -f $MachineHost),
        ('ssh {0} "grep -nH -C {1} -- ''{2}'' {3} 2>/dev/null | grep -- ''{4}''"' -f $sshTarget, $SearchContext, $escapedUid, $currentLog, $escapedGid),
        ('ssh {0} "zgrep -nH -C {1} -- ''{2}'' {3} 2>/dev/null | grep -- ''{4}''"' -f $sshTarget, $SearchContext, $escapedUid, $historyLogs, $escapedGid)
    )
}

$uid = Get-FieldValue -Line $LogLine -FieldName 'uid'
$gid = Get-FieldValue -Line $LogLine -FieldName 'gid'
$dscid = Get-FieldValue -Line $LogLine -FieldName 'dscid'

if (-not $uid) {
    throw 'Unable to extract uid from the StorageWorker.Export() log line.'
}

if (-not $gid) {
    throw 'Unable to extract gid from the StorageWorker.Export() log line.'
}

if (-not $dscid) {
    throw 'Unable to extract dscid from the StorageWorker.Export() log line.'
}

$resolvedHosts = Resolve-Hosts -ExplicitHosts $StorageHosts -Area $Area -Dscid $dscid
$directories = if ($LogDirectory) { @($LogDirectory) } else { $serviceInfo.LogDirectories }

if (-not $directories -or $directories.Count -eq 0) {
    throw 'No storage-worker log directory configured. Add Services["storage-worker"].LogDirectories or pass -LogDirectory.'
}

$output = New-Object System.Collections.Generic.List[string]
[void]$output.Add('# uid: {0}' -f $uid)
[void]$output.Add('# gid: {0}' -f $gid)
[void]$output.Add('# dscid: {0}' -f $dscid)
if ($Area) {
    [void]$output.Add('# area: {0}' -f $Area)
}
[void]$output.Add('# hosts source: {0}' -f $resolvedHosts.Source)
[void]$output.Add('# next: search storage-worker logs by uid and gid')
[void]$output.Add('')

foreach ($machineHost in $resolvedHosts.Hosts) {
    foreach ($directory in $directories) {
        foreach ($line in (New-StorageWorkerSearchCommand -MachineHost $machineHost -Directory $directory -CurrentPattern $serviceInfo.CurrentLogPattern -HistoryPattern $serviceInfo.HistoryLogPattern -Uid $uid -Gid $gid -SearchContext $Context)) {
            [void]$output.Add($line)
        }
        [void]$output.Add('')
    }
}

$output -join [Environment]::NewLine
