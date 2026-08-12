#Requires -RunAsAdministrator
# Repair WSL E_UNEXPECTED / 灾难性故障 (run in elevated PowerShell).
# Usage:  powershell -ExecutionPolicy Bypass -File deploy\repair-wsl.ps1

$ErrorActionPreference = "Continue"
Write-Host "==> WSL repair starting..." -ForegroundColor Cyan

function Assert-Admin {
  $id = [Security.Principal.WindowsIdentity]::GetCurrent()
  $p = New-Object Security.Principal.WindowsPrincipal($id)
  if (-not $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Must run as Administrator."
  }
}
Assert-Admin

Write-Host "==> Enabling optional features (WSL + Virtual Machine Platform)"
dism.exe /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart
dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart

Write-Host "==> Restarting related services"
foreach ($name in @("HvHost", "vmcompute", "LxssManager")) {
  $svc = Get-Service $name -ErrorAction SilentlyContinue
  if (-not $svc) {
    Write-Warning "service missing: $name"
    continue
  }
  try {
    if ($svc.Status -ne "Running") {
      Start-Service $name -ErrorAction Stop
      Write-Host "started $name"
    } else {
      Restart-Service $name -Force -ErrorAction Stop
      Write-Host "restarted $name"
    }
  } catch {
    Write-Warning "failed to start/restart ${name}: $($_.Exception.Message)"
  }
}

Write-Host "==> Updating WSL"
wsl --update
wsl --set-default-version 2

Write-Host "==> Shutting down WSL"
wsl --shutdown
Start-Sleep -Seconds 3

Write-Host "==> Distro list"
wsl -l -v

Write-Host "==> Probe Ubuntu"
$probe = & wsl -d Ubuntu -e echo WSL_OK 2>&1
$code = $LASTEXITCODE
Write-Host $probe
Write-Host "exit=$code"

if ($code -ne 0) {
  Write-Host ""
  Write-Host "Ubuntu still broken. Reinstall distro? This DELETES the Ubuntu filesystem." -ForegroundColor Yellow
  Write-Host "Run manually if you accept data loss inside Ubuntu:" -ForegroundColor Yellow
  Write-Host '  wsl --unregister Ubuntu'
  Write-Host '  wsl --install -d Ubuntu'
  Write-Host "Then reboot once more and open: wsl -d Ubuntu"
  exit 1
}

Write-Host "WSL OK. Next in Ubuntu:" -ForegroundColor Green
Write-Host "  cd /mnt/e/workspace/github/sixath/sixath"
Write-Host "  bash deploy/install-docker-wsl.sh"
