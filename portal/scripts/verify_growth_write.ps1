# Growth 技能写盘最小闭环验收（Windows PowerShell）
#
# 前置：
#   1. MySQL 已建库 sath，且已执行 migrations 001–003
#   2. 后端已用 portal/configs 启动（growth.llm_review_enabled=true）
#   3. 至少有一个 chat_sessions 行（随便聊一句即可）；Agent.workspace 建议指向 DemoWorkspace
#
# 用法：
#   # 终端 1：启动后端
#   cd portal; go run ./cmd/backend/... -conf ./configs
#
#   # 终端 2：验收（自动取最近会话、置 pending、轮询 metrics、检查 SKILL.md）
#   .\scripts\verify_growth_write.ps1
#
#   .\scripts\verify_growth_write.ps1 -SessionId "<uuid>" -Workspace "D:\path\to\workspace"
#   .\scripts\verify_growth_write.ps1 -SetPendingOnly   # 仅 SQL 置 pending，不轮询
#
# 若 config 中 session_end_skill_review_enabled: true，也可不发 SQL：
#   与 Agent 对话（≥1 次工具成功 + 1 轮 assistant 回复）后 worker 会自动 pending。

param(
    [string]$SessionId = "",
    [string]$Workspace = "",
    [string]$ApiBase = "http://localhost:8000",
    [string]$ConfigPath = "",
    [string]$Dsn = "",
    [int]$MaxWaitSec = 90,
    [switch]$SetPendingOnly,
    [switch]$NoClean
)

$ErrorActionPreference = "Stop"

function Write-Step([string]$msg) {
    Write-Host "`n==> $msg" -ForegroundColor Cyan
}

function Write-Ok([string]$msg) {
    Write-Host "[OK] $msg" -ForegroundColor Green
}

function Write-Warn([string]$msg) {
    Write-Host "[WARN] $msg" -ForegroundColor Yellow
}

function Write-Fail([string]$msg) {
    Write-Host "[FAIL] $msg" -ForegroundColor Red
}

function Get-RepoRoot {
    $portal = Split-Path $PSScriptRoot -Parent
    Split-Path $portal -Parent
}

function Parse-GoMySqlDsn([string]$source) {
    # root:root@tcp(localhost:3306)/sath?parseTime=True&loc=Local&charset=utf8mb4
    if ($source -notmatch '^([^:]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^?]+)') {
        throw "无法解析 data.database.source: $source"
    }
    @{
        User = $Matches[1]
        Pass = $Matches[2]
        Host = $Matches[3]
        Port = $Matches[4]
        Db   = $Matches[5]
    }
}

function Invoke-MySqlQuery([hashtable]$db, [string]$sql) {
    $mysql = Get-Command mysql -ErrorAction SilentlyContinue
    if (-not $mysql) {
        throw "未找到 mysql 客户端。请安装 MySQL 客户端或将 mysql 加入 PATH。"
    }
    $args = @(
        "-h$($db.Host)", "-P$($db.Port)", "-u$($db.User)", "-p$($db.Pass)",
        "-N", "-B", $db.Db, "-e", $sql
    )
    $out = & mysql @args 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "mysql 执行失败: $out"
    }
    return ($out | Where-Object { $_ -ne $null -and "$_".Trim() -ne "" })
}

function Get-DatabaseSourceFromConfig([string]$path) {
    if (-not (Test-Path $path)) {
        throw "配置文件不存在: $path"
    }
    $lines = Get-Content $path -Raw
    if ($lines -match '(?m)^\s*source:\s*(.+)\s*$') {
        return $Matches[1].Trim()
    }
    throw "config.yaml 中未找到 data.database.source"
}

function Get-GrowthMetrics([string]$base) {
    $uri = "$base/api/v1/growth/metrics"
    try {
        return Invoke-RestMethod -Uri $uri -Method Get -TimeoutSec 5
    } catch {
        throw "无法访问 $uri （后端是否已启动？）: $_"
    }
}

$repoRoot = Get-RepoRoot
$portalRoot = Join-Path $repoRoot "portal"
if (-not $ConfigPath) {
    $ConfigPath = Join-Path $portalRoot "configs\config.yaml"
}

$demoWorkspace = Join-Path $repoRoot "data\workspaces\demo-growth"
$skillRel = "skills\example\SKILL.md"

Write-Step "Growth 写盘验收"
Write-Host "Repo: $repoRoot"

# 1. 准备 demo workspace 目录
if (-not $Workspace) {
    $Workspace = $demoWorkspace
}
$Workspace = [System.IO.Path]::GetFullPath($Workspace)
$skillsDir = Join-Path $Workspace "skills"
if (-not (Test-Path $skillsDir)) {
    New-Item -ItemType Directory -Force -Path $skillsDir | Out-Null
    Write-Ok "已创建 $skillsDir"
} else {
    Write-Ok "workspace 目录已存在: $Workspace"
}

# 2. 解析 DSN
if (-not $Dsn) {
    $Dsn = Get-DatabaseSourceFromConfig $ConfigPath
}
$db = Parse-GoMySqlDsn $Dsn
Write-Ok "MySQL $($db.Host):$($db.Port)/$($db.Db)"

