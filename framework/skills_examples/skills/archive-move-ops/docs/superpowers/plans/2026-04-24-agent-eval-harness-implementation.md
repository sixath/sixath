# Agent 评测 Harness 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `archive-move-ops` 落地一套可复用的本地 agent 评测 harness，支持 case 加载、脚本执行、规则判分、报告输出和 `.learnings/` 集成。

**Architecture:** 采用分层插件式结构，把通用能力拆成 loader、runner、adapter、judge、reporter、learning sink 六部分。第一版只跑本地 fixture，用 PowerShell 脚本和 JSON case 文件完成批量评测，并为半交互模式预留同一套结果模型。

**Tech Stack:** PowerShell 脚本、JSON case 文件、现有 `tests/*.test.ps1` 风格测试、Markdown/JSON 报告输出

---

## 文件结构

### 新增文件

- `D:\workspace\skills\archive-move-ops\harness\lib.ps1`
  负责通用装配函数，包括路径解析、case 读取、结果对象构建和公共工具函数。
- `D:\workspace\skills\archive-move-ops\harness\run-harness.ps1`
  负责命令行入口、case 过滤、批量执行、汇总退出码。
- `D:\workspace\skills\archive-move-ops\harness\adapters\archive-move-ops.ps1`
  负责把 case 类型映射到当前 skill 的脚本和参数。
- `D:\workspace\skills\archive-move-ops\harness\judges\rule-judges.ps1`
  负责规则判分。
- `D:\workspace\skills\archive-move-ops\harness\reporters\fs-reporter.ps1`
  负责写入 `reports/latest` 和 `reports/history`。
- `D:\workspace\skills\archive-move-ops\harness\learning-sink.ps1`
  负责把分类后的失败记录到 `.learnings/`。
- `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\parse-dispatch.basic-json.json`
- `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\followup.basic-route.json`
- `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\entry.traceid.basic.json`
- `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\report.traceid.basic.json`
- `D:\workspace\skills\archive-move-ops\tests\harness\load-cases.test.ps1`
- `D:\workspace\skills\archive-move-ops\tests\harness\runner.test.ps1`
- `D:\workspace\skills\archive-move-ops\tests\harness\judges.test.ps1`
- `D:\workspace\skills\archive-move-ops\tests\harness\reporter.test.ps1`
- `D:\workspace\skills\archive-move-ops\tests\harness\adapter.test.ps1`
- `D:\workspace\skills\archive-move-ops\tests\harness\learning-sink.test.ps1`
- `D:\workspace\skills\archive-move-ops\tests\fixtures\harness\cases\invalid-missing-type.json`
- `D:\workspace\skills\archive-move-ops\tests\fixtures\harness\cases\valid-minimal.json`

### 修改文件

- `D:\workspace\skills\archive-move-ops\tests\run-tests.ps1`
  把新的 harness 测试文件接入现有测试入口。

## 约束与约定

- 当前工作区不是 git 仓库，因此计划中的“提交”步骤统一替换为“checkpoint 记录”。
- 所有新增测试继续沿用当前仓库的脚本式测试风格，即脚本执行失败时直接 `throw`，成功时 `Write-Host PASS ...`。
- 所有运行命令统一使用 `powershell -NoProfile -ExecutionPolicy Bypass`，避免本机 profile 干扰。
- 第一版不接 SSH、不读取远端日志、不引入 Pester。

## 任务拆解

### 任务 1：搭起 case loader 骨架

**Files:**
- Create: `D:\workspace\skills\archive-move-ops\harness\lib.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\harness\load-cases.test.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\fixtures\harness\cases\valid-minimal.json`
- Create: `D:\workspace\skills\archive-move-ops\tests\fixtures\harness\cases\invalid-missing-type.json`
- Modify: `D:\workspace\skills\archive-move-ops\tests\run-tests.ps1`

- [ ] **步骤 1：先写 failing test，锁定 case 发现与 schema 校验**

