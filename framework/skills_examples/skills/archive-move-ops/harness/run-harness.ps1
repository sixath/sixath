[CmdletBinding()]
param(
    [AllowEmptyString()]
    [string]$CaseRoot = '',
    [AllowEmptyString()]
    [string]$ReportRoot = '',
    [string]$Skill = 'archive-move-ops'
)

if ([string]::IsNullOrWhiteSpace($CaseRoot)) {
    $CaseRoot = Join-Path $PSScriptRoot '..\cases'
}

if ([string]::IsNullOrWhiteSpace($ReportRoot)) {
    $ReportRoot = Join-Path $PSScriptRoot '..\reports'
}

$adapterPath = Join-Path $PSScriptRoot ("adapters\{0}.ps1" -f $Skill)

. (Join-Path $PSScriptRoot 'lib.ps1')
. (Join-Path $PSScriptRoot 'learning-sink.ps1')
. (Join-Path $PSScriptRoot 'judges\rule-judges.ps1')
. (Join-Path $PSScriptRoot 'reporters\fs-reporter.ps1')

function Get-HarnessAdapterResolverName {
    param(
        [Parameter(Mandatory)]
        [string]$SkillName
    )

    $segments = @($SkillName -split '[^A-Za-z0-9]+' | Where-Object { $_ -ne '' })
    if ($segments.Count -eq 0) {
        throw "Skill name '$SkillName' does not contain any resolver-safe characters."
    }

    $pascalName = ($segments | ForEach-Object {
        if ($_.Length -eq 1) {
            $_.ToUpperInvariant()
        } else {
            $_.Substring(0, 1).ToUpperInvariant() + $_.Substring(1)
        }
    }) -join ''

    return "Resolve-${pascalName}Invocation"
}

if (-not (Test-Path $adapterPath)) {
    throw "Harness adapter is not implemented yet. Expected adapter file at $adapterPath."
}

. $adapterPath

$adapterResolverName = Get-HarnessAdapterResolverName -SkillName $Skill
if (-not (Get-Command -Name $adapterResolverName -CommandType Function -ErrorAction SilentlyContinue)) {
    throw "Harness adapter resolver is not implemented yet. Expected function $adapterResolverName in $adapterPath."
}

$runId = Get-Date -Format 'yyyyMMdd-HHmmss'
$runTimer = [System.Diagnostics.Stopwatch]::StartNew()
$startedAt = (Get-Date).ToString('o')
$results = New-Object System.Collections.Generic.List[object]

foreach ($case in Get-HarnessCases -CaseRoot (Join-Path $CaseRoot $Skill)) {
    $invocation = & $adapterResolverName -CaseObject $case
    $execution = Invoke-HarnessCommand -FilePath $invocation.FilePath -ArgumentList $invocation.ArgumentList
    $judges = Invoke-HarnessJudges -CaseId $case.Id -ExecutionResult $execution -Judges $case.Expect.judges
    $verdict = if (($judges | Where-Object { -not $_.Passed }).Count -eq 0 -and $execution.ExitCode -eq 0) { 'passed' } else { 'failed' }
    $classification = if ($verdict -eq 'passed') {
        'ok'
    } elseif ($execution.ExitCode -ne 0) {
        'execution_failed'
    } else {
        'assertion_failed'
    }

    $results.Add([pscustomobject]@{
        CaseId = $case.Id
        Verdict = $verdict
        Classification = $classification
        Execution = $execution
        Judges = $judges
    })
}

$runTimer.Stop()

$learningRoot = Join-Path $PSScriptRoot '..\.learnings'
foreach ($result in $results) {
    Write-HarnessLearningEntry -LearningRoot $learningRoot -Result $result
}

$written = Write-HarnessReports -ReportRoot $ReportRoot -RunResult ([pscustomobject]@{
    RunId = $runId
    StartedAt = $startedAt
    DurationMs = [int]$runTimer.ElapsedMilliseconds
    Results = $results
})

Write-Host "Summary written to $($written.SummaryMarkdownPath)"

if (@($results | Where-Object { $_.Verdict -eq 'failed' }).Count -gt 0) {
    exit 1
}
