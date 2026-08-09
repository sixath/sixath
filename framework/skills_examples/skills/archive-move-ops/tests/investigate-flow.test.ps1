[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot '..\scripts\investigate-flow.ps1'
$stubPath = Join-Path $PSScriptRoot 'fixtures\ssh-host-key-failed.cmd'

$blocked = & $scriptPath -FlowId '4_v0cag1d3guo8' -SshExecutable $stubPath -StrictHostKeyChecking yes | ConvertFrom-Json
if ($blocked.conclusion.status -ne 'blocked_ssh_host_key') {
    throw "Expected blocked_ssh_host_key, got $($blocked.conclusion.status)"
}

$firstResult = @($blocked.archiver_manager)[0]
if ($firstResult.error_category -ne 'blocked_ssh_host_key') {
    throw "Expected first result error_category blocked_ssh_host_key, got $($firstResult.error_category)"
}

if ($blocked.conclusion.message -notmatch 'Host key verification failed') {
    throw "Expected conclusion message to mention host key verification failure."
}

$dryRun = & $scriptPath -FlowId '4_v0cag1d3guo8' -DryRun -StrictHostKeyChecking accept-new | ConvertFrom-Json
$dryRunCommand = @($dryRun.archiver_manager)[0].command
if ($dryRunCommand -notmatch [regex]::Escape('-o StrictHostKeyChecking=accept-new')) {
    throw "Expected dry-run SSH command to include StrictHostKeyChecking=accept-new. Command: $dryRunCommand"
}

$dateScoped = & $scriptPath -FlowId '4_v0cag1d3guo8' -UnionDatePrefix '2026-04-20' -DryRun | ConvertFrom-Json
$dateScopedCommand = @($dateScoped.archiver_manager)[0].command
if ($dateScopedCommand -notmatch [regex]::Escape('ls -1')) {
    throw "Expected date-scoped dry-run command to enumerate candidate files first. Command: $dateScopedCommand"
}
if ($dateScopedCommand -notmatch [regex]::Escape('2026-04-20')) {
    throw "Expected date-scoped dry-run command to include UnionDatePrefix filter. Command: $dateScopedCommand"
}

Write-Host 'PASS investigate-flow.test.ps1'
