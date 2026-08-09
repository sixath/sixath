[CmdletBinding()]
 param(
     [Parameter(Mandatory = $true)]
     [ValidateSet('union-archiver-dispatch', 'archiver-manager')]
     [string]$Service,
 
     [Parameter(Mandatory = $true)]
     [string]$Keyword,
 
    [string[]]$Areas,

    [int]$Context = 2
)

$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User

function Escape-SingleQuotedShellValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value
    )

    return $Value -replace "'", "'""'""'"
}

function New-SearchCommand {
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
        [string]$SearchTerm,

        [Parameter(Mandatory = $true)]
        [int]$SearchContext
     )

    $escaped = Escape-SingleQuotedShellValue -Value $SearchTerm
    $currentLog = '{0}{1}' -f $Directory, $CurrentPattern
    $historyLogs = '{0}{1}' -f $Directory, $HistoryPattern
    $sshTarget = if ($sshUser) { '{0}@{1}' -f $sshUser, $MachineHost } else { $MachineHost }

    return @(
        ('# host: {0}' -f $MachineHost),
        ('ssh {0} "grep -nH -C {1} -- ''{2}'' {3} 2>/dev/null"' -f $sshTarget, $SearchContext, $escaped, $currentLog),
        ('ssh {0} "zgrep -nH -C {1} -- ''{2}'' {3} 2>/dev/null"' -f $sshTarget, $SearchContext, $escaped, $historyLogs)
    )
}

function Resolve-Areas {
    param(
        [string[]]$RawAreas
    )

    $resolved = New-Object System.Collections.Generic.List[string]
    foreach ($item in ($RawAreas | Where-Object { $_ -ne $null })) {
        foreach ($part in ($item -split ',')) {
            $trimmed = $part.Trim()
            if ($trimmed) {
                [void]$resolved.Add($trimmed)
            }
        }
    }

    return $resolved.ToArray()
}

$serviceInfo = $environment.Services[$Service]
if (-not $serviceInfo) {
    throw "Unknown service: $Service"
}

$commands = New-Object System.Collections.Generic.List[string]

if ($Service -eq 'union-archiver-dispatch') {
    foreach ($machineHost in $serviceInfo.Hosts) {
        foreach ($directory in $serviceInfo.LogDirectories) {
            foreach ($line in (New-SearchCommand -MachineHost $machineHost -Directory $directory -CurrentPattern $serviceInfo.CurrentLogPattern -HistoryPattern $serviceInfo.HistoryLogPattern -SearchTerm $Keyword -SearchContext $Context)) {
                [void]$commands.Add($line)
            }
            [void]$commands.Add('')
        }
    }
}
else {
    $resolvedAreas = Resolve-Areas -RawAreas $Areas

    if (-not $resolvedAreas -or $resolvedAreas.Count -eq 0) {
        throw "Areas are required when searching archiver-manager."
    }

    foreach ($area in $resolvedAreas) {
        $hosts = $serviceInfo.HostsByArea["$area"]
        if (-not $hosts) {
            [void]$commands.Add("# area $area has no configured hosts")
            [void]$commands.Add('')
            continue
        }

        [void]$commands.Add('# area: {0}' -f $area)
        foreach ($machineHost in $hosts) {
            foreach ($directory in $serviceInfo.LogDirectories) {
                foreach ($line in (New-SearchCommand -MachineHost $machineHost -Directory $directory -CurrentPattern $serviceInfo.CurrentLogPattern -HistoryPattern $serviceInfo.HistoryLogPattern -SearchTerm $Keyword -SearchContext $Context)) {
                    [void]$commands.Add($line)
                }
                [void]$commands.Add('')
            }
        }
    }
}

$commands -join [Environment]::NewLine
