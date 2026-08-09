function Write-HarnessReports {
    param(
        [Parameter(Mandatory)]
        [string]$ReportRoot,
        [Parameter(Mandatory)]
        [pscustomobject]$RunResult
    )

    $latestRoot = Join-Path $ReportRoot 'latest'
    $historyRoot = Join-Path (Join-Path $ReportRoot 'history') $RunResult.RunId
    if (Test-Path $latestRoot) {
        Remove-Item -Path $latestRoot -Recurse -Force
    }

    New-Item -ItemType Directory -Force -Path $latestRoot, $historyRoot | Out-Null

    $passedCount = @($RunResult.Results | Where-Object Verdict -eq 'passed').Count
    $failedCount = @($RunResult.Results | Where-Object Verdict -eq 'failed').Count
    $skippedCount = @($RunResult.Results | Where-Object Verdict -eq 'skipped').Count
    $failedCaseIds = @($RunResult.Results | Where-Object Verdict -eq 'failed' | ForEach-Object { $_.CaseId })
    $totalDurationMs = if ($RunResult.PSObject.Properties.Name.Contains('DurationMs')) {
        [int]$RunResult.DurationMs
    } else {
        [int](@($RunResult.Results | ForEach-Object {
            if ($_.PSObject.Properties.Name.Contains('Execution') -and $_.Execution.PSObject.Properties.Name.Contains('DurationMs')) {
                [int]$_.Execution.DurationMs
            } else {
                0
            }
        }) | Measure-Object -Sum).Sum
    }

    $summary = [pscustomobject]@{
        run_id = $RunResult.RunId
        started_at = $RunResult.StartedAt
        total = $RunResult.Results.Count
        passed = $passedCount
        failed = $failedCount
        skipped = $skippedCount
        total_duration_ms = $totalDurationMs
        failed_case_ids = $failedCaseIds
    }

    $summaryJsonPath = Join-Path $historyRoot 'summary.json'
    $summaryMdPath = Join-Path $historyRoot 'summary.md'
    $detailPaths = New-Object System.Collections.Generic.List[string]

    $summary | ConvertTo-Json -Depth 10 | Set-Content -Path $summaryJsonPath -Encoding utf8
    @(
        '# Harness Summary'
        ''
        "- run_id: $($summary.run_id)"
        "- total: $($summary.total)"
        "- passed: $($summary.passed)"
        "- failed: $($summary.failed)"
        "- skipped: $($summary.skipped)"
        "- total_duration_ms: $($summary.total_duration_ms)"
        "- failed_case_ids: $(if ($summary.failed_case_ids.Count -gt 0) { $summary.failed_case_ids -join ', ' } else { '(none)' })"
    ) | Set-Content -Path $summaryMdPath -Encoding utf8

    foreach ($result in $RunResult.Results) {
        $detailPath = Join-Path $historyRoot "$($result.CaseId).json"
        $result | ConvertTo-Json -Depth 20 | Set-Content -Path $detailPath -Encoding utf8
        $detailPaths.Add($detailPath)
    }

    Copy-Item -Path (Join-Path $historyRoot '*') -Destination $latestRoot -Force

    return [pscustomobject]@{
        SummaryPath = $summaryJsonPath
        SummaryMarkdownPath = $summaryMdPath
        DetailPaths = $detailPaths
    }
}
