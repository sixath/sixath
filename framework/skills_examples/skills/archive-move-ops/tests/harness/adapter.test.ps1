[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')
. (Join-Path $PSScriptRoot '..\..\harness\adapters\archive-move-ops.ps1')

$case = [pscustomobject]@{
    Id = 'archive.parse-dispatch.basic-json'
    CasePath = 'D:\workspace\skills\archive-move-ops\cases\archive-move-ops\parse-dispatch.basic-json.json'
    Type = 'parse_dispatch_log'
    Input = [pscustomobject]@{
        logLine = 'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)'
    }
}

$resolved = Resolve-ArchiveMoveOpsInvocation -CaseObject $case

if ($resolved.FilePath -notlike '*parse-dispatch-log.ps1') {
    throw "Expected parse-dispatch-log.ps1, got $($resolved.FilePath)"
}

if (($resolved.ArgumentList -join ' ') -notmatch '-AsJson') {
    throw 'Expected parse_dispatch_log cases to request -AsJson.'
}

$reportCase = [pscustomobject]@{
    Id = 'archive.report.traceid.basic'
    CasePath = 'D:\workspace\skills\archive-move-ops\cases\archive-move-ops\report.traceid.basic.json'
    Type = 'build_investigation_report'
    Input = [pscustomobject]@{
        traceId = 'trace-demo-123'
        dispatchLogFile = '..\..\tests\fixtures\dispatch-log-sample.txt'
    }
}

$resolvedReport = Resolve-ArchiveMoveOpsInvocation -CaseObject $reportCase
$dispatchLogIndex = [array]::IndexOf($resolvedReport.ArgumentList, '-DispatchLogFile')

if ($dispatchLogIndex -lt 0) {
    throw 'Expected build_investigation_report cases to include -DispatchLogFile.'
}

$resolvedDispatchLogPath = $resolvedReport.ArgumentList[$dispatchLogIndex + 1]
$expectedDispatchLogPath = 'D:\workspace\skills\archive-move-ops\tests\fixtures\dispatch-log-sample.txt'

if ($resolvedDispatchLogPath -ne $expectedDispatchLogPath) {
    throw "Expected relative dispatchLogFile to resolve against CasePath. Expected: $expectedDispatchLogPath`nActual: $resolvedDispatchLogPath"
}

$harnessRoot = Join-Path $PSScriptRoot '..\..\harness'
$runHarnessPath = Join-Path $harnessRoot 'run-harness.ps1'
$testSkill = 'test-skill'
$testAdapterPath = Join-Path (Join-Path $harnessRoot 'adapters') "$testSkill.ps1"
$tempScriptPath = [System.IO.Path]::GetTempFileName() + '.ps1'
$tempCaseRoot = Join-Path $env:TEMP ("archive-move-ops-harness-adapter-case-{0}" -f ([guid]::NewGuid().ToString('N')))
$tempReportRoot = Join-Path $env:TEMP ("archive-move-ops-harness-adapter-report-{0}" -f ([guid]::NewGuid().ToString('N')))
$tempSkillCaseRoot = Join-Path $tempCaseRoot $testSkill

try {
    Set-Content -Path $tempScriptPath -Encoding utf8 -Value @'
param([string]$Message)
Write-Output $Message
'@

    New-Item -ItemType Directory -Path $tempSkillCaseRoot -Force | Out-Null

    $adapterContent = @'
function Resolve-TestSkillInvocation {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$CaseObject
    )

    return [pscustomobject]@{
        FilePath = '__SCRIPT_PATH__'
        ArgumentList = @('-Message', $CaseObject.Input.message)
    }
}
'@.Replace('__SCRIPT_PATH__', $tempScriptPath)

    Set-Content -Path $testAdapterPath -Encoding utf8 -Value $adapterContent

    Set-Content -Path (Join-Path $tempSkillCaseRoot 'dynamic-dispatch.json') -Encoding utf8 -Value @'
{
  "id": "test.dynamic-dispatch",
  "skill": "test-skill",
  "type": "emit_message",
  "description": "Validates dynamic adapter dispatch for -Skill.",
  "input": {
    "message": "dynamic adapter dispatch works"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "dynamic adapter dispatch works" }
    ]
  }
}
'@

    $runResult = Invoke-HarnessCommand -FilePath $runHarnessPath -ArgumentList @(
        '-CaseRoot', $tempCaseRoot,
        '-ReportRoot', $tempReportRoot,
        '-Skill', $testSkill
    )

    if ($runResult.ExitCode -ne 0) {
        throw "Expected dynamic -Skill harness run to succeed, got exit code $($runResult.ExitCode) with stderr: $($runResult.StdErr)"
    }

    if ($runResult.StdOut -notmatch 'Summary written to') {
        throw "Expected dynamic -Skill harness run to print summary path, got stdout: $($runResult.StdOut)"
    }

    $latestSummaryPath = Join-Path (Join-Path $tempReportRoot 'latest') 'summary.json'
    if (-not (Test-Path -LiteralPath $latestSummaryPath)) {
        throw "Expected dynamic -Skill harness run to write latest summary: $latestSummaryPath"
    }

    $latestSummary = Get-Content -Path $latestSummaryPath -Raw | ConvertFrom-Json
    if (@($latestSummary.failed_case_ids).Count -ne 0) {
        throw "Expected dynamic -Skill harness run to have no failing ids, got: $($latestSummary.failed_case_ids -join ', ')"
    }
} finally {
    foreach ($path in @($testAdapterPath, $tempScriptPath)) {
        if (Test-Path -LiteralPath $path) {
            Remove-Item -LiteralPath $path -Force
        }
    }

    foreach ($path in @($tempCaseRoot, $tempReportRoot)) {
        if (Test-Path -LiteralPath $path) {
            Remove-Item -LiteralPath $path -Recurse -Force
        }
    }
}

Write-Host 'PASS adapter.test.ps1'
