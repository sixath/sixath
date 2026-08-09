[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$FlowId,

    [string]$UnionDatePrefix,

    [int]$Context = 2,

    [ValidateSet('accept-new', 'yes', 'no')]
    [string]$StrictHostKeyChecking = 'accept-new',

    [string]$SshExecutable = 'ssh',

    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User

function Write-ErrorLog {
    param(
        [Parameter(Mandatory = $true)][string]$Message,
        [string]$Stage = '',
        [string]$Service = '',
        [string]$HostName = '',
        [string]$ErrorCategory = ''
    )

    $parts = @()
    $parts += (Get-Date).ToString('o')
    $parts += '[ERROR]'
    if ($Stage) { $parts += "stage=$Stage" }
    if ($Service) { $parts += "service=$Service" }
    if ($HostName) { $parts += "host=$HostName" }
    if ($ErrorCategory) { $parts += "category=$ErrorCategory" }
    $parts += $Message
    [Console]::Error.WriteLine(($parts -join ' '))
}

function Escape-SingleQuotedShellValue {
    param([Parameter(Mandatory = $true)][string]$Value)
    return $Value -replace "'", "'""'""'"
}

function New-Result {
    param(
        [Parameter(Mandatory = $true)][string]$Stage,
        [Parameter(Mandatory = $true)][string]$Service,
        [string]$Area,
        [string]$HostName,
        [string]$Command,
        [int]$ExitCode = 0,
        [string]$Output = '',
        [string]$ErrorText = '',
        [string]$ErrorCategory = '',
        [string]$SkippedReason = ''
    )

    [pscustomobject]@{
        stage = $Stage
        service = $Service
        area = $Area
        host = $HostName
        command = $Command
        exit_code = $ExitCode
        output = $Output
        error = $ErrorText
        error_category = $ErrorCategory
        skipped_reason = $SkippedReason
    }
}

function Get-SshErrorCategory {
    param(
        [int]$ExitCode,
        [string]$Text
    )

    if ($ExitCode -eq 0) {
        return ''
    }
    if ($Text -match 'Host key verification failed') {
        return 'blocked_ssh_host_key'
    }
    if ($Text -match 'Permission denied|publickey|BatchMode') {
        return 'blocked_ssh_auth'
    }
    if ($Text -match 'Connection timed out|Operation timed out|timed out') {
        return 'blocked_ssh_timeout'
    }
    if ($Text -match 'Could not resolve|No route to host|Connection refused') {
        return 'blocked_ssh_network'
    }
    return 'blocked_ssh_error'
}

function Invoke-RemoteCommand {
    param(
        [Parameter(Mandatory = $true)][string]$Stage,
        [Parameter(Mandatory = $true)][string]$Service,
        [string]$Area,
        [Parameter(Mandatory = $true)][string]$HostName,
        [Parameter(Mandatory = $true)][string]$RemoteCommand
    )

    $target = if ($sshUser) { "$sshUser@$HostName" } else { $HostName }
    $sshArgs = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=8', '-o', "StrictHostKeyChecking=$StrictHostKeyChecking", $target, $RemoteCommand)
    $displayCommand = "$SshExecutable " + ($sshArgs -join ' ')

    if ($DryRun) {
        return New-Result -Stage $Stage -Service $Service -Area $Area -HostName $HostName -Command $displayCommand
    }

    $previousErrorAction = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $SshExecutable @sshArgs 2>&1
        $exitCode = $LASTEXITCODE
    }
    catch {
        $exitCode = -1
        $exceptionText = $_.Exception.Message
        Write-ErrorLog -Message "SSH invocation threw exception. command=$displayCommand error=$exceptionText" -Stage $Stage -Service $Service -HostName $HostName -ErrorCategory 'blocked_ssh_error'
        return New-Result -Stage $Stage -Service $Service -Area $Area -HostName $HostName -Command $displayCommand -ExitCode $exitCode -Output '' -ErrorText $exceptionText -ErrorCategory 'blocked_ssh_error'
    }
    finally {
        $ErrorActionPreference = $previousErrorAction
    }
    $text = ($output -join [Environment]::NewLine)
    $errorText = if ($exitCode -ne 0) { $text } else { '' }
    $errorCategory = Get-SshErrorCategory -ExitCode $exitCode -Text $text
    if ($errorCategory) {
        Write-ErrorLog -Message "SSH command failed. exit_code=$exitCode command=$displayCommand error=$errorText" -Stage $Stage -Service $Service -HostName $HostName -ErrorCategory $errorCategory
    }
    return New-Result -Stage $Stage -Service $Service -Area $Area -HostName $HostName -Command $displayCommand -ExitCode $exitCode -Output $text -ErrorText $errorText -ErrorCategory $errorCategory
}

