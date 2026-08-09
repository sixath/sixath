[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')
. (Join-Path $PSScriptRoot '..\..\harness\reporters\fs-reporter.ps1')

function Assert-Equal {
    param(
        [Parameter(Mandatory)]
        $Actual,
        [Parameter(Mandatory)]
        $Expected,
        [Parameter(Mandatory)]
        [string]$Message
    )

    if ($Actual -ne $Expected) {
        throw "$Message`nExpected: $Expected`nActual: $Actual"
    }
}

function New-CaseResult {
    param(
        [Parameter(Mandatory)]
        [string]$CaseId,
        [Parameter(Mandatory)]
        [string]$Verdict,
        [Parameter(Mandatory)]
        [string]$Classification,
        [Parameter(Mandatory)]
        [int]$ExitCode,
        [Parameter(Mandatory)]
        [AllowEmptyString()]
        [string]$StdOut,
        [Parameter(Mandatory)]
        [AllowEmptyString()]
        [string]$StdErr
    )

    return [pscustomobject]@{
        CaseId = $CaseId
        Verdict = $Verdict
        Classification = $Classification
        Execution = [pscustomobject]@{
            ExitCode = $ExitCode
            StdOut = $StdOut
            StdErr = $StdErr
        }
        Judges = @()
    }
}

function Assert-SequenceEqual {
    param(
        [Parameter(Mandatory)]
        [AllowEmptyCollection()]
        [object[]]$Actual,
        [Parameter(Mandatory)]
        [AllowEmptyCollection()]
        [object[]]$Expected,
        [Parameter(Mandatory)]
        [string]$Message
    )

    if ($Actual.Count -ne $Expected.Count) {
        throw "$Message`nExpected count: $($Expected.Count)`nActual count: $($Actual.Count)"
    }

    for ($index = 0; $index -lt $Expected.Count; $index += 1) {
        if ($Actual[$index] -ne $Expected[$index]) {
            throw "$Message`nMismatch at index $index`nExpected: $($Expected[$index])`nActual: $($Actual[$index])"
        }
    }
}

function Get-RequiredRegexMatch {
    param(
        [Parameter(Mandatory)]
        [string]$Actual,
        [Parameter(Mandatory)]
        [string]$Pattern,
        [Parameter(Mandatory)]
        [string]$Message
    )

    $match = [regex]::Match($Actual, $Pattern)
    if (-not $match.Success) {
        throw "$Message`nPattern: $Pattern`nActual: $Actual"
    }

    return $match
}

$reportRoot = Join-Path $env:TEMP ("archive-move-ops-harness-report-test-{0}" -f ([guid]::NewGuid().ToString('N')))
if (Test-Path $reportRoot) {
    throw "Expected unique temp report root to be absent: $reportRoot"
}

try {
    $run1 = [pscustomobject]@{
        RunId = '20260424-120000'
        StartedAt = '2026-04-24T12:00:00Z'
        Results = @(
            (New-CaseResult -CaseId 'fixture.pass' -Verdict 'passed' -Classification 'ok' -ExitCode 0 -StdOut 'hello' -StdErr ''),
            (New-CaseResult -CaseId 'fixture.fail' -Verdict 'failed' -Classification 'assertion_failed' -ExitCode 7 -StdOut '' -StdErr 'boom'),
            (New-CaseResult -CaseId 'fixture.skip' -Verdict 'skipped' -Classification 'skipped' -ExitCode 0 -StdOut '' -StdErr '')
        )
        DurationMs = 345
    }

    $written1 = Write-HarnessReports -ReportRoot $reportRoot -RunResult $run1

    $historyRoot1 = Join-Path (Join-Path $reportRoot 'history') $run1.RunId
    $latestRoot = Join-Path $reportRoot 'latest'
    $historySummaryJsonPath1 = Join-Path $historyRoot1 'summary.json'
    $historySummaryMdPath1 = Join-Path $historyRoot1 'summary.md'
    $historyDetailPath1 = Join-Path $historyRoot1 'fixture.pass.json'
    $latestSummaryJsonPath = Join-Path $latestRoot 'summary.json'
    $latestSummaryMdPath = Join-Path $latestRoot 'summary.md'
    $latestDetailPath1 = Join-Path $latestRoot 'fixture.pass.json'

    Assert-Equal -Actual $written1.SummaryPath -Expected $historySummaryJsonPath1 -Message 'SummaryPath should point to history summary.json for the run.'
    Assert-Equal -Actual $written1.SummaryMarkdownPath -Expected $historySummaryMdPath1 -Message 'SummaryMarkdownPath should point to history summary.md for the run.'
    Assert-Equal -Actual $written1.DetailPaths.Count -Expected 3 -Message 'Expected one detail path per case result in the first run.'

    foreach ($path in @($written1.SummaryPath, $written1.SummaryMarkdownPath, $historyDetailPath1, $latestSummaryJsonPath, $latestSummaryMdPath, $latestDetailPath1)) {
        if (-not (Test-Path $path)) {
            throw "Expected report output at $path"
        }
    }

    $summary1 = Get-Content $written1.SummaryPath -Raw | ConvertFrom-Json
    Assert-Equal -Actual $summary1.run_id -Expected $run1.RunId -Message 'summary.json should preserve run_id.'
    Assert-Equal -Actual $summary1.total -Expected 3 -Message 'summary.json should count total results.'
    Assert-Equal -Actual $summary1.passed -Expected 1 -Message 'summary.json should count passed results.'
    Assert-Equal -Actual $summary1.failed -Expected 1 -Message 'summary.json should count failed results.'
    Assert-Equal -Actual $summary1.skipped -Expected 1 -Message 'summary.json should count skipped results.'
    Assert-Equal -Actual $summary1.total_duration_ms -Expected 345 -Message 'summary.json should preserve total duration.'
    Assert-SequenceEqual -Actual @($summary1.failed_case_ids) -Expected @('fixture.fail') -Message 'summary.json should list failing case ids.'

    $summaryMarkdown1 = Get-Content $written1.SummaryMarkdownPath -Raw
    if ($summaryMarkdown1 -notmatch '- total_duration_ms: 345') {
        throw "Expected summary.md to include total_duration_ms, got: $summaryMarkdown1"
    }

    if ($summaryMarkdown1 -notmatch '- failed_case_ids: fixture\.fail') {
        throw "Expected summary.md to include failing case ids, got: $summaryMarkdown1"
    }

    $detail1 = Get-Content $historyDetailPath1 -Raw | ConvertFrom-Json
    Assert-Equal -Actual $detail1.CaseId -Expected 'fixture.pass' -Message 'detail JSON should preserve CaseId.'
    Assert-Equal -Actual $detail1.Verdict -Expected 'passed' -Message 'detail JSON should preserve Verdict.'
    Assert-Equal -Actual $detail1.Classification -Expected 'ok' -Message 'detail JSON should preserve Classification.'
    Assert-Equal -Actual $detail1.Execution.ExitCode -Expected 0 -Message 'detail JSON should preserve execution exit code.'
    Assert-Equal -Actual $detail1.Execution.StdOut -Expected 'hello' -Message 'detail JSON should preserve execution stdout.'
    Assert-Equal -Actual $detail1.Execution.StdErr -Expected '' -Message 'detail JSON should preserve execution stderr.'

    $run2 = [pscustomobject]@{
        RunId = '20260424-120500'
        StartedAt = '2026-04-24T12:05:00Z'
        Results = @(
            (New-CaseResult -CaseId 'fixture.pass' -Verdict 'passed' -Classification 'ok' -ExitCode 0 -StdOut 'fresh' -StdErr '')
        )
        DurationMs = 12
    }

    $written2 = Write-HarnessReports -ReportRoot $reportRoot -RunResult $run2
    $historyRoot2 = Join-Path (Join-Path $reportRoot 'history') $run2.RunId
    $historySummaryJsonPath2 = Join-Path $historyRoot2 'summary.json'
    $historyDetailPath2 = Join-Path $historyRoot2 'fixture.pass.json'
    $staleLatestPath = Join-Path $latestRoot 'fixture.fail.json'

    Assert-Equal -Actual $written2.SummaryPath -Expected $historySummaryJsonPath2 -Message 'Second SummaryPath should point to the second history summary.json.'

    foreach ($path in @($written2.SummaryPath, $written2.SummaryMarkdownPath, $historyDetailPath2, $latestSummaryJsonPath, $latestSummaryMdPath, (Join-Path $latestRoot 'fixture.pass.json'))) {
        if (-not (Test-Path $path)) {
            throw "Expected refreshed report output at $path"
        }
    }

    if (Test-Path $staleLatestPath) {
        throw "Expected latest reports to be refreshed and remove stale detail file: $staleLatestPath"
    }

    $latestSummary = Get-Content $latestSummaryJsonPath -Raw | ConvertFrom-Json
    Assert-Equal -Actual $latestSummary.run_id -Expected $run2.RunId -Message 'latest summary.json should mirror the newest run.'
    Assert-Equal -Actual $latestSummary.total -Expected 1 -Message 'latest summary.json should mirror the newest run total.'
    Assert-Equal -Actual $latestSummary.passed -Expected 1 -Message 'latest summary.json should mirror the newest run passed count.'
    Assert-Equal -Actual $latestSummary.failed -Expected 0 -Message 'latest summary.json should mirror the newest run failed count.'
    Assert-Equal -Actual $latestSummary.skipped -Expected 0 -Message 'latest summary.json should mirror the newest run skipped count.'
    Assert-Equal -Actual $latestSummary.total_duration_ms -Expected 12 -Message 'latest summary.json should mirror the newest run duration.'
    Assert-SequenceEqual -Actual @($latestSummary.failed_case_ids) -Expected @() -Message 'latest summary.json should mirror the newest run failing ids.'

    $latestDetail = Get-Content (Join-Path $latestRoot 'fixture.pass.json') -Raw | ConvertFrom-Json
    Assert-Equal -Actual $latestDetail.Execution.StdOut -Expected 'fresh' -Message 'latest detail JSON should mirror the newest run detail content.'

    $runHarnessPath = Join-Path $PSScriptRoot '..\..\harness\run-harness.ps1'
    $runHarnessResult = Invoke-HarnessCommand -FilePath $runHarnessPath
    $runHarnessOutput = $runHarnessResult.StdOut

    if ($runHarnessResult.ExitCode -ne 0) {
        throw "Expected run-harness.ps1 without arguments to succeed for Task 5, got exit code $($runHarnessResult.ExitCode) with stderr: $($runHarnessResult.StdErr)"
    }

    $summaryMatch = Get-RequiredRegexMatch -Actual $runHarnessOutput -Pattern 'Summary written to (?<path>.+summary\.md)' -Message 'Expected run-harness.ps1 to print the summary markdown path.'
    $summaryPath = $summaryMatch.Groups['path'].Value.Trim()

    if (-not (Test-Path -LiteralPath $summaryPath)) {
        throw "Expected printed summary path to exist: $summaryPath"
    }

    if ($runHarnessResult.StdErr -match 'Join-Path : Cannot bind argument to parameter ''Path'' because it is an empty string') {
        throw "Expected run-harness.ps1 to avoid parameter-binding failure, got: $($runHarnessResult.StdErr)"
    }
} finally {
    if (Test-Path $reportRoot) {
        Remove-Item $reportRoot -Recurse -Force
    }
}

Write-Host 'PASS reporter.test.ps1'
