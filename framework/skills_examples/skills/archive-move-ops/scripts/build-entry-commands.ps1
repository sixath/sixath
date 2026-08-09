[CmdletBinding(DefaultParameterSetName = 'TraceId')]
param(
    [Parameter(Mandatory = $true, ParameterSetName = 'TraceId')]
    [string]$TraceId,

    [Parameter(Mandatory = $true, ParameterSetName = 'FlowId')]
    [string]$FlowId,

    [Parameter(ParameterSetName = 'TraceId')]
    [Parameter(ParameterSetName = 'FlowId')]
    [string]$UnionDatePrefix,

    [int]$Context = 2
)

$searchScriptPath = Join-Path $PSScriptRoot 'build-search-commands.ps1'
$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User

function New-UnionDateBatchCommands {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Keyword,

        [Parameter(Mandatory = $true)]
        [string]$DatePrefix
    )

    $commands = New-Object System.Collections.Generic.List[string]
    $hosts = $environment.Services['union-archiver-dispatch'].Hosts
    $directories = $environment.Services['union-archiver-dispatch'].LogDirectories

    foreach ($machineHost in $hosts) {
        $sshTarget = if ($sshUser) { '{0}@{1}' -f $sshUser, $machineHost } else { $machineHost }
        foreach ($directory in $directories) {
            $historyGlob = '{0}union-archiver-dispatch-{1}*.log.gz' -f $directory, $DatePrefix
            [void]$commands.Add('# host: {0}' -f $machineHost)
            $command = 'ssh ' + $sshTarget +
                ' "zcat ' + $historyGlob +
                " 2>/dev/null | grep -- '" + $Keyword +
                "' | grep 'Worker.startSyncDispatch()'" + '"'
            [void]$commands.Add($command)
            [void]$commands.Add('')
        }
    }

    return $commands -join [Environment]::NewLine
}

switch ($PSCmdlet.ParameterSetName) {
    'TraceId' {
        $commands = & $searchScriptPath `
            -Service 'union-archiver-dispatch' `
            -Keyword $TraceId `
            -Context $Context

        $output = New-Object System.Collections.Generic.List[string]
        foreach ($line in @(
            '# mode: traceId'
            '# traceId: {0}' -f $TraceId
            '# next: run these union-archiver-dispatch commands first'
            ''
            $commands
        )) {
            [void]$output.Add($line)
        }

        if ($UnionDatePrefix) {
            [void]$output.Add('')
            [void]$output.Add('# union date batch: {0}' -f $UnionDatePrefix)
            [void]$output.Add('# next: search date-scoped compressed logs and filter Worker.startSyncDispatch() first')
            [void]$output.Add('')
            [void]$output.Add((New-UnionDateBatchCommands -Keyword $TraceId -DatePrefix $UnionDatePrefix))
        }

        $output -join [Environment]::NewLine
        break
    }
    'FlowId' {
        if ($FlowId -notmatch '^(?<area>\d+)_') {
            throw "Unable to infer area from flow_id: $FlowId"
        }

        $area = $Matches['area']
        $commands = & $searchScriptPath `
            -Service 'archiver-manager' `
            -Keyword $FlowId `
            -Areas @($area) `
            -Context $Context

        $output = New-Object System.Collections.Generic.List[string]
        foreach ($line in @(
            '# mode: flowId'
            '# flow_id: {0}' -f $FlowId
            '# inferred area: {0}' -f $area
            '# note: flow_id mode can only infer the prefixed area; use dispatch logs to confirm the full src -> dst route'
            ''
            $commands
        )) {
            [void]$output.Add($line)
        }

        if ($UnionDatePrefix) {
            [void]$output.Add('')
            [void]$output.Add('# union date batch: {0}' -f $UnionDatePrefix)
            [void]$output.Add('# next: search date-scoped compressed union logs and filter Worker.startSyncDispatch() first')
            [void]$output.Add('')
            [void]$output.Add((New-UnionDateBatchCommands -Keyword $FlowId -DatePrefix $UnionDatePrefix))
        }

        $output -join [Environment]::NewLine
        break
    }
    default {
        throw "Unsupported parameter set: $($PSCmdlet.ParameterSetName)"
    }
}
