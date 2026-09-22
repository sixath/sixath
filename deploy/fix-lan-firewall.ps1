#Requires -RunAsAdministrator
$ErrorActionPreference = "Continue"
$log = "E:\workspace\github\sixath\sixath\deploy\lan-firewall-result.log"
function Log($m) { "$(Get-Date -Format o) $m" | Tee-Object -FilePath $log -Append }

try {
  Remove-Item $log -Force -ErrorAction SilentlyContinue
  Log "START elevated=$([bool](([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)))"

  $wslIp = (wsl -d Ubuntu -e hostname -I).Trim().Split(" ")[0]
  Log "WSL_IP=$wslIp"
  if (-not $wslIp) { throw "empty WSL IP" }

  foreach ($p in 18080,18000,18088,19000) {
    netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$p | Out-Null
    netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=$p connectaddress=$wslIp connectport=$p | Out-Null
    Log "portproxy 0.0.0.0:$p -> ${wslIp}:$p"

    $name = "SixathWSL-$p"
    netsh advfirewall firewall delete rule name="$name" | Out-Null
    $r = netsh advfirewall firewall add rule name="$name" dir=in action=allow protocol=TCP localport=$p profile=any
    Log "firewall add $name => $r"
  }

  Log "RULES:"
  netsh advfirewall firewall show rule name=all | Select-String -Pattern "SixathWSL" -Context 0,5 | Out-String | Tee-Object -FilePath $log -Append
  Log "DONE"
} catch {
  Log "ERROR: $_"
  exit 1
}