```powershell
[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')

$fixtureRoot = Join-Path $PSScriptRoot '..\fixtures\harness\cases'
$cases = Get-HarnessCases -CaseRoot $fixtureRoot

if ($cases.Count -ne 1) {
    throw "Expected exactly 1 valid case, got $($cases.Count)."
}

if ($cases[0].Id -ne 'fixture.valid-minimal') {
    throw "Expected valid case id fixture.valid-minimal, got $($cases[0].Id)."
}

$failed = $false
try {
    Test-HarnessCaseSchema -CaseObject (Get-Content (Join-Path $fixtureRoot 'invalid-missing-type.json') -Raw | ConvertFrom-Json -Depth 10)
} catch {
    $failed = $_.Exception.Message -like '*type*'
}

if (-not $failed) {
    throw 'Expected schema validation to fail for missing type.'
}

Write-Host 'PASS load-cases.test.ps1'
```

- [ ] **步骤 2：运行测试，确认它因缺少 loader 实现而失败**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\load-cases.test.ps1
```

Expected:

```text
FAIL with "The term 'Get-HarnessCases' is not recognized"
```

- [ ] **步骤 3：写最小实现，支持读取 JSON、过滤非法 case、单独校验 schema**

```powershell
function Test-HarnessCaseSchema {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$CaseObject
    )

    $required = @('id', 'skill', 'type', 'description', 'input', 'expect')
    foreach ($field in $required) {
        if (-not $CaseObject.PSObject.Properties.Name.Contains($field)) {
            throw "Case schema missing required field: $field"
        }
    }

    if (-not $CaseObject.expect.PSObject.Properties.Name.Contains('judges')) {
        throw 'Case schema missing required field: expect.judges'
    }

    return $true
}

function Get-HarnessCases {
    param(
        [Parameter(Mandatory)]
        [string]$CaseRoot
    )

    $items = New-Object System.Collections.Generic.List[object]
    Get-ChildItem -Path $CaseRoot -Filter *.json -File | Sort-Object FullName | ForEach-Object {
        $caseObject = Get-Content $_.FullName -Raw | ConvertFrom-Json -Depth 20
        try {
            Test-HarnessCaseSchema -CaseObject $caseObject | Out-Null
            $items.Add([pscustomobject]@{
                Id = $caseObject.id
                Skill = $caseObject.skill
                Type = $caseObject.type
                Description = $caseObject.description
                Input = $caseObject.input
                Expect = $caseObject.expect
                CasePath = $_.FullName
            })
        } catch {
            if ($_.Exception.Message -like '*missing required field*') {
                return
            }
            throw
        }
    }

    return $items
}
```

- [ ] **步骤 4：补上 fixture 文件与测试入口接线**

```json
{
  "id": "fixture.valid-minimal",
  "skill": "archive-move-ops",
  "type": "parse_dispatch_log",
  "description": "minimal valid fixture",
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)"
  },
  "expect": {
    "judges": []
  }
}
```

```json
{
  "id": "fixture.invalid-missing-type",
  "skill": "archive-move-ops",
  "description": "invalid fixture",
  "input": {},
  "expect": {
    "judges": []
  }
}
```

```powershell
$testFiles = @(
    (Join-Path $PSScriptRoot 'build-entry-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-flow-investigation-template.test.ps1'),
    (Join-Path $PSScriptRoot 'build-followup-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-investigation-report.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\load-cases.test.ps1')
)
```

- [ ] **步骤 5：重新运行测试，确认 loader 通过**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\load-cases.test.ps1
```

Expected:

```text
PASS load-cases.test.ps1
```

- [ ] **步骤 6：做 checkpoint 记录**

记录内容：

```text
checkpoint: case loader skeleton ready
```

### 任务 2：实现 runner 和统一结果对象

**Files:**
- Modify: `D:\workspace\skills\archive-move-ops\harness\lib.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\harness\runner.test.ps1`

- [ ] **步骤 1：先写 failing test，锁定 runner 返回结构**

```powershell
[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')

$result = Invoke-HarnessCommand -FilePath (Join-Path $PSScriptRoot '..\..\scripts\parse-dispatch-log.ps1') -ArgumentList @(
    '-LogLine',
    'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)',
    '-AsJson'
)

$required = @('Command', 'ExitCode', 'StdOut', 'StdErr', 'DurationMs', 'Succeeded')
foreach ($name in $required) {
    if (-not $result.PSObject.Properties.Name.Contains($name)) {
        throw "Missing result property: $name"
    }
}

if (-not $result.Succeeded) {
    throw 'Expected parse-dispatch-log invocation to succeed.'
}

if ($result.ExitCode -ne 0) {
    throw "Expected exit code 0, got $($result.ExitCode)."
}

if ($result.StdOut -notmatch '301_rqkkw0snhnmt') {
    throw 'Expected stdout to include flow id.'
}

Write-Host 'PASS runner.test.ps1'
```

