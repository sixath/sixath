[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')
. (Join-Path $PSScriptRoot '..\..\harness\judges\rule-judges.ps1')

function Assert-JudgeResultShape {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$Result
    )

    $required = @('CaseId', 'Kind', 'Passed', 'Message')
    foreach ($name in $required) {
        if (-not $Result.PSObject.Properties.Name.Contains($name)) {
            throw "Missing judge result property: $name"
        }
    }
}

$result = [pscustomobject]@{
    ExitCode = 0
    StdOut = '{"flow_id":"301_rqkkw0snhnmt","src_area_type":"400","dst_area_type":"301"}'
    StdErr = ''
}

$judges = @(
    [pscustomobject]@{ kind = 'exit_code'; equals = 0 },
    [pscustomobject]@{ kind = 'contains_text'; target = 'stdout'; value = '301_rqkkw0snhnmt' },
    [pscustomobject]@{ kind = 'not_contains_text'; target = 'stderr'; value = 'fatal' },
    [pscustomobject]@{ kind = 'json_field_equals'; field = 'dst_area_type'; equals = '301' },
    [pscustomobject]@{ kind = 'json_field_exists'; field = 'flow_id' },
    [pscustomobject]@{ kind = 'report_section_exists'; section = 'src_area_type' }
)

$judgeResults = Invoke-HarnessJudges -CaseId 'fixture.judges' -ExecutionResult $result -Judges $judges

if ($judgeResults.Count -ne 6) {
    throw "Expected 6 judge results, got $($judgeResults.Count)."
}

foreach ($judgeResult in $judgeResults) {
    Assert-JudgeResultShape -Result $judgeResult
}

if (($judgeResults | Where-Object { -not $_.Passed }).Count -ne 0) {
    throw 'Expected all judges to pass.'
}

$failingJudgeResults = Invoke-HarnessJudges -CaseId 'fixture.judges.failure' -ExecutionResult $result -Judges @(
    [pscustomobject]@{ kind = 'exit_code'; equals = 7 },
    [pscustomobject]@{ kind = 'contains_text'; target = 'stderr'; value = 'missing error text' },
    [pscustomobject]@{ kind = 'not_contains_text'; target = 'stdout'; value = 'flow_id' },
    [pscustomobject]@{ kind = 'json_field_exists'; field = 'missing_field' },
    [pscustomobject]@{ kind = 'report_section_exists'; section = 'Missing Section' }
)

if ($failingJudgeResults.Count -ne 5) {
    throw "Expected 5 failing judge results, got $($failingJudgeResults.Count)."
}

foreach ($judgeResult in $failingJudgeResults) {
    Assert-JudgeResultShape -Result $judgeResult
}

if (($failingJudgeResults | Where-Object { $_.Passed }).Count -ne 0) {
    throw 'Expected failing judges to report Passed = $false.'
}

if (($failingJudgeResults | Where-Object { $_.CaseId -ne 'fixture.judges.failure' }).Count -ne 0) {
    throw 'Expected failing judge results to preserve CaseId.'
}

if (($failingJudgeResults | Where-Object { [string]::IsNullOrWhiteSpace($_.Message) }).Count -ne 0) {
    throw 'Expected failing judge results to include a message.'
}

$unsupportedThrown = $false

try {
    Invoke-HarnessJudges -CaseId 'fixture.judges.unsupported' -ExecutionResult $result -Judges @(
        [pscustomobject]@{ kind = 'nope' }
    ) | Out-Null
} catch {
    $unsupportedThrown = $true

    if ($_.Exception.Message -notmatch 'Unsupported judge kind: nope') {
        throw "Expected explicit unsupported judge error, got: $($_.Exception.Message)"
    }
}

if (-not $unsupportedThrown) {
    throw 'Expected unsupported judge kind to throw.'
}

Write-Host 'PASS judges.test.ps1'
