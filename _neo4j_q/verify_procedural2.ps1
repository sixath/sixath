$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }

$create = Invoke-RestMethod -Uri "http://localhost:8000/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "proc-tf-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
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
  $failedBus = 0
  $toolPhases = New-Object System.Collections.Generic.List[string]
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ($payload -eq '' -or $payload -eq '[DONE]') { continue }
    $events.Add($payload)
    if ($payload -match 'agent\.tool\.failed|ToolFailed|"phase":"failed"|tool\.failed') { $failedBus++ }
    if ($payload -match 'tool_call|"tool"') {
      try {
        $j = $payload | ConvertFrom-Json
        if ($j.tool_call) {
          $toolPhases.Add(("{0}:{1}:{2}" -f $j.tool_call.tool_name, $j.tool_call.phase, $j.tool_call.allowed))
        }
      } catch {}
    }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{ EventCount = $events.Count; FailedBus = $failedBus; ToolPhases = $toolPhases; Events = $events }
}

# Force real ToolFailed: tool_describe missing tool returns Go error
$failPrompt = 'Fault injection: call tool_describe with name exactly equal to NOT_A_REAL_TOOL_XYZ. Do not call other tools. After the tool returns, say FAIL_DONE.'
Write-Host '=== turn1 tool_describe miss ==='
$r1 = Send-SSE $sid $failPrompt 150
Write-Host ("turn1 events={0} failedBus={1} tools={2}" -f $r1.EventCount, $r1.FailedBus, ($r1.ToolPhases -join ';'))
$r1.Events | Where-Object { $_ -match 'failed|NOT_A_REAL|tool_describe|error' } | Select-Object -First 8 | ForEach-Object {
  Write-Host ("HIT1: {0}" -f $_.Substring(0, [Math]::Min(260, $_.Length)))
}

Write-Host '=== turn2 tool_describe miss again ==='
$r2 = Send-SSE $sid $failPrompt 150
Write-Host ("turn2 events={0} failedBus={1} tools={2}" -f $r2.EventCount, $r2.FailedBus, ($r2.ToolPhases -join ';'))
$r2.Events | Where-Object { $_ -match 'failed|NOT_A_REAL|tool_describe|error' } | Select-Object -First 8 | ForEach-Object {
  Write-Host ("HIT2: {0}" -f $_.Substring(0, [Math]::Min(260, $_.Length)))
}

$sshPrompt = 'I need to use ssh next. Quote any procedural / ask_user repair suggestion from context if present. Do not connect.'
Write-Host '=== turn3 ssh ==='
$r3 = Send-SSE $sid $sshPrompt 150
Write-Host ("turn3 events={0}" -f $r3.EventCount)
$r3.Events | Where-Object { $_ -match 'procedural|ask_user|Prefetch|过程修复' } | Select-Object -First 12 | ForEach-Object {
  Write-Host ("HIT3: {0}" -f $_.Substring(0, [Math]::Min(300, $_.Length)))
}
# text chunks
$text = ''
foreach ($e in $r3.Events) {
  try {
    $j = $e | ConvertFrom-Json
    if ($j.content -and ($j.content -notmatch '^agent\.')) { $text += [string]$j.content }
    if ($j.delta) { $text += [string]$j.delta }
  } catch {}
}
Write-Host '--- turn3 text ---'
Write-Host $text.Substring(0, [Math]::Min(1200, $text.Length))

@{ session = $sid; t1_failed = $r1.FailedBus; t2_failed = $r2.FailedBus; t1_tools = @($r1.ToolPhases); t2_tools = @($r2.ToolPhases); t3_text = $text } |
  ConvertTo-Json -Depth 5 | Out-File -Encoding utf8 d:\workspace\github\sixath\_neo4j_q\verify_procedural2_out.json
Write-Host "SESSION=$sid done"
