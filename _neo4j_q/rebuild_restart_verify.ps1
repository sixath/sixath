$ErrorActionPreference = 'Stop'
$exe = 'D:\workspace\github\sixath\portal\bin\backend_e2e.exe'
Get-Item $exe | Select-Object FullName, Length, LastWriteTime | Format-List

$old = Get-NetTCPConnection -LocalPort 8000 -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique
foreach ($procId in $old) {
  if ($procId) {
    Write-Host "Stopping PID $procId"
    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
  }
}
Start-Sleep -Seconds 2
$p = Start-Process -FilePath $exe -ArgumentList '-conf','E:\configs\sixath\portal' -WorkingDirectory 'D:\workspace\github\sixath\portal' -PassThru -WindowStyle Hidden
Write-Host "Started PID=$($p.Id)"
for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 1
  $code = curl.exe -s -o NUL -w "%{http_code}" --max-time 2 -H "Authorization: Bearer dev-bootstrap-token" "http://localhost:8000/api/v1/agents"
  if ($code -eq '200') { Write-Host "API ready after $($i+1)s"; break }
  Write-Host "wait$i HTTP=$code"
}
if ($code -ne '200') { throw "api not ready" }

# run clean verify
& powershell -NoProfile -File 'd:\workspace\github\sixath\_neo4j_q\verify_clean.ps1'
$sid = (Get-Content 'd:\workspace\github\sixath\_neo4j_q\last_session.txt' -ErrorAction SilentlyContinue | Select-Object -Last 1)
if ($sid -match 'SESSION=(.+)') { $sid = $Matches[1] }
Write-Host "Checking DB for $sid"
python 'd:\workspace\github\sixath\_neo4j_q\check_session_proc.py' $sid
