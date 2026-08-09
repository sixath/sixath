[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')

function Assert-RunnerResultShape {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$Result
    )

    $required = @('Command', 'ExitCode', 'StdOut', 'StdErr', 'DurationMs', 'Succeeded')
    foreach ($name in $required) {
        if (-not $Result.PSObject.Properties.Name.Contains($name)) {
            throw "Missing result property: $name"
        }
    }
}

$result = Invoke-HarnessCommand -FilePath (Join-Path $PSScriptRoot '..\..\scripts\parse-dispatch-log.ps1') -ArgumentList @(
    '-LogLine',
    'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)',
    '-AsJson'
)

Assert-RunnerResultShape -Result $result

if (-not $result.Succeeded) {
    throw 'Expected parse-dispatch-log invocation to succeed.'
}

if ($result.ExitCode -ne 0) {
    throw "Expected exit code 0, got $($result.ExitCode)."
}

if ($result.StdOut -notmatch '301_rqkkw0snhnmt') {
    throw 'Expected stdout to include flow id.'
}

$entryResult = Invoke-HarnessCommand -FilePath (Join-Path $PSScriptRoot '..\..\scripts\build-entry-commands.ps1') -ArgumentList @(
    '-TraceId',
    'trace-demo-123'
)

Assert-RunnerResultShape -Result $entryResult

if (-not $entryResult.Succeeded) {
    throw "Expected build-entry-commands invocation to succeed, got stderr: $($entryResult.StdErr)"
}

if ($entryResult.ExitCode -ne 0) {
    throw "Expected build-entry-commands exit code 0, got $($entryResult.ExitCode)."
}

if ($entryResult.StdOut -notmatch 'union-archiver-dispatch') {
    throw 'Expected build-entry-commands stdout to include service name.'
}

if ($entryResult.StdOut -notmatch 'trace-demo-123') {
    throw 'Expected build-entry-commands stdout to include trace id.'
}

$failureScript = [System.IO.Path]::GetTempFileName() + '.ps1'

try {
    Set-Content -Path $failureScript -Value @'
Write-Error 'runner failure path'
exit 7
'@

    $failureResult = Invoke-HarnessCommand -FilePath $failureScript
    Assert-RunnerResultShape -Result $failureResult

    if ($failureResult.ExitCode -ne 7) {
        throw "Expected exit code 7, got $($failureResult.ExitCode)."
    }

    if ($failureResult.Succeeded) {
        throw 'Expected failure result to report Succeeded = $false.'
    }

    if ($failureResult.StdErr -notmatch 'runner failure path') {
        throw 'Expected stderr to capture the failure message.'
    }
} finally {
    Remove-Item $failureScript -ErrorAction SilentlyContinue
}

Write-Host 'PASS runner.test.ps1'