function Search-LogSet {
    param(
        [Parameter(Mandatory = $true)][string]$Stage,
        [Parameter(Mandatory = $true)][string]$Service,
        [string]$Area,
        [Parameter(Mandatory = $true)][string[]]$Hosts,
        [Parameter(Mandatory = $true)][string[]]$Directories,
        [Parameter(Mandatory = $true)][string]$CurrentPattern,
        [Parameter(Mandatory = $true)][string]$HistoryPattern,
        [Parameter(Mandatory = $true)][string]$Keyword,
        [string]$DatePrefix,
        [string]$Pipe
    )

    $results = New-Object System.Collections.Generic.List[object]
    $escaped = Escape-SingleQuotedShellValue -Value $Keyword
    foreach ($hostName in $Hosts) {
        foreach ($directory in $Directories) {
            $current = "$directory$CurrentPattern"
            $history = "$directory$HistoryPattern"
            if ($DatePrefix) {
                $escapedDatePrefix = Escape-SingleQuotedShellValue -Value $DatePrefix
                $cmd = "candidate_files=`$(ls -1 $current $history 2>/dev/null | grep -- '$escapedDatePrefix'); if [ -n `"$candidate_files`" ]; then for f in `$candidate_files; do case `"$f`" in *.gz) zgrep -nH -C $Context -- '$escaped' `"$f`" 2>/dev/null ;; *) grep -nH -C $Context -- '$escaped' `"$f`" 2>/dev/null ;; esac; done; else grep -nH -C $Context -- '$escaped' $current 2>/dev/null; zgrep -nH -C $Context -- '$escaped' $history 2>/dev/null; fi"
            }
            else {
                $cmd = "grep -nH -C $Context -- '$escaped' $current 2>/dev/null; zgrep -nH -C $Context -- '$escaped' $history 2>/dev/null"
            }
            if ($Pipe) {
                $cmd = "($cmd) | $Pipe"
            }
            [void]$results.Add((Invoke-RemoteCommand -Stage $Stage -Service $Service -Area $Area -HostName $hostName -RemoteCommand $cmd))
        }
    }
    return $results
}

function Get-FieldValue {
    param(
        [string]$Text,
        [Parameter(Mandatory = $true)][string]$FieldName
    )

    if (-not $Text) {
        return ''
    }

    $normalized = $Text -replace '\\\"', '"'
    $patterns = @(
        ('"{0}"\s*:\s*"?(?<value>[^",}}\s]+)"?' -f [regex]::Escape($FieldName)),
        ('{0}\((?<value>[^)]*)\)' -f [regex]::Escape($FieldName)),
        ('{0}=(?<value>[^&\s\\n]+)' -f [regex]::Escape($FieldName))
    )
    foreach ($pattern in $patterns) {
        $match = [regex]::Match($normalized, $pattern)
        if ($match.Success) {
            return $match.Groups['value'].Value
        }
    }
    return ''
}

function Get-FirstFieldValue {
    param(
        [string]$Text,
        [Parameter(Mandatory = $true)][string[]]$FieldNames
    )

    foreach ($fieldName in $FieldNames) {
        $value = Get-FieldValue -Text $Text -FieldName $fieldName
        if ($value) {
            return [pscustomobject]@{ field = $fieldName; value = $value }
        }
    }
    return [pscustomobject]@{ field = $FieldNames[0]; value = '' }
}

function Join-Outputs {
    param([object[]]$Results)
    return ($Results | ForEach-Object { $_.output }) -join [Environment]::NewLine
}

function Get-HostsByArea {
    param([Parameter(Mandatory = $true)][string]$Service, [Parameter(Mandatory = $true)][string]$Area)
    $svc = $environment.Services[$Service]
    if (-not $svc -or -not $svc.HostsByArea) {
        return @()
    }
    $hosts = $svc.HostsByArea["$Area"]
    if ($hosts) { return @($hosts) }
    return @()
}

if ($FlowId -notmatch '^(?<area>\d+)_') {
    throw "Unable to infer area from flow_id: $FlowId"
}

$inferredArea = $Matches['area']
$managerSvc = $environment.Services['archiver-manager']
$managerHosts = Get-HostsByArea -Service 'archiver-manager' -Area $inferredArea