- [ ] **步骤 2：运行测试，确认它因为缺少 runner 而失败**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\runner.test.ps1
```

Expected:

```text
FAIL with "The term 'Invoke-HarnessCommand' is not recognized"
```

- [ ] **步骤 3：实现最小 runner，统一捕获 stdout、stderr、exit code 和耗时**

```powershell
function Invoke-HarnessCommand {
    param(
        [Parameter(Mandatory)]
        [string]$FilePath,
        [string[]]$ArgumentList = @()
    )

    $stdoutFile = [System.IO.Path]::GetTempFileName()
    $stderrFile = [System.IO.Path]::GetTempFileName()
    $timer = [System.Diagnostics.Stopwatch]::StartNew()

    try {
        $proc = Start-Process -FilePath 'powershell.exe' `
            -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $FilePath) + $ArgumentList `
            -RedirectStandardOutput $stdoutFile `
            -RedirectStandardError $stderrFile `
            -Wait `
            -PassThru `
            -NoNewWindow

        $timer.Stop()

        return [pscustomobject]@{
            Command = "powershell -NoProfile -ExecutionPolicy Bypass -File $FilePath $($ArgumentList -join ' ')"
            ExitCode = $proc.ExitCode
            StdOut = [System.IO.File]::ReadAllText($stdoutFile)
            StdErr = [System.IO.File]::ReadAllText($stderrFile)
            DurationMs = [int]$timer.ElapsedMilliseconds
            Succeeded = ($proc.ExitCode -eq 0)
        }
    } finally {
        Remove-Item $stdoutFile, $stderrFile -ErrorAction SilentlyContinue
    }
}
```

- [ ] **步骤 4：运行测试，确认 runner 通过**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\runner.test.ps1
```

Expected:

```text
PASS runner.test.ps1
```

- [ ] **步骤 5：做 checkpoint 记录**

记录内容：

```text
checkpoint: runner result model ready
```

### 任务 3：实现规则判分器

**Files:**
- Create: `D:\workspace\skills\archive-move-ops\harness\judges\rule-judges.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\harness\judges.test.ps1`
- Modify: `D:\workspace\skills\archive-move-ops\harness\lib.ps1`
- Modify: `D:\workspace\skills\archive-move-ops\tests\run-tests.ps1`

- [ ] **步骤 1：先写 failing test，覆盖文本 judge 和 JSON judge**

```powershell
[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')
. (Join-Path $PSScriptRoot '..\..\harness\judges\rule-judges.ps1')

$result = [pscustomobject]@{
    ExitCode = 0
    StdOut = '{"flow_id":"301_rqkkw0snhnmt","src_area_type":"400","dst_area_type":"301"}'
    StdErr = ''
}

$judges = @(
    [pscustomobject]@{ kind = 'exit_code'; equals = 0 },
    [pscustomobject]@{ kind = 'contains_text'; target = 'stdout'; value = '301_rqkkw0snhnmt' },
    [pscustomobject]@{ kind = 'json_field_equals'; field = 'dst_area_type'; equals = '301' }
)

$judgeResults = Invoke-HarnessJudges -CaseId 'fixture.judges' -ExecutionResult $result -Judges $judges

if ($judgeResults.Count -ne 3) {
    throw "Expected 3 judge results, got $($judgeResults.Count)."
}

if (($judgeResults | Where-Object { -not $_.Passed }).Count -ne 0) {
    throw 'Expected all judges to pass.'
}

Write-Host 'PASS judges.test.ps1'
```

