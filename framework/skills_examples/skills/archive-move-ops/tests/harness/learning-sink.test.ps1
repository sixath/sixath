[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\learning-sink.ps1')
. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')

function New-TestResult {
    param(
        [Parameter(Mandatory)]
        [string]$CaseId,
        [Parameter(Mandatory)]
        [string]$Verdict,
        [Parameter(Mandatory)]
        [string]$Classification,
        [int]$ExitCode = 0,
        [string]$StdOut = '',
        [string]$StdErr = ''
    )

    return [pscustomobject]@{
        CaseId = $CaseId
        Verdict = $Verdict
        Classification = $Classification
        Execution = [pscustomobject]@{
            ExitCode = $ExitCode
            StdErr = $StdErr
            StdOut = $StdOut
        }
    }
}

function Get-ErrorsContent {
    param(
        [Parameter(Mandatory)]
        [string]$Path
    )

    if (-not (Test-Path $Path)) {
        return ''
    }

    return (Get-Content -Path $Path -Raw)
}

function Get-FileLength {
    param(
        [Parameter(Mandatory)]
        [string]$Path
    )

    if (-not (Test-Path $Path)) {
        return 0
    }

    return (Get-Item -LiteralPath $Path).Length
}

$learningRoot = Join-Path $env:TEMP 'archive-move-ops-learning-test'
if (Test-Path $learningRoot) {
    Remove-Item $learningRoot -Recurse -Force
}

New-Item -ItemType Directory -Force -Path $learningRoot | Out-Null
$errorsPath = Join-Path $learningRoot 'ERRORS.md'
Set-Content -Path $errorsPath -Value "# Errors`n" -Encoding utf8
$baselineContent = Get-ErrorsContent -Path $errorsPath

Write-HarnessLearningEntry -LearningRoot $learningRoot -Result (New-TestResult -CaseId 'archive.followup.basic-route' -Verdict 'failed' -Classification 'assertion_failed' -StdOut 'missing route')

$content = Get-ErrorsContent -Path $errorsPath
if ($content -notmatch 'archive.followup.basic-route') {
    throw 'Expected errors log to include failing case id.'
}

if ($content -notmatch 'Classification: assertion_failed') {
    throw 'Expected errors log to include classification.'
}

if ($content -notmatch 'ExitCode: 0') {
    throw 'Expected errors log to include exit code.'
}

if ($content -match '\[ERR-' -or $content -match 'Priority' -or $content -match 'Status' -or $content -match 'Area') {
    throw 'Expected learning sink to append a concise entry without incident workflow metadata.'
}

Write-HarnessLearningEntry -LearningRoot $learningRoot -Result (New-TestResult -CaseId 'archive.followup.passed' -Verdict 'passed' -Classification 'ok')
$afterPassedContent = Get-ErrorsContent -Path $errorsPath
if ($afterPassedContent -ne $content) {
    throw 'Expected passed verdicts to produce no learning entry.'
}

Write-HarnessLearningEntry -LearningRoot $learningRoot -Result (New-TestResult -CaseId 'archive.followup.ignored' -Verdict 'failed' -Classification 'skipped')
$afterIgnoredClassificationContent = Get-ErrorsContent -Path $errorsPath
if ($afterIgnoredClassificationContent -ne $content) {
    throw 'Expected unsupported classifications to produce no learning entry.'
}

$tempRoot = Join-Path $env:TEMP 'archive-move-ops-learning-harness-test'
if (Test-Path $tempRoot) {
    Remove-Item $tempRoot -Recurse -Force
}

New-Item -ItemType Directory -Force -Path $tempRoot | Out-Null
$isolatedRoot = Join-Path $tempRoot 'workspace'
$caseRoot = Join-Path $tempRoot 'cases'
$reportRoot = Join-Path $tempRoot 'reports'
$skillCaseRoot = Join-Path $caseRoot 'archive-move-ops'
New-Item -ItemType Directory -Force -Path $isolatedRoot | Out-Null
New-Item -ItemType Directory -Force -Path $skillCaseRoot | Out-Null
New-Item -ItemType Directory -Force -Path $reportRoot | Out-Null

$sourceRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Copy-Item -LiteralPath (Join-Path $sourceRoot 'harness') -Destination $isolatedRoot -Recurse -Force
Copy-Item -LiteralPath (Join-Path $sourceRoot 'scripts') -Destination $isolatedRoot -Recurse -Force
Copy-Item -LiteralPath (Join-Path $sourceRoot 'references') -Destination $isolatedRoot -Recurse -Force

$runHarnessPath = Join-Path $isolatedRoot 'harness\run-harness.ps1'
$isolatedErrorsPath = Join-Path $isolatedRoot '.learnings\ERRORS.md'

$caseRunId = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$failingCaseId = "archive.learning-sink.e2e-failure.$caseRunId"
$passingCaseId = "archive.learning-sink.e2e-pass.$caseRunId"

if ((Test-Path $isolatedErrorsPath) -and (Select-String -Path $isolatedErrorsPath -Pattern ([regex]::Escape($failingCaseId)) -Quiet)) {
    throw 'Expected failing case id to be unique before the harness failure-path run.'
}

if ((Test-Path $isolatedErrorsPath) -and (Select-String -Path $isolatedErrorsPath -Pattern ([regex]::Escape($passingCaseId)) -Quiet)) {
    throw 'Expected passing case id to be unique before the harness pass-path run.'
}

Set-Content -Path (Join-Path $skillCaseRoot 'failing-case.json') -Encoding utf8 -Value @"
{
  "id": "$failingCaseId",
  "skill": "archive-move-ops",
  "type": "build_followup_commands",
  "description": "Intentional harness failure for learning sink verification.",
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "__never_matches__" }
    ]
  }
}
"@

Set-Content -Path (Join-Path $skillCaseRoot 'passing-case.json') -Encoding utf8 -Value @"
{
  "id": "$passingCaseId",
  "skill": "archive-move-ops",
  "type": "build_followup_commands",
  "description": "Passing harness case for learning sink verification.",
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "# route: 400 -> 301" }
    ]
  }
}
"@

$failingRun = Invoke-HarnessCommand -FilePath $runHarnessPath -ArgumentList @(
    '-CaseRoot', $caseRoot,
    '-ReportRoot', $reportRoot
)

if ($failingRun.ExitCode -eq 0) {
    throw 'Expected failing harness run to return a non-zero exit code.'
}

if (-not (Select-String -Path $isolatedErrorsPath -Pattern ([regex]::Escape($failingCaseId)) -Quiet)) {
    throw 'Expected failing harness run to record the failing case id.'
}

if (-not (Select-String -Path $isolatedErrorsPath -Pattern 'Classification: assertion_failed' -Quiet)) {
    throw 'Expected failing harness run to record assertion_failed classification.'
}

Remove-Item -LiteralPath (Join-Path $skillCaseRoot 'failing-case.json') -Force

$passingRun = Invoke-HarnessCommand -FilePath $runHarnessPath -ArgumentList @(
    '-CaseRoot', $caseRoot,
    '-ReportRoot', $reportRoot
)

if ($passingRun.ExitCode -ne 0) {
    throw "Expected passing harness run to return exit code 0, got $($passingRun.ExitCode)."
}

if (Select-String -Path $isolatedErrorsPath -Pattern ([regex]::Escape($passingCaseId)) -Quiet -ErrorAction SilentlyContinue) {
    throw 'Expected passing harness run to produce no learning entry.'
}

Write-Host 'PASS learning-sink.test.ps1'
