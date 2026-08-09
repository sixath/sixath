[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$testFiles = @(
    (Join-Path $PSScriptRoot 'build-entry-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-flow-investigation-template.test.ps1'),
    (Join-Path $PSScriptRoot 'build-followup-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-investigation-report.test.ps1'),
    (Join-Path $PSScriptRoot 'build-uid-error-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-storage-worker-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-data-channel-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'investigate-flow.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\adapter.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\judges.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\learning-sink.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\load-cases.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\reporter.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\runner.test.ps1')
)

foreach ($testFile in $testFiles) {
    & $testFile
}