- [ ] **步骤 2：运行测试，确认它因为缺少 judge 实现而失败**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\judges.test.ps1
```

Expected:

```text
FAIL with "The term 'Invoke-HarnessJudges' is not recognized"
```

- [ ] **步骤 3：实现最小 judge 集合**

```powershell
function Invoke-HarnessJudges {
    param(
        [Parameter(Mandatory)]
        [string]$CaseId,
        [Parameter(Mandatory)]
        [pscustomobject]$ExecutionResult,
        [Parameter(Mandatory)]
        [object[]]$Judges
    )

    $jsonCache = $null
    $results = New-Object System.Collections.Generic.List[object]

    foreach ($judge in $Judges) {
        $passed = $false
        $message = ''

        switch ($judge.kind) {
            'exit_code' {
                $passed = ($ExecutionResult.ExitCode -eq $judge.equals)
                $message = "expected exit code $($judge.equals), actual $($ExecutionResult.ExitCode)"
            }
            'contains_text' {
                $targetText = if ($judge.target -eq 'stderr') { $ExecutionResult.StdErr } else { $ExecutionResult.StdOut }
                $passed = $targetText -match [regex]::Escape($judge.value)
                $message = "expected $($judge.target) to contain $($judge.value)"
            }
            'json_field_equals' {
                if (-not $jsonCache) {
                    $jsonCache = $ExecutionResult.StdOut | ConvertFrom-Json -Depth 20
                }
                $actual = $jsonCache.$($judge.field)
                $passed = ($actual -eq $judge.equals)
                $message = "expected json field $($judge.field) to equal $($judge.equals), actual $actual"
            }
            default {
                throw "Unsupported judge kind: $($judge.kind)"
            }
        }

        $results.Add([pscustomobject]@{
            CaseId = $CaseId
            Kind = $judge.kind
            Passed = $passed
            Message = $message
        })
    }

    return $results
}
```

- [ ] **步骤 4：把新测试接到测试入口**

```powershell
$testFiles = @(
    (Join-Path $PSScriptRoot 'build-entry-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-flow-investigation-template.test.ps1'),
    (Join-Path $PSScriptRoot 'build-followup-commands.test.ps1'),
    (Join-Path $PSScriptRoot 'build-investigation-report.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\load-cases.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\runner.test.ps1'),
    (Join-Path $PSScriptRoot 'harness\judges.test.ps1')
)
```

- [ ] **步骤 5：运行测试，确认 judge 通过**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\judges.test.ps1
```

Expected:

```text
PASS judges.test.ps1
```

- [ ] **步骤 6：做 checkpoint 记录**

记录内容：

```text
checkpoint: core rule judges ready
```

### 任务 4：实现 reporter 和批量入口

**Files:**
- Create: `D:\workspace\skills\archive-move-ops\harness\reporters\fs-reporter.ps1`
- Modify: `D:\workspace\skills\archive-move-ops\harness\run-harness.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\harness\reporter.test.ps1`
- Modify: `D:\workspace\skills\archive-move-ops\tests\run-tests.ps1`

- [ ] **步骤 1：先写 failing test，锁定 summary 报告文件和详情文件**

```powershell
[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\reporters\fs-reporter.ps1')

$reportRoot = Join-Path $env:TEMP 'archive-move-ops-harness-report-test'
if (Test-Path $reportRoot) {
    Remove-Item $reportRoot -Recurse -Force
}

$run = [pscustomobject]@{
    RunId = '20260424-120000'
    StartedAt = '2026-04-24T12:00:00Z'
    Results = @(
        [pscustomobject]@{
            CaseId = 'fixture.pass'
            Verdict = 'passed'
            Classification = 'ok'
            Execution = [pscustomobject]@{ ExitCode = 0; StdOut = 'hello'; StdErr = '' }
            Judges = @()
        }
    )
}

$written = Write-HarnessReports -ReportRoot $reportRoot -RunResult $run

if (-not (Test-Path $written.SummaryPath)) {
    throw "Expected summary file at $($written.SummaryPath)"
}

if (-not (Test-Path $written.DetailPaths[0])) {
    throw "Expected detail file at $($written.DetailPaths[0])"
}

Write-Host 'PASS reporter.test.ps1'
```

