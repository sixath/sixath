# Phase 2 live smoke: insights + rewind against local portal.
# ASCII-only for Windows PowerShell 5.1.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$base = 'http://localhost:8000'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }

Write-Host '=== insights ==='
$insights = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent/insights" -Headers @{ Authorization = "Bearer $tok" }
Write-Host ("turns={0} tool_calls={1} error_rate={2}" -f $insights.turns, $insights.tool_calls, $insights.error_rate)

Write-Host '=== create session + stream tool ==='
$create = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "traj-p2-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$sid = $create.id
Write-Host ("SESSION={0}" -f $sid)
$marker = "phase2marker$(Get-Date -Format 'HHmmss')"
$prompt = "Call exactly one terminal tool: echo $marker Then reply briefly."

function Send-SSE([string]$sessionId, [string]$content, [int]$timeoutSec = 180) {
  $uri = "$base/api/v1/sessions/$sessionId/messages/stream"
  $req = [System.Net.HttpWebRequest]::Create($uri)
  $req.Method = 'POST'
  $req.ContentType = 'application/json'
  $req.Accept = 'text/event-stream'
  $req.Headers.Add('Authorization', "Bearer $tok")
  $req.Timeout = $timeoutSec * 1000
  $req.ReadWriteTimeout = $timeoutSec * 1000
  $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes((@{ content = $content } | ConvertTo-Json -Compress))
  $req.ContentLength = $bodyBytes.Length
  $s = $req.GetRequestStream(); $s.Write($bodyBytes, 0, $bodyBytes.Length); $s.Close()
  $resp = $req.GetResponse()
  $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $tools = 0
  $etype = ''
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    if ($line.StartsWith('event:')) { $etype = $line.Substring(6).Trim(); continue }
    if (-not $line.StartsWith('data:')) { continue }
    if ($etype -eq 'tool_call') { $tools++ }
    if ($etype -eq 'done') { break }
    $etype = ''
  }
  $reader.Close(); $resp.Close()
  return $tools
}

$toolEvents = Send-SSE $sid $prompt 240
Write-Host ("tool_events={0}" -f $toolEvents)

Start-Sleep -Seconds 1
$msgs = Invoke-RestMethod -Uri "$base/api/v1/sessions/$sid/messages?limit=50" -Headers @{ Authorization = "Bearer $tok" }
$anchor = $null
foreach ($m in $msgs.items) {
  if ($m.role -eq 'user' -and $m.id) { $anchor = $m; break }
}
if (-not $anchor) {
  foreach ($m in $msgs.items) {
    if ($m.id) { $anchor = $m; break }
  }
}
if (-not $anchor) { throw 'no message to rewind' }
Write-Host ("rewind_anchor={0}" -f $anchor.id)

$before = @($msgs.items).Count
$rw = Invoke-RestMethod -Uri "$base/api/v1/sessions/$sid/rewind" -Method POST -Headers $h -Body (@{ message_id = $anchor.id } | ConvertTo-Json)
Write-Host ("rewind_count={0} deactivated_msgs={1}" -f $rw.rewind_count, @($rw.deactivated_messages).Count)

$afterMsgs = Invoke-RestMethod -Uri "$base/api/v1/sessions/$sid/messages?limit=50" -Headers @{ Authorization = "Bearer $tok" }
$after = @($afterMsgs.items).Count
Write-Host ("messages_before={0} after={1}" -f $before, $after)

$ok = $true
if ($before -le $after) { Write-Host 'FAIL: rewind did not shorten message list'; $ok = $false }
else { Write-Host 'PASS: rewind shortened messages' }
if ($null -eq $insights) { Write-Host 'FAIL: insights empty'; $ok = $false }
else { Write-Host 'PASS: insights reachable' }

if ($ok) { Write-Host 'SMOKE_OK'; exit 0 } else { Write-Host 'SMOKE_FAIL'; exit 1 }
