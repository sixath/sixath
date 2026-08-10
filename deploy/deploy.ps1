param(
  [switch]$WithNeo4j,
  [switch]$WithTls,
  [switch]$Build,
  [switch]$Down,
  [switch]$SmokeOnly
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

function Require-Docker {
  if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "docker not found in PATH"
  }
  docker compose version | Out-Null
}

Require-Docker

if (-not (Test-Path .env)) {
  Copy-Item .env.example .env
  Write-Warning "created .env from .env.example"
}

New-Item -ItemType Directory -Force -Path secrets | Out-Null
Get-ChildItem secrets\*.txt.example -ErrorAction SilentlyContinue | ForEach-Object {
  $dest = $_.FullName -replace '\.example$', ''
  if (-not (Test-Path $dest)) {
    Copy-Item $_.FullName $dest
    Write-Warning "created $dest from example — replace before production use"
  }
}

# Load simple KEY=VAL from .env
$envMap = @{}
Get-Content .env | ForEach-Object {
  $line = $_.Trim()
  if ($line -eq '' -or $line.StartsWith('#')) { return }
  $i = $line.IndexOf('=')
  if ($i -lt 1) { return }
  $k = $line.Substring(0, $i).Trim()
  $v = $line.Substring($i + 1).Trim()
  $envMap[$k] = $v
  Set-Item -Path "Env:$k" -Value $v
}

if ($WithTls) {
  $domain = $envMap['DOMAIN']
  if ([string]::IsNullOrWhiteSpace($domain) -or $domain -eq 'localhost') {
    throw "--with-tls / -WithTls requires DOMAIN in .env (not empty/localhost)"
  }
}

$profiles = @()
if ($WithNeo4j) { $profiles += @('--profile', 'neo4j') }
if ($WithTls) { $profiles += @('--profile', 'tls') }

if ($Down) {
  docker compose --profile neo4j --profile tls down
  exit 0
}

if (-not $SmokeOnly) {
  $up = @('compose') + $profiles + @('up', '-d')
  if ($Build) { $up += '--build' }
  & docker @up

  Write-Host "waiting for healthy services..."
  $deadline = (Get-Date).AddSeconds(180)
  do {
    try {
      & "$PSScriptRoot\smoke-check.ps1" | Out-Null
      break
    } catch {
      Start-Sleep -Seconds 3
    }
  } while ((Get-Date) -lt $deadline)
}

& "$PSScriptRoot\smoke-check.ps1"

$webPort = if ($envMap['WEB_HOST_PORT']) { $envMap['WEB_HOST_PORT'] } else { '18080' }
$email = if ($envMap['BOOTSTRAP_ADMIN_EMAIL']) { $envMap['BOOTSTRAP_ADMIN_EMAIL'] } else { 'admin@example.com' }
Write-Host "Web UI: http://127.0.0.1:$webPort"
Write-Host "Bootstrap email: $email"
if ($WithTls) {
  Write-Host "TLS URL: https://$($envMap['DOMAIN'])"
}