- [ ] **步骤 2：运行测试，确认它因为缺少 reporter 而失败**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\reporter.test.ps1
```

Expected:

```text
FAIL with "The term 'Write-HarnessReports' is not recognized"
```

- [ ] **步骤 3：实现最小 reporter，写 summary.json、summary.md 和单 case detail.json**

```powershell
function Write-HarnessReports {
    param(
        [Parameter(Mandatory)]
        [string]$ReportRoot,
        [Parameter(Mandatory)]
        [pscustomobject]$RunResult
    )

    $latestRoot = Join-Path $ReportRoot 'latest'
    $historyRoot = Join-Path (Join-Path $ReportRoot 'history') $RunResult.RunId
    New-Item -ItemType Directory -Force -Path $latestRoot, $historyRoot | Out-Null

    $summary = [pscustomobject]@{
        run_id = $RunResult.RunId
        started_at = $RunResult.StartedAt
        total = $RunResult.Results.Count
        passed = ($RunResult.Results | Where-Object Verdict -eq 'passed').Count
        failed = ($RunResult.Results | Where-Object Verdict -eq 'failed').Count
        skipped = ($RunResult.Results | Where-Object Verdict -eq 'skipped').Count
    }

    $summaryJsonPath = Join-Path $historyRoot 'summary.json'
    $summaryMdPath = Join-Path $historyRoot 'summary.md'
    $detailPaths = New-Object System.Collections.Generic.List[string]

    $summary | ConvertTo-Json -Depth 10 | Set-Content -Path $summaryJsonPath -Encoding utf8
    @(
        "# Harness Summary"
        ""
        "- run_id: $($summary.run_id)"
        "- total: $($summary.total)"
        "- passed: $($summary.passed)"
        "- failed: $($summary.failed)"
        "- skipped: $($summary.skipped)"
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
```

- [ ] **步骤 4：实现 `run-harness.ps1` 的最小批量入口**

```powershell
[CmdletBinding()]
param(
    [string]$CaseRoot = (Join-Path $PSScriptRoot '..\cases'),
    [string]$ReportRoot = (Join-Path $PSScriptRoot '..\reports'),
    [string]$Skill = 'archive-move-ops'
)

. (Join-Path $PSScriptRoot 'lib.ps1')
. (Join-Path $PSScriptRoot 'adapters\archive-move-ops.ps1')
. (Join-Path $PSScriptRoot 'judges\rule-judges.ps1')
. (Join-Path $PSScriptRoot 'reporters\fs-reporter.ps1')

$runId = Get-Date -Format 'yyyyMMdd-HHmmss'
$startedAt = (Get-Date).ToString('o')
$results = New-Object System.Collections.Generic.List[object]

foreach ($case in Get-HarnessCases -CaseRoot (Join-Path $CaseRoot $Skill)) {
    $invocation = Resolve-ArchiveMoveOpsInvocation -CaseObject $case
    $execution = Invoke-HarnessCommand -FilePath $invocation.FilePath -ArgumentList $invocation.ArgumentList
    $judges = Invoke-HarnessJudges -CaseId $case.Id -ExecutionResult $execution -Judges $case.Expect.judges
    $verdict = if (($judges | Where-Object { -not $_.Passed }).Count -eq 0 -and $execution.ExitCode -eq 0) { 'passed' } else { 'failed' }

    $results.Add([pscustomobject]@{
        CaseId = $case.Id
        Verdict = $verdict
        Classification = if ($verdict -eq 'passed') { 'ok' } else { 'assertion_failed' }
        Execution = $execution
        Judges = $judges
    })
}

$written = Write-HarnessReports -ReportRoot $ReportRoot -RunResult ([pscustomobject]@{
    RunId = $runId
    StartedAt = $startedAt
    Results = $results
})

Write-Host "Summary written to $($written.SummaryMarkdownPath)"

if (($results | Where-Object Verdict -eq 'failed').Count -gt 0) {
    exit 1
}
```

- [ ] **步骤 5：运行 reporter 测试，确认报告文件能落盘**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\reporter.test.ps1
```

Expected:

```text
PASS reporter.test.ps1
```

- [ ] **步骤 6：做 checkpoint 记录**

记录内容：

```text
checkpoint: reporting and batch entry ready
```

### 任务 5：接入 `archive-move-ops` adapter 与真实 case

**Files:**
- Create: `D:\workspace\skills\archive-move-ops\harness\adapters\archive-move-ops.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\harness\adapter.test.ps1`
- Create: `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\parse-dispatch.basic-json.json`
- Create: `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\followup.basic-route.json`
- Create: `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\entry.traceid.basic.json`
- Create: `D:\workspace\skills\archive-move-ops\cases\archive-move-ops\report.traceid.basic.json`
- Modify: `D:\workspace\skills\archive-move-ops\tests\run-tests.ps1`

- [ ] **步骤 1：先写 failing test，锁定 case 类型到脚本的映射**

```powershell
[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\adapters\archive-move-ops.ps1')

$case = [pscustomobject]@{
    Id = 'archive.parse-dispatch.basic-json'
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

Write-Host 'PASS adapter.test.ps1'
```

- [ ] **步骤 2：运行测试，确认它因为缺少 adapter 而失败**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\adapter.test.ps1
```

Expected:

```text
FAIL with "The term 'Resolve-ArchiveMoveOpsInvocation' is not recognized"
```

- [ ] **步骤 3：实现最小 adapter**

```powershell
function Resolve-ArchiveMoveOpsInvocation {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$CaseObject
    )

    $root = Join-Path $PSScriptRoot '..\..\scripts'
    switch ($CaseObject.Type) {
        'parse_dispatch_log' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'parse-dispatch-log.ps1')
                ArgumentList = @('-LogLine', $CaseObject.Input.logLine, '-AsJson')
            }
        }
        'build_followup_commands' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'build-followup-commands.ps1')
                ArgumentList = @('-LogLine', $CaseObject.Input.logLine)
            }
        }
        'build_entry_commands' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'build-entry-commands.ps1')
                ArgumentList = @('-TraceId', $CaseObject.Input.traceId)
            }
        }
        'build_investigation_report' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'build-investigation-report.ps1')
                ArgumentList = @('-TraceId', $CaseObject.Input.traceId, '-DispatchLogFile', $CaseObject.Input.dispatchLogFile)
            }
        }
        default {
            throw "Unsupported archive-move-ops case type: $($CaseObject.Type)"
        }
    }
}
```

- [ ] **步骤 4：写四个真实 case 文件**

```json
{
  "id": "archive.parse-dispatch.basic-json",
  "skill": "archive-move-ops",
  "type": "parse_dispatch_log",
  "description": "将 dispatch 日志解析为 JSON 字段",
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "301_rqkkw0snhnmt" },
      { "kind": "json_field_equals", "field": "dst_area_type", "equals": "301" }
    ]
  }
}
```

```json
{
  "id": "archive.followup.basic-route",
  "skill": "archive-move-ops",
  "type": "build_followup_commands",
  "description": "根据 dispatch 行构建 follow-up 命令",
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "# route: 400 -> 301" },
      { "kind": "contains_text", "target": "stdout", "value": "archiver-manager.log" }
    ]
  }
}
```

```json
{
  "id": "archive.entry.traceid.basic",
  "skill": "archive-move-ops",
  "type": "build_entry_commands",
  "description": "根据 traceId 生成 dispatch 搜索命令",
  "input": {
    "traceId": "trace-demo-123"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "union-archiver-dispatch" },
      { "kind": "contains_text", "target": "stdout", "value": "trace-demo-123" }
    ]
  }
}
```

```json
{
  "id": "archive.report.traceid.basic",
  "skill": "archive-move-ops",
  "type": "build_investigation_report",
  "description": "根据 traceId 和 fixture 生成 investigation report",
  "input": {
    "traceId": "trace-demo-123",
    "dispatchLogFile": "D:\\workspace\\skills\\archive-move-ops\\tests\\fixtures\\dispatch-log-sample.txt"
  },
  "expect": {
    "judges": [
      { "kind": "exit_code", "equals": 0 },
      { "kind": "contains_text", "target": "stdout", "value": "Dispatch hit:" },
      { "kind": "contains_text", "target": "stdout", "value": "Conclusion:" }
    ]
  }
}
```

- [ ] **步骤 5：运行 adapter 测试并做一次端到端冒烟**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\adapter.test.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\harness\run-harness.ps1
```

