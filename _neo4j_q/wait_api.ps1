$ErrorActionPreference = 'Continue'
Get-CimInstance Win32_Process | Where-Object { $_.Name -match 'backend' } | Select-Object ProcessId, Name, CommandLine | Format-List
Get-NetTCPConnection -LocalPort 8000 -ErrorAction SilentlyContinue | Format-Table OwningProcess, State

$exe = 'D:\workspace\github\sixath\portal\bin\backend_e2e.exe'
$alive = Get-Process -Id 38000 -ErrorAction SilentlyContinue
if (-not $alive) {
  Write-Host 'PID 38000 dead; restarting'
  $p = Start-Process -FilePath $exe -ArgumentList '-conf','E:\configs\sixath\portal' -WorkingDirectory 'D:\workspace\github\sixath\portal' -PassThru -WindowStyle Hidden
  Write-Host "NEW_PID=$($p.Id)"
  Start-Sleep -Seconds 8
} else {
  Write-Host 'PID 38000 still alive; waiting more'
  Start-Sleep -Seconds 8
}

for ($i=0; $i -lt 20; $i++) {
  try {
    $code = curl.exe -s -o NUL -w "%{http_code}" --max-time 3 -H "Authorization: Bearer dev-bootstrap-token" "http://localhost:8000/api/v1/agents"
    Write-Host "try$i HTTP=$code"
    if ($code -eq '200') { exit 0 }
  } catch {
    Write-Host "try$i err"
  }
  Start-Sleep -Seconds 2
}
exit 1
