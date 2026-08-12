# Windows → WSL one-click deploy (Docker Engine inside Ubuntu; no Docker Desktop).
# First time in Ubuntu: bash deploy/install-docker-wsl.sh
# Example: .\deploy\deploy-wsl.ps1 -Build -Distro Ubuntu
param(
  [switch]$WithNeo4j,
  [switch]$WithTls,
  [switch]$Build,
  [switch]$Down,
  [switch]$SmokeOnly,
  [string]$Distro = "Ubuntu"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

if (-not (Get-Command wsl -ErrorAction SilentlyContinue)) {
  throw "wsl not found. Install WSL (wsl --install) and Docker Desktop with WSL2 backend."
}

$wslPath = (& wsl -e wslpath -a $Root 2>$null | Select-Object -First 1)
if (-not $wslPath) {
  throw "failed to convert path via wslpath: $Root"
}
$wslPath = ($wslPath -replace "`r", "").Trim()

$flags = New-Object System.Collections.Generic.List[string]
if ($WithNeo4j) { [void]$flags.Add("--with-neo4j") }
if ($WithTls) { [void]$flags.Add("--with-tls") }
if ($Build) { [void]$flags.Add("--build") }
if ($Down) { [void]$flags.Add("--down") }
if ($SmokeOnly) { [void]$flags.Add("--smoke-only") }
$flagStr = ($flags -join " ")

Write-Host "WSL path: $wslPath"
if ($Distro) { Write-Host "Distro:   $Distro" }

# Strip CRLF then run (repo on NTFS often has CRLF scripts that break bash).
$bashCmd = @"
set -euo pipefail
cd '$wslPath'
tmp=`$(mktemp)
tr -d '\r' < deploy/deploy-wsl.sh > "`$tmp"
chmod +x "`$tmp"
bash "`$tmp" $flagStr
ec=`$?
rm -f "`$tmp"
exit `$ec
"@

$wslArgs = @()
if ($Distro) {
  $wslArgs += @("-d", $Distro)
}
$wslArgs += @("-e", "bash", "-lc", $bashCmd)

& wsl @wslArgs
exit $LASTEXITCODE
