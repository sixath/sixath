# Run from execute_skill_script; finds framework root via go.mod.
$ErrorActionPreference = "Stop"
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path
$Root = if ($env:SKILL_ROOT) { $env:SKILL_ROOT } else { Resolve-Path (Join-Path $Here "..\..\..") }
if (-not (Test-Path (Join-Path $Root "go.mod"))) {
    Write-Error "framework go.mod not found at $Root"
}
Set-Location $Root
Write-Host "==> go test ./... (framework root: $Root)"
go test ./...
