$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$envMap = @{}
if (Test-Path .env) {
  Get-Content .env | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq '' -or $line.StartsWith('#')) { return }
    $i = $line.IndexOf('=')
    if ($i -lt 1) { return }
    $envMap[$line.Substring(0, $i).Trim()] = $line.Substring($i + 1).Trim()
  }
}

$webPort = if ($envMap['WEB_HOST_PORT']) { $envMap['WEB_HOST_PORT'] } else { '18080' }
$gwPort = if ($envMap['GATEWAY_HOST_PORT']) { $envMap['GATEWAY_HOST_PORT'] } else { '18088' }
$portalPort = if ($envMap['PORTAL_HTTP_HOST_PORT']) { $envMap['PORTAL_HTTP_HOST_PORT'] } else { '18000' }

function Check-Url([string]$Name, [string]$Url) {
  try {
    Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 5 | Out-Null
    Write-Host "OK $Name $Url"
  } catch {
    Write-Error "FAIL $Name $Url"
    throw
  }
}

Check-Url web "http://127.0.0.1:$webPort/healthz"
Check-Url gateway "http://127.0.0.1:$gwPort/healthz"
Check-Url portal "http://127.0.0.1:$portalPort/readyz"

@{
  ok = $true
  web = "http://127.0.0.1:$webPort/healthz"
  gateway = "http://127.0.0.1:$gwPort/healthz"
  portal = "http://127.0.0.1:$portalPort/readyz"
} | ConvertTo-Json | Set-Content -Path deploy/last-smoke.json -Encoding utf8

Write-Host "smoke ok"