$managerResults = @()
if ($managerHosts.Count -eq 0) {
    $managerResults = @(New-Result -Stage 'archiver_manager' -Service 'archiver-manager' -Area $inferredArea -SkippedReason "No archiver-manager hosts configured for area $inferredArea")
}
else {
    $managerResults = @(Search-LogSet -Stage 'archiver_manager' -Service 'archiver-manager' -Area $inferredArea -Hosts $managerHosts -Directories $managerSvc.LogDirectories -CurrentPattern $managerSvc.CurrentLogPattern -HistoryPattern $managerSvc.HistoryLogPattern -Keyword $FlowId -DatePrefix $UnionDatePrefix)
}

$managerText = Join-Outputs -Results $managerResults
$taskInfo = Get-FirstFieldValue -Text $managerText -FieldNames @('task_id', 'task-id', 'taskId', 'taskid')
$uidInfo = Get-FirstFieldValue -Text $managerText -FieldNames @('uid')
$srcUidInfo = Get-FirstFieldValue -Text $managerText -FieldNames @('src_uid')
$dstUidInfo = Get-FirstFieldValue -Text $managerText -FieldNames @('dst_uid', 'dest_uid')
$gidInfo = Get-FirstFieldValue -Text $managerText -FieldNames @('gid')
$dscidInfo = Get-FirstFieldValue -Text $managerText -FieldNames @('dscid')

$uidErrorResults = New-Object System.Collections.Generic.List[object]
if ($srcUidInfo.value) {
    foreach ($result in (Search-LogSet -Stage 'uid_error_source' -Service 'archiver-manager' -Area $inferredArea -Hosts $managerHosts -Directories $managerSvc.LogDirectories -CurrentPattern $managerSvc.CurrentLogPattern -HistoryPattern $managerSvc.HistoryLogPattern -Keyword $srcUidInfo.value -DatePrefix $UnionDatePrefix -Pipe "grep -i -C $Context -- 'ERROR'")) {
        [void]$uidErrorResults.Add($result)
    }
}
else {
    [void]$uidErrorResults.Add((New-Result -Stage 'uid_error_source' -Service 'archiver-manager' -Area $inferredArea -SkippedReason 'Missing src_uid in archiver-manager hits'))
}

if ($dstUidInfo.value) {
    foreach ($result in (Search-LogSet -Stage 'uid_error_destination' -Service 'archiver-manager' -Area $inferredArea -Hosts $managerHosts -Directories $managerSvc.LogDirectories -CurrentPattern $managerSvc.CurrentLogPattern -HistoryPattern $managerSvc.HistoryLogPattern -Keyword $dstUidInfo.value -DatePrefix $UnionDatePrefix -Pipe "grep -i -C $Context -- 'ERROR'")) {
        [void]$uidErrorResults.Add($result)
    }
}
else {
    [void]$uidErrorResults.Add((New-Result -Stage 'uid_error_destination' -Service 'archiver-manager' -Area $inferredArea -SkippedReason 'Missing dst_uid/dest_uid in archiver-manager hits'))
}

$dataChannelResults = New-Object System.Collections.Generic.List[object]
$dataSvc = $environment.Services['data-channel']
$dataHosts = Get-HostsByArea -Service 'data-channel' -Area $inferredArea
if (-not $taskInfo.value -or -not $uidInfo.value) {
    [void]$dataChannelResults.Add((New-Result -Stage 'data_channel' -Service 'data-channel' -Area $inferredArea -SkippedReason 'Missing task_id or uid in archiver-manager hits'))
}
elseif ($dataHosts.Count -eq 0) {
    [void]$dataChannelResults.Add((New-Result -Stage 'data_channel' -Service 'data-channel' -Area $inferredArea -SkippedReason "No data-channel hosts configured for area $inferredArea"))
}
else {
    foreach ($hostName in $dataHosts) {
        foreach ($target in $dataSvc.DeployLogTargets) {
            $pathPattern = "$($target.Directory)$($target.Pattern)"
            $escapedTask = Escape-SingleQuotedShellValue -Value $taskInfo.value
            $cmd = "grep -nH -C $Context -- '$escapedTask' $pathPattern 2>/dev/null"
            [void]$dataChannelResults.Add((Invoke-RemoteCommand -Stage "data_channel_$($target.Name)" -Service 'data-channel' -Area $inferredArea -HostName $hostName -RemoteCommand $cmd))
        }
        foreach ($directory in $dataSvc.StorageWorker.LogDirectories) {
            $pathPattern = "$directory$($dataSvc.StorageWorker.Pattern)"
            $escapedUid = Escape-SingleQuotedShellValue -Value $uidInfo.value
            $cmd = "grep -nH -C $Context -- '$escapedUid' $pathPattern 2>/dev/null"
            [void]$dataChannelResults.Add((Invoke-RemoteCommand -Stage 'data_channel_storage_worker_uid' -Service 'data-channel' -Area $inferredArea -HostName $hostName -RemoteCommand $cmd))
        }
    }
}

