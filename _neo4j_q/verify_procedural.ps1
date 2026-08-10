$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }

Write-Host '=== probe agent ==='
$ag = Invoke-RestMethod -Uri "http://localhost:8000/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
Write-Host ("agent name={0} id={1} terminalLocal={2}" -f $ag.name, $ag.id, $ag.runtimeTools.terminalLocalEnabled)

Write-Host '=== create session ==='
$create = Invoke-RestMethod -Uri "http://localhost:8000/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "proc-verify-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$sid = $create.id
Write-Host ("SESSION={0}" -f $sid)
if (-not $sid) { throw 'no session id' }

function Send-SSE([string]$sessionId, [string]$content, [int]$timeoutSec = 180) {
  $uri = "http://localhost:8000/api/v1/sessions/$sessionId/messages/stream"
  $req = [System.Net.HttpWebRequest]::Create($uri)
  $req.Method = 'POST'
  $req.ContentType = 'application/json'
  $req.Accept = 'text/event-stream'
  $req.Headers.Add('Authorization', "Bearer $tok")
  $req.Timeout = $timeoutSec * 1000
  $req.ReadWriteTimeout = $timeoutSec * 1000
  $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes((@{ content = $content } | ConvertTo-Json -Compress))
  $req.ContentLength = $bodyBytes.Length
  $s = $req.GetRequestStream()
  $s.Write($bodyBytes, 0, $bodyBytes.Length)
  $s.Close()
  $resp = $req.GetResponse()
  $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $events = New-Object System.Collections.Generic.List[string]
  $text = ''
  $tools = New-Object System.Collections.Generic.List[string]
  $failed = New-Object System.Collections.Generic.List[string]
  $debugHits = New-Object System.Collections.Generic.List[string]
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    if ($line.StartsWith('data:')) {
      $payload = $line.Substring(5).Trim()
      if ($payload -eq '[DONE]' -or $payload -eq '') { continue }
      $events.Add($payload)
      try {
        $j = $payload | ConvertFrom-Json
        $etype = [string]$j.type
        if (-not $etype) { $etype = [string]$j.event }
        if ($etype -eq 'text' -or $etype -eq 'delta' -or $etype -eq 'chunk' -or $etype -eq 'message') {
          if ($j.content) { $text += [string]$j.content }
          elseif ($j.delta) { $text += [string]$j.delta }
          elseif ($j.text) { $text += [string]$j.text }
        }
        if ($etype -match 'tool') {
          $name = ''
          $status = ''
          if ($j.tool_call) {
            $name = [string]$j.tool_call.name
            $status = [string]$j.tool_call.status
          } elseif ($j.toolCall) {
            $name = [string]$j.toolCall.name
            $status = [string]$j.toolCall.status
          } else {
            $name = [string]$j.name
            $status = [string]$j.status
          }
          $tools.Add("$etype|$name|$status")
          if ($status -eq 'failed') { $failed.Add($payload) }
        }
        if ($payload -match 'procedural|ask_user|Prefetch') {
          $debugHits.Add($payload.Substring(0, [Math]::Min(300, $payload.Length)))
        }
      } catch {
        if ($payload -match 'failed|procedural') { $failed.Add($payload) }
      }
    }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{
    Text = $text
    Tools = $tools
    Failed = $failed
    DebugHits = $debugHits
    EventCount = $events.Count
    AllEvents = $events
  }
}

$failPrompt = 'Fault injection test: immediately call terminal_local (or bash/shell) and run the command: exit 1. Call the tool first. After failure, reply in one short sentence.'

Write-Host '=== turn1 force tool fail ==='
$r1 = Send-SSE $sid $failPrompt 150
Write-Host ("turn1 events={0} tools={1} failedHits={2} textLen={3}" -f $r1.EventCount, ($r1.Tools -join ';'), $r1.Failed.Count, $r1.Text.Length)
$r1.Failed | Select-Object -First 3 | ForEach-Object { Write-Host ("FAIL_EVT: {0}" -f $_.Substring(0, [Math]::Min(240, $_.Length))) }
$r1.Tools | Select-Object -First 10 | ForEach-Object { Write-Host ("TOOL: {0}" -f $_) }

Write-Host '=== turn2 force tool fail again ==='
$r2 = Send-SSE $sid $failPrompt 150
Write-Host ("turn2 events={0} tools={1} failedHits={2} textLen={3}" -f $r2.EventCount, ($r2.Tools -join ';'), $r2.Failed.Count, $r2.Text.Length)
$r2.Failed | Select-Object -First 3 | ForEach-Object { Write-Host ("FAIL_EVT: {0}" -f $_.Substring(0, [Math]::Min(240, $_.Length))) }
$r2.Tools | Select-Object -First 10 | ForEach-Object { Write-Host ("TOOL: {0}" -f $_) }

$sshPrompt = 'I will use ssh to login a jump host for troubleshooting. First explain your plan. If context suggests ask_user or a procedural repair hint, quote it explicitly. Do not connect to production.'
Write-Host '=== turn3 ssh trigger query ==='
$r3 = Send-SSE $sid $sshPrompt 150
Write-Host ("turn3 events={0} tools={1} textLen={2} debugHits={3}" -f $r3.EventCount, ($r3.Tools -join ';'), $r3.Text.Length, $r3.DebugHits.Count)
Write-Host '--- turn3 text head ---'
if ($r3.Text.Length -gt 0) { Write-Host $r3.Text.Substring(0, [Math]::Min(1000, $r3.Text.Length)) }
$r3.DebugHits | Select-Object -First 10 | ForEach-Object { Write-Host ("DBG: {0}" -f $_) }

$types = New-Object System.Collections.Generic.HashSet[string]
foreach ($e in $r1.AllEvents) {
  try {
    $j = $e | ConvertFrom-Json
    if ($j.type) { [void]$types.Add([string]$j.type) }
    elseif ($j.event) { [void]$types.Add([string]$j.event) }
  } catch {}
}
Write-Host ("turn1 event types: {0}" -f (($types | Sort-Object) -join ', '))

# Dump first few raw events for debugging shape
$r1.AllEvents | Select-Object -First 8 | ForEach-Object {
  Write-Host ("RAW1: {0}" -f $_.Substring(0, [Math]::Min(220, $_.Length)))
}

$out = [ordered]@{
  session = $sid
  agent = $agent
  turn1_failed = $r1.Failed.Count
  turn2_failed = $r2.Failed.Count
  turn1_tools = @($r1.Tools)
  turn2_tools = @($r2.Tools)
  turn3_text = $r3.Text
  turn3_debug = @($r3.DebugHits)
  turn1_types = @($types)
}
$out | ConvertTo-Json -Depth 6 | Out-File -Encoding utf8 d:\workspace\github\sixath\_neo4j_q\verify_procedural_out.json
Write-Host "SESSION=$sid done"
