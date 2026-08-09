# 配置 Growth 闭环所需环境变量（当前 PowerShell 会话有效）
# 用法: . .\scripts\setup_growth_env.ps1
# 可选: 复制 .env.growth.example 为 .env.growth 并填入 API Key

$ErrorActionPreference = "Stop"
$portal = Split-Path $PSScriptRoot -Parent
$repo = Split-Path $portal -Parent

# 让 Growth 复盘读到仓库根 .learnings
$learnings = Join-Path $repo ".learnings"
if (Test-Path $learnings) {
    $env:SATH_LEARNINGS_DIR = $learnings
    Write-Host "[OK] SATH_LEARNINGS_DIR=$learnings"
} else {
    Write-Host "[WARN] .learnings not found at $learnings" -ForegroundColor Yellow
}

$envFile = Join-Path $portal ".env.growth"
if (Test-Path $envFile) {
    Get-Content $envFile | ForEach-Object {
        $line = $_.Trim()
        if ($line -eq "" -or $line.StartsWith("#")) { return }
        if ($line -match '^([^=]+)=(.*)$') {
            $name = $Matches[1].Trim()
            $val = $Matches[2].Trim().Trim('"')
            Set-Item -Path "env:$name" -Value $val
            Write-Host "[OK] $name=(set)"
        }
    }
} else {
    Write-Host "[INFO] 无 .env.growth；可复制 .env.growth.example 并配置 SATH_GROWTH_LLM_*" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "下一步:" -ForegroundColor Cyan
Write-Host "  cd portal"
Write-Host "  go run ./cmd/backend/... -conf ./configs"
Write-Host "  # 另一终端: .\scripts\verify_growth_write.ps1  或与 Agent 对话触发 C2s"