$dataText = Join-Outputs -Results $dataChannelResults
if (-not $gidInfo.value) {
    $gidInfo = Get-FirstFieldValue -Text $dataText -FieldNames @('gid')
}
if (-not $dscidInfo.value) {
    $dscidInfo = Get-FirstFieldValue -Text $dataText -FieldNames @('dscid')
}

$storageResults = New-Object System.Collections.Generic.List[object]
$storageSvc = $environment.Services['storage-worker']
if (-not $uidInfo.value -or -not $gidInfo.value -or -not $dscidInfo.value) {
    [void]$storageResults.Add((New-Result -Stage 'storage_worker' -Service 'storage-worker' -Area $inferredArea -SkippedReason 'Missing uid, gid, or dscid for storage-worker search'))
}
else {
    $hostsByDscid = $storageSvc.HostsByAreaDscid["$inferredArea"]
    $storageHosts = if ($hostsByDscid) { @($hostsByDscid["$($dscidInfo.value)"]) } else { @() }
    if ($storageHosts.Count -eq 0) {
        [void]$storageResults.Add((New-Result -Stage 'storage_worker' -Service 'storage-worker' -Area $inferredArea -SkippedReason "No storage-worker hosts configured for area $inferredArea dscid $($dscidInfo.value)"))
    }
    else {
        $escapedUid = Escape-SingleQuotedShellValue -Value $uidInfo.value
        $escapedGid = Escape-SingleQuotedShellValue -Value $gidInfo.value
        foreach ($hostName in $storageHosts) {
            foreach ($directory in $storageSvc.LogDirectories) {
                $current = "$directory$($storageSvc.CurrentLogPattern)"
                $history = "$directory$($storageSvc.HistoryLogPattern)"
                $cmd = "(grep -nH -C $Context -- '$escapedUid' $current 2>/dev/null; zgrep -nH -C $Context -- '$escapedUid' $history 2>/dev/null) | grep -- '$escapedGid'"
                [void]$storageResults.Add((Invoke-RemoteCommand -Stage 'storage_worker' -Service 'storage-worker' -Area $inferredArea -HostName $hostName -RemoteCommand $cmd))
            }
        }
    }
}

$blockingErrors = @($managerResults + $uidErrorResults + $dataChannelResults + $storageResults | Where-Object { $_.error_category })
$managerHits = @($managerResults | Where-Object { $_.exit_code -eq 0 -and $_.output })
$status = if ($managerHits.Count -eq 0 -and $blockingErrors.Count -gt 0) {
    $blockingErrors[0].error_category
}
elseif ($managerHits.Count -gt 0) {
    'evidence_found'
}
else {
    'no_archiver_manager_hit'
}

$conclusion = switch ($status) {
    'blocked_ssh_host_key' { "SSH host key verification failed on $($blockingErrors[0].host): $($blockingErrors[0].error)" }
    'blocked_ssh_auth' { "SSH authentication failed on $($blockingErrors[0].host): $($blockingErrors[0].error)" }
    'blocked_ssh_timeout' { "SSH connection timed out on $($blockingErrors[0].host): $($blockingErrors[0].error)" }
    'blocked_ssh_network' { "SSH network access failed on $($blockingErrors[0].host): $($blockingErrors[0].error)" }
    'blocked_ssh_error' { "SSH execution failed on $($blockingErrors[0].host): $($blockingErrors[0].error)" }
    'evidence_found' { 'archiver-manager evidence was found; review extracted identifiers and downstream stages.' }
    default { 'No archiver-manager hit was found for the inferred flow_id area.' }
}

[pscustomobject]@{
    flow_id = $FlowId
    inferred_area = $inferredArea
    union_date_prefix = $UnionDatePrefix
    dry_run = [bool]$DryRun
    extracted = [ordered]@{
        task_id = $taskInfo.value
        uid = $uidInfo.value
        src_uid = $srcUidInfo.value
        dst_uid = $dstUidInfo.value
        gid = $gidInfo.value
        dscid = $dscidInfo.value
    }
    archiver_manager = $managerResults
    uid_error_search = $uidErrorResults
    data_channel = $dataChannelResults
    storage_worker = $storageResults
    conclusion = [ordered]@{
        status = $status
        message = $conclusion
    }
} | ConvertTo-Json -Depth 8
