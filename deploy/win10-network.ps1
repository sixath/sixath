#Requires -RunAsAdministrator
# Win10 + WSL2 Docker networking helper (no mirrored mode — Win11 only).
#
# What this machine can do on Win10:
#   - INBOUND:  LAN -> Windows IP:ports -> portproxy -> WSL eth0 (Compose publishes)
#   - OUTBOUND: containers cannot reach some corp nets (e.g. 10.19.x); use a Windows
#               user-space TCP proxy, then hit http://<vEthernet-WSL-IP>:<port> from containers
#
# Usage (Admin PowerShell):
#   .\deploy\win10-network.ps1
#   .\deploy\win10-network.ps1 -SkipFirewall
#   .\deploy\win10-network.ps1 -EsListenPort 29200 -EsTarget 10.19.240.122:29200
#
# Optional: keep ES proxy running separately (example):
#   powershell -File C:\Users\Admin\tools\run-es-proxy.ps1

param(
  [string]$Distro = "Ubuntu",
  [int[]]$Ports = @(18080, 18000, 18088, 19000),
  [switch]$SkipFirewall,
  [string]$EsTarget = "",          # e.g. 10.19.240.122:29200
  [int]$EsListenPort = 0           # Windows listen port for corp forward (0 = skip)
)

$ErrorActionPreference = "Stop"

function Assert-Admin {
  $id = [Security.Principal.WindowsIdentity]::GetCurrent()
  $p = New-Object Security.Principal.WindowsPrincipal($id)
  if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run in elevated (Administrator) PowerShell."
  }
}
Assert-Admin

function Get-WslEth0 {
  param([string]$Name)
  $ip = (& wsl -d $Name -e bash -lc "ip -4 -o addr show eth0 2>/dev/null | awk '{print `$4}' | cut -d/ -f1" |
    Select-Object -First 1)
  if (-not $ip) { throw "WSL eth0 IPv4 empty. Is distro '$Name' running?" }
  return ($ip -replace "`r", "").Trim()
}

function Get-WslHostGateway {
  # Windows-side vEthernet (WSL) address — containers/WSL use this to reach Windows listeners.
  $gw = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
    Where-Object { $_.InterfaceAlias -like '*WSL*' -and $_.IPAddress -notlike '169.254.*' } |
    Select-Object -First 1 -ExpandProperty IPAddress
  return $gw
}

function Ensure-WslConfig {
  $cfg = Join-Path $env:USERPROFILE '.wslconfig'
  $needWrite = $true
  if (Test-Path $cfg) {
    $t = Get-Content $cfg -Raw
    if ($t -match 'vmIdleTimeout\s*=\s*-1') { $needWrite = $false }
  }
  if ($needWrite) {
    @"
# Win10: mirrored networking is unsupported. Keep VM alive for portproxy.
[wsl2]
vmIdleTimeout=-1
"@ | Set-Content -Path $cfg -Encoding UTF8
    Write-Host "Updated $cfg (vmIdleTimeout=-1). Reboot WSL later if it was already running."
  } else {
    Write-Host ".wslconfig already has vmIdleTimeout=-1"
  }
}

Assert-Admin
Ensure-WslConfig

# Ensure distro awake
& wsl -d $Distro -e echo WSL_OK | Out-Null
$wslIp = Get-WslEth0 -Name $Distro
$winGw = Get-WslHostGateway
$lanIps = Get-NetIPAddress -AddressFamily IPv4 |
  Where-Object {
    $_.IPAddress -notlike '127.*' -and
    $_.IPAddress -notlike '169.254.*' -and
    $_.InterfaceAlias -notlike '*WSL*' -and
    $_.InterfaceAlias -notlike '*Loopback*'
  } |
  Select-Object -ExpandProperty IPAddress -Unique

Write-Host ""
Write-Host "=== addresses ==="
Write-Host "WSL eth0 (portproxy target): $wslIp"
Write-Host "Windows vEthernet WSL (container->Windows): $(if ($winGw) { $winGw } else { '(not found)' })"
Write-Host "Windows LAN IP(s): $($lanIps -join ', ')"

Write-Host ""
Write-Host "=== inbound: portproxy + firewall ==="
foreach ($port in $Ports) {
  netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$port 2>$null | Out-Null
  netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=$port connectaddress=$wslIp connectport=$port
  Write-Host "  0.0.0.0:$port -> ${wslIp}:$port"
  if (-not $SkipFirewall) {
    $name = "SixathWSL-$port"
    netsh advfirewall firewall delete rule name="$name" 2>$null | Out-Null
    netsh advfirewall firewall add rule name="$name" dir=in action=allow protocol=TCP localport=$port profile=any | Out-Null
  }
}

Write-Host ""
Write-Host "portproxy:"
netsh interface portproxy show all

if ($EsListenPort -gt 0 -and $EsTarget) {
  $parts = $EsTarget.Split(':')
  if ($parts.Count -ne 2) { throw "-EsTarget must be host:port" }
  Write-Host ""
  Write-Host "=== outbound hint (corp ES etc.) ==="
  Write-Host "Start a Windows TCP proxy on 0.0.0.0:$EsListenPort -> $EsTarget"
  Write-Host "  example: C:\Users\Admin\tools\run-es-proxy.ps1"
  if ($winGw) {
    Write-Host "From containers / WSL use: http://${winGw}:$EsListenPort  (NOT $EsTarget directly)"
  }
}

Write-Host ""
Write-Host "=== open from other PCs ==="
foreach ($ip in $lanIps) {
  Write-Host "  http://${ip}:18080"
}
Write-Host ""
Write-Host "Re-run after: wsl --shutdown, reboot, or WSL eth0 IP change."
Write-Host "Only inbound helper: .\deploy\expose-wsl-ports.ps1"