Expected:

```text
PASS adapter.test.ps1
Summary written to ...\reports\history\<run-id>\summary.md
```

- [ ] **步骤 6：做 checkpoint 记录**

记录内容：

```text
checkpoint: archive-move-ops adapter and green cases ready
```

### 任务 6：接入 learning sink 与失败分类

**Files:**
- Create: `D:\workspace\skills\archive-move-ops\harness\learning-sink.ps1`
- Create: `D:\workspace\skills\archive-move-ops\tests\harness\learning-sink.test.ps1`
- Modify: `D:\workspace\skills\archive-move-ops\harness\run-harness.ps1`
- Modify: `D:\workspace\skills\archive-move-ops\tests\run-tests.ps1`

- [ ] **步骤 1：先写 failing test，锁定 classified failure 会被追加到 `.learnings/ERRORS.md`**

```powershell
[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\learning-sink.ps1')

$learningRoot = Join-Path $env:TEMP 'archive-move-ops-learning-test'
if (Test-Path $learningRoot) {
    Remove-Item $learningRoot -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $learningRoot | Out-Null
Set-Content -Path (Join-Path $learningRoot 'ERRORS.md') -Value "# Errors`n" -Encoding utf8

Write-HarnessLearningEntry -LearningRoot $learningRoot -Result ([pscustomobject]@{
    CaseId = 'archive.followup.basic-route'
    Verdict = 'failed'
    Classification = 'assertion_failed'
    Execution = [pscustomobject]@{ ExitCode = 0; StdErr = ''; StdOut = 'missing route' }
})