# 3. 解析 session_id 与 agent workspace
Write-Step "解析会话"
if (-not $SessionId) {
    $rows = Invoke-MySqlQuery $db @"
SELECT cs.id, a.workspace
FROM chat_sessions cs
JOIN agents a ON a.id = cs.agent_id
ORDER BY cs.updated_at DESC
LIMIT 1;
"@
    if (-not $rows -or $rows.Count -eq 0) {
        Write-Fail "没有 chat_sessions。请先创建 Agent（workspace=$Workspace）并发送一条消息。"
        Write-Host @"

建议：
  1. 创建 Agent，workspace 设为：
     $Workspace
  2. 新建会话并发送任意消息
  3. 重新运行本脚本

"@ -ForegroundColor Yellow
        exit 2
    }
    $parts = "$($rows[0])" -split "`t"
    $SessionId = $parts[0]
    if ($parts.Count -ge 2 -and $parts[1]) {
        $dbWorkspace = $parts[1].Trim()
        if ($dbWorkspace -and (Test-Path -LiteralPath $dbWorkspace)) {
            if ($dbWorkspace -ne $Workspace) {
                Write-Warn "DB 中 Agent.workspace=$dbWorkspace ，与默认 demo 路径不同；将按 DB workspace 检查写盘结果。"
                $Workspace = [System.IO.Path]::GetFullPath($dbWorkspace)
            }
        } else {
            Write-Warn "DB workspace 路径不存在或未配置: '$dbWorkspace'。请把 Agent.workspace 设为: $demoWorkspace"
        }
    }
}
Write-Ok "session_id=$SessionId"
Write-Ok "检查写盘路径: $Workspace\$skillRel"

# 4. 确保 growth 状态行并置 pending
Write-Step "置 pending_skill_review=1"
$sidEsc = $SessionId.Replace("'", "''")
Invoke-MySqlQuery $db @"
INSERT INTO chat_growth_states (session_id, pending_skill_review, tool_iters_since_review)
VALUES ('$sidEsc', 1, 0)
ON DUPLICATE KEY UPDATE
  pending_skill_review = 1,
  tool_iters_since_review = 0,
  last_skill_error = NULL;
"@ | Out-Null
Write-Ok "已置 pending（worker 应在 poll 间隔内消费；config 建议 worker_poll_interval=10s）"

if ($SetPendingOnly) {
    Write-Host "`n仅置 pending。请确认后端已启动，然后检查 metrics 与 SKILL.md。" -ForegroundColor Yellow
    exit 0
}

# 5. 清理旧 example 技能（patch op=create 在文件已存在时会失败）
$skillFull = Join-Path $Workspace $skillRel
$exampleDir = Join-Path $Workspace "skills\example"
if (-not $NoClean -and (Test-Path -LiteralPath $exampleDir)) {
    Remove-Item -LiteralPath $exampleDir -Recurse -Force
    Write-Ok "已删除旧 $exampleDir 以便 create 补丁验收"
}

# 6. 轮询 metrics + 文件
Write-Step "等待 GrowthWorker 写盘（最多 ${MaxWaitSec}s）"
Write-Host "请确认另一终端已运行: cd portal; go run ./cmd/backend/... -conf ./configs"

$baseline = Get-GrowthMetrics $ApiBase
$baseCompleted = [long]$baseline.reviews_completed
Write-Ok "baseline reviews_completed=$baseCompleted"

$deadline = (Get-Date).AddSeconds($MaxWaitSec)
$passed = $false
while ((Get-Date) -lt $deadline) {
    Start-Sleep -Seconds 2
    $m = Get-GrowthMetrics $ApiBase
    $completed = [long]$m.reviews_completed
    $failed = [long]$m.reviews_failed

    $pendingRows = Invoke-MySqlQuery $db "SELECT pending_skill_review, IFNULL(last_skill_error,'') FROM chat_growth_states WHERE session_id='$sidEsc';"
    $pending = $true
    $lastErr = ""
    if ($pendingRows -and $pendingRows.Count -gt 0) {
        $p = "$($pendingRows[0])" -split "`t"
        $pending = ($p[0] -eq "1")
        if ($p.Count -ge 2) { $lastErr = $p[1] }
    }

    $fileOk = Test-Path -LiteralPath $skillFull
    Write-Host ("  completed={0} failed={1} pending_skill={2} file={3}" -f $completed, $failed, $pending, $fileOk)

    if ($fileOk -and -not $pending -and $completed -gt $baseCompleted) {
        $passed = $true
        break
    }
    if ($failed -gt [long]$baseline.reviews_failed -and $lastErr) {
        Write-Fail "复盘失败: $lastErr"
        exit 3
    }
}

Write-Step "验收结果"
if ($passed) {
    Write-Ok "reviews_completed 已增加"
    Write-Ok "pending_skill_review 已清除"
    Write-Ok "SKILL 已写入: $skillFull"
    Get-Content -LiteralPath $skillFull -TotalCount 8
    Write-Host "`n下一步：用同一 Agent 新建会话，让模型 load_skill(name=`"example`")。" -ForegroundColor Green
    exit 0
}

Write-Fail "超时或未通过。排查："
Write-Host @"
  - 后端是否 -conf ./configs 且 growth.llm_review_enabled=true
  - Agent.workspace 是否指向: $Workspace
  - migrations 001-003 是否已执行
  - GET $ApiBase/api/v1/growth/metrics
  - SELECT * FROM chat_growth_states WHERE session_id='$SessionId';
"@ -ForegroundColor Yellow
exit 4
