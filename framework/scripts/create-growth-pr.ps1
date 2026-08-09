# 创建 Growth phase2 PR（需先执行一次: gh auth login）
$ErrorActionPreference = "Stop"
$env:Path = "C:\Program Files\GitHub CLI;C:\Program Files\Go\bin;" + $env:Path

Set-Location (Split-Path $PSScriptRoot -Parent)

gh auth status 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "请先登录: gh auth login" -ForegroundColor Yellow
    exit 1
}

$url = gh pr create `
    --base main `
    --head feat/growth-system `
    --title "feat(growth): Growth System phase 2 — skill review, LLM runner, metrics" `
    --body-file pr-body.md

Write-Host "PR created: $url" -ForegroundColor Green
