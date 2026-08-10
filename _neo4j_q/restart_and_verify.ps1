$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# Restart portal with same conf
$old = Get-NetTCPConnection -LocalPort 8000 -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique
foreach ($procId in $old) {
  if ($procId) {
    Write-Host "Stopping PID $procId"
    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
  }
}
Start-Sleep -Seconds 2
$exe = 'D:\workspace\github\sixath\portal\bin\backend_e2e.exe'
if (-not (Test-Path $exe)) { $exe = 'D:\workspace\github\sixath\portal\bin\backend.exe' }
$p = Start-Process -FilePath $exe -ArgumentList '-conf','E:\configs\sixath\portal' -WorkingDirectory 'D:\workspace\github\sixath\portal' -PassThru -WindowStyle Hidden
Write-Host "Started PID=$($p.Id)"
Start-Sleep -Seconds 4
$code = curl.exe -s -o NUL -w "%{http_code}" -H "Authorization: Bearer dev-bootstrap-token" "http://localhost:8000/api/v1/agents"
Write-Host "API_HTTP=$code"
if ($code -ne '200') { throw "api not ready: $code" }

$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$create = Invoke-RestMethod -Uri "http://localhost:8000/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "proc-clean-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$sid = $create.id
Write-Host "SESSION=$sid"

function Send-SSE([string]$sessionId, [string]$content, [int]$timeoutSec = 180) {
  $uri = "http://localhost:8000/api/v1/sessions/$sessionId/messages/stream"
  $req = [System.Net.HttpWebRequest]::Create($uri)
  $req.Method = 'POST'; $req.ContentType = 'application/json'; $req.Accept = 'text/event-stream'
  $req.Headers.Add('Authorization', "Bearer $tok")
  $req.Timeout = $timeoutSec * 1000; $req.ReadWriteTimeout = $timeoutSec * 1000
  $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes((@{ content = $content } | ConvertTo-Json -Compress))
  $req.ContentLength = $bodyBytes.Length
  $s = $req.GetRequestStream(); $s.Write($bodyBytes, 0, $bodyBytes.Length); $s.Close()
  $resp = $req.GetResponse(); $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $failed = 0; $events = New-Object System.Collections.Generic.List[string]
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine(); if ($null -eq $line) { break }
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ($payload -eq '' -or $payload -eq '[DONE]') { continue }
    $events.Add($payload)
    if ($payload -match 'agent\.tool\.failed|"phase":"failed"') { $failed++ }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{ Failed = $failed; Events = $events }
}

$failPrompt = 'Fault injection: call describe_table with table_name exactly NO_SUCH_TABLE_PROC_CLEAN. Then reply FAIL_DONE. No other tools.'
Write-Host '=== fail1 ==='
$r1 = Send-SSE $sid $failPrompt 150
Write-Host ("fail1={0}" -f $r1.Failed)
Write-Host '=== fail2 ==='
$r2 = Send-SSE $sid $failPrompt 150
Write-Host ("fail2={0}" -f $r2.Failed)

Write-Host '=== ssh turn ==='
$r3 = Send-SSE $sid 'I will use ssh to a jump host. Quote any procedural repair / ask_user suggestion from context verbatim.' 150
$procHits = @($r3.Events | Where-Object { $_ -match '过程修复|procedural|ask_user' })
Write-Host ("ssh_proc_hits={0}" -f $procHits.Count)
$procHits | Select-Object -First 10 | ForEach-Object { Write-Host ("P: {0}" -f $_.Substring(0, [Math]::Min(240, $_.Length))) }

Write-Host "SESSION=$sid"
"SESSION=$sid" | Out-File -Encoding ascii d:\workspace\github\sixath\_neo4j_q\last_session.txt