$content = Get-Content -Path (Join-Path $learningRoot 'ERRORS.md') -Raw
if ($content -notmatch 'archive.followup.basic-route') {
    throw 'Expected errors log to include failing case id.'
}

Write-Host 'PASS learning-sink.test.ps1'
```

- [ ] **步骤 2：运行测试，确认它因为缺少 learning sink 而失败**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\harness\learning-sink.test.ps1
```

Expected:

```text
FAIL with "The term 'Write-HarnessLearningEntry' is not recognized"
```

- [ ] **步骤 3：实现最小 learning sink，只记录分类明确的失败**

```powershell
function Write-HarnessLearningEntry {
    param(
        [Parameter(Mandatory)]
        [string]$LearningRoot,
        [Parameter(Mandatory)]
        [pscustomobject]$Result
    )

    if ($Result.Verdict -ne 'failed') {
        return
    }

    if ($Result.Classification -notin @('assertion_failed', 'execution_failed')) {
        return
    }

    $errorsPath = Join-Path $LearningRoot 'ERRORS.md'
    $timestamp = (Get-Date).ToString('o')
    @(
        ""
        "## [ERR-$((Get-Date).ToString('yyyyMMdd-HHmmss'))] harness.$($Result.Classification)"
        ""
        "**Logged**: $timestamp"
        "**Priority**: medium"
        "**Status**: pending"
        "**Area**: tests"
        ""
        "### Summary"
        "Harness case failed: $($Result.CaseId)"
        ""
        "### Context"
        "- Classification: $($Result.Classification)"
        "- ExitCode: $($Result.Execution.ExitCode)"
        ""
        "---"
    ) | Add-Content -Path $errorsPath -Encoding utf8
}
```

- [ ] **步骤 4：把 learning sink 接入批量入口**

```powershell
. (Join-Path $PSScriptRoot 'learning-sink.ps1')

$learningRoot = Join-Path $PSScriptRoot '..\.learnings'
foreach ($result in $results) {
    Write-HarnessLearningEntry -LearningRoot $learningRoot -Result $result
}
```

- [ ] **步骤 5：跑全量测试和全量 harness，确认第一版闭环成立**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\run-tests.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\harness\run-harness.ps1
```

Expected:

```text
PASS build-entry-commands.test.ps1
PASS build-flow-investigation-template.test.ps1
PASS build-followup-commands.test.ps1
PASS build-investigation-report.test.ps1
PASS load-cases.test.ps1
PASS runner.test.ps1
PASS judges.test.ps1
PASS reporter.test.ps1
PASS adapter.test.ps1
PASS learning-sink.test.ps1
Summary written to ...\reports\history\<run-id>\summary.md
```

- [ ] **步骤 6：做最终 checkpoint 记录**

记录内容：

```text
checkpoint: v1 local eval harness complete
```

## 自检

### Spec 覆盖检查

- case schema：任务 1
- runner 结果对象：任务 2
- 规则 judge：任务 3
- report 输出：任务 4
- `archive-move-ops` adapter 和示例 case：任务 5
- `.learnings/` 集成：任务 6

### 占位符检查

- 计划内没有使用未完成占位词或“稍后实现”类表述
- 每个任务都给出了明确文件路径
- 每个测试步骤都给出了实际运行命令

### 类型与命名一致性

- loader 使用 `Get-HarnessCases` / `Test-HarnessCaseSchema`
- runner 使用 `Invoke-HarnessCommand`
- judge 使用 `Invoke-HarnessJudges`
- adapter 使用 `Resolve-ArchiveMoveOpsInvocation`
- reporter 使用 `Write-HarnessReports`
- learning sink 使用 `Write-HarnessLearningEntry`
