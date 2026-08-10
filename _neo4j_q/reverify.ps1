$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$exe = 'D:\workspace\github\sixath\portal\bin\backend_e2e.exe'
Write-Host ("binary mtime={0}" -f (Get-Item $exe).LastWriteTime)

$old = Get-NetTCPConnection -LocalPort 8000 -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique
foreach ($procId in $old) {
  if ($procId -and $procId -gt 0) {
    Write-Host "Stopping PID $procId"
    Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
  }
}
Start-Sleep -Seconds 2
$p = Start-Process -FilePath $exe -ArgumentList '-conf','E:\configs\sixath\portal' -WorkingDirectory 'D:\workspace\github\sixath\portal' -PassThru -WindowStyle Hidden
Write-Host "Started PID=$($p.Id)"

$code = '000'
for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 1
  $code = curl.exe -s -o NUL -w "%{http_code}" --max-time 2 -H "Authorization: Bearer dev-bootstrap-token" "http://localhost:8000/api/v1/agents"
  if ($code -eq '200') { Write-Host "API ready after $($i+1)s"; break }
}
if ($code -ne '200') { throw "api not ready: $code" }

$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$create = Invoke-RestMethod -Uri "http://localhost:8000/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "proc-reverify-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$sid = $create.id
Write-Host "SESSION=$sid"
"SESSION=$sid" | Out-File -Encoding ascii d:\workspace\github\sixath\_neo4j_q\last_session.txt

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
  $failed = 0
  $askUser = 0
  $procHints = New-Object System.Collections.Generic.List[string]
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine(); if ($null -eq $line) { break }
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ($payload -eq '' -or $payload -eq '[DONE]') { continue }
    if ($payload -match 'agent\.tool\.failed|"phase":"failed"') { $failed++ }
    if ($payload -match '"tool_name":"ask_user"|tool":"ask_user"') { $askUser++ }
    if ($payload -match '过程修复|procedural_repair|tool_failed') {
      $procHints.Add($payload.Substring(0, [Math]::Min(220, $payload.Length)))
    }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{ Failed = $failed; AskUser = $askUser; Hints = $procHints }
}

$failPrompt = 'Fault injection: call describe_table with table_name exactly NO_SUCH_TABLE_REVERIFY. Then reply FAIL_DONE. No other tools.'
Write-Host '=== fail1 ==='
$r1 = Send-SSE $sid $failPrompt 150
Write-Host ("fail1_tool_failed_events={0}" -f $r1.Failed)

Write-Host '=== fail2 ==='
$r2 = Send-SSE $sid $failPrompt 150
Write-Host ("fail2_tool_failed_events={0}" -f $r2.Failed)

Write-Host '=== ssh trigger ==='
$r3 = Send-SSE $sid 'I will use ssh to login a jump host for troubleshooting. If context has a procedural repair tip about ask_user, quote it. Do not connect to production.' 150
Write-Host ("ssh_ask_user_events={0} hint_hits={1}" -f $r3.AskUser, $r3.Hints.Count)
$r3.Hints | Select-Object -First 8 | ForEach-Object { Write-Host ("HINT: {0}" -f $_) }

Write-Host "SESSION=$sid"
python d:\workspace\github\sixath\_neo4j_q\check_session_proc.py $sid
python d:\workspace\github\sixath\_neo4j_q\print_unit.py 2>$null
# print newest procedural for this session via check output already

$pass = ($r1.Failed -gt 0) -and ($r2.Failed -gt 0)
Write-Host ("TOOL_FAILED_OK={0}" -f $pass)
Write-Host "UI=http://localhost:5174/?agent=$agent"
Write-Host "Open session: $sid"
