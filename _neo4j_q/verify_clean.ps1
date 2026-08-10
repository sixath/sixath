$ErrorActionPreference = 'Stop'
$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$create = Invoke-RestMethod -Uri "http://localhost:8000/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "proc-clean-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
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
$r1.Events | Where-Object { $_ -match 'failed|describe_table' } | Select-Object -First 4 | ForEach-Object { Write-Host ("H1: {0}" -f $_.Substring(0, [Math]::Min(220, $_.Length))) }

Write-Host '=== fail2 ==='
$r2 = Send-SSE $sid $failPrompt 150
Write-Host ("fail2={0}" -f $r2.Failed)
$r2.Events | Where-Object { $_ -match 'failed|describe_table' } | Select-Object -First 4 | ForEach-Object { Write-Host ("H2: {0}" -f $_.Substring(0, [Math]::Min(220, $_.Length))) }

Write-Host '=== ssh ==='
$r3 = Send-SSE $sid 'I will use ssh to a jump host. Quote any procedural repair suggestion from context verbatim if present.' 150
$r3.Events | Where-Object { $_ -match '过程修复|procedural|ask_user|Prefetch' } | Select-Object -First 12 | ForEach-Object { Write-Host ("H3: {0}" -f $_.Substring(0, [Math]::Min(260, $_.Length))) }
Write-Host "SESSION=$sid done"
