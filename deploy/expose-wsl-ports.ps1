#Requires -RunAsAdministrator
# Expose WSL-hosted Compose ports on the Windows LAN IP.
# WSL2 only forwards to 127.0.0.1 by default; LAN clients need portproxy + firewall.
#
# Usage (Admin PowerShell):
#   .\deploy\expose-wsl-ports.ps1
#   .\deploy\expose-wsl-ports.ps1 -Remove
#   .\deploy\expose-wsl-ports.ps1 -Distro Ubuntu -Ports 18080,18000,18088

param(
  [string]$Distro = "Ubuntu",
  [int[]]$Ports = @(18080, 18000, 18088, 19000),
  [switch]$Remove
)

$ErrorActionPreference = "Stop"

function Assert-Admin {
  $id = [Security.Principal.WindowsIdentity]::GetCurrent()
  $p = New-Object Security.Principal.WindowsPrincipal($id)
  if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this script in an elevated (Administrator) PowerShell."
  }
}
Assert-Admin

function Get-WslIpv4 {
  param([string]$Name)
  $ip = (& wsl -d $Name -e bash -lc "ip -4 -o addr show eth0 | awk '{print `$4}' | cut -d/ -f1" 2>$null |
    Select-Object -First 1)
  if (-not $ip) {
    throw "Could not read WSL eth0 IPv4 for distro '$Name'. Is WSL running? Try: wsl -d $Name"
  }
  return ($ip -replace "`r", "").Trim()
}

$rulePrefix = "SixathWSL"

if ($Remove) {
  Write-Host "==> Removing portproxy + firewall rules"
  foreach ($port in $Ports) {
    netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$port 2>$null | Out-Null
    netsh interface portproxy delete v4tov4 listenaddress=127.0.0.1 listenport=$port 2>$null | Out-Null
    $rule = "$rulePrefix-$port"
    if (Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue) {
      Remove-NetFirewallRule -DisplayName $rule
    }
  }
  Write-Host "Done. Current portproxy:"
  netsh interface portproxy show all
  exit 0
}

$wslIp = Get-WslIpv4 -Name $Distro
Write-Host "WSL distro: $Distro"
Write-Host "WSL eth0:   $wslIp"

$lanIps = Get-NetIPAddress -AddressFamily IPv4 |
  Where-Object {
    $_.IPAddress -notlike '127.*' -and
    $_.InterfaceAlias -notlike '*WSL*' -and
    $_.InterfaceAlias -notlike '*Loopback*'
  } |
  Select-Object -ExpandProperty IPAddress -Unique

Write-Host "Windows LAN IP(s): $($lanIps -join ', ')"

foreach ($port in $Ports) {
  Write-Host "==> forward 0.0.0.0:$port -> ${wslIp}:$port"
  netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$port 2>$null | Out-Null
  netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=$port connectaddress=$wslIp connectport=$port

  $rule = "$rulePrefix-$port"
  if (Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue) {
    Remove-NetFirewallRule -DisplayName $rule
  }
  New-NetFirewallRule -DisplayName $rule -Direction Inbound -Action Allow -Protocol TCP -LocalPort $port |
    Out-Null
}

Write-Host ""
Write-Host "portproxy table:"
netsh interface portproxy show all
Write-Host ""
Write-Host "From another machine, open:"
foreach ($ip in $lanIps) {
  Write-Host "  http://${ip}:18080   (Web UI)"
}
Write-Host ""
Write-Host "Note: WSL IP changes after wsl --shutdown / reboot. Re-run this script then."
Write-Host "Remove rules:  .\deploy\expose-wsl-ports.ps1 -Remove"
