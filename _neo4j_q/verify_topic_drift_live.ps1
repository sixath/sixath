$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$tok = "dev-bootstrap-token"
$base = "http://localhost:8000"
$h = @{ Authorization = "Bearer $tok"; "Content-Type" = "application/json" }
$agent = "e8107fb3-e40a-4207-9d9a-6768847aaf79"
$outPath = Join-Path $PSScriptRoot "verify_topic_drift_live_out.json"
$results = [ordered]@{ ok = $false; session = $null; tools = @(); reply_excerpt = ""; checks = @() }

function Assert-True([bool]$cond, [string]$msg) {
  if (-not $cond) { throw "ASSERT: $msg" }
  Write-Host "OK  $msg"
  $script:results.checks += $msg
}

function Send-SSE([string]$sessionId, [string]$jsonBody, [int]$timeoutSec = 180) {
  $uri = "$base/api/v1/sessions/$sessionId/messages/stream"
  $req = [System.Net.HttpWebRequest]::Create($uri)
  $req.Method = "POST"
  $req.ContentType = "application/json"
  $req.Accept = "text/event-stream"
  $req.Headers.Add("Authorization", "Bearer $tok")
  $req.Timeout = $timeoutSec * 1000
  $req.ReadWriteTimeout = $timeoutSec * 1000
  $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes($jsonBody)
  $req.ContentLength = $bodyBytes.Length
  $s = $req.GetRequestStream(); $s.Write($bodyBytes, 0, $bodyBytes.Length); $s.Close()
  $resp = $req.GetResponse()
  $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $lines = New-Object System.Collections.Generic.List[string]
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    [void]$lines.Add($line)
  }
  $reader.Close(); $resp.Close()
  $lines -join "`n" | Set-Content (Join-Path $PSScriptRoot "verify_topic_drift_live_sse.txt") -Encoding utf8
  if (($lines -join "`n") -match "event: error") { throw ("SSE error: " + ($lines -join " | ")) }
}

function Get-ToolTimeline([string]$sessionId) {
  $msgs = Invoke-RestMethod -Uri "$base/api/v1/sessions/$sessionId/messages" -Headers @{ Authorization = "Bearer $tok" }
  $out = @()
  $reply = ""
  foreach ($m in @($msgs.items)) {
    if ($m.role -eq "assistant" -and $m.content) { $reply = [string]$m.content }
    if ($m.role -ne "assistant" -or -not $m.metadata -or -not $m.metadata.timeline) { continue }
    foreach ($t in @($m.metadata.timeline)) {
      if ($t.toolName) { $out += [string]$t.toolName }
    }
  }
  return @{ tools = $out; reply = $reply }
}

$ag = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
Assert-True (-not [bool]$ag.runtimeTools.webToolsEnabled) "agent webToolsEnabled=false"

$session = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = ("topic-drift-live-" + (Get-Date -Format "HHmmss")) } | ConvertTo-Json)
$sid = $session.id
Assert-True (-not [string]::IsNullOrWhiteSpace($sid)) ("session created: " + $sid)
$results.session = $sid

$prompt = "Summarize cloudgame in one or two sentences. Then you MUST call web_search for query: Consumer Rights Law Article 25 seven-day no-reason return original text, and put the search results in the same reply."
$jsonBody = (@{ content = $prompt } | ConvertTo-Json -Compress)
Write-Host ("=== send (" + $sid + ") ===")
Send-SSE $sid $jsonBody 240

$got = Get-ToolTimeline $sid
$tools = @($got.tools)
$results.tools = $tools
if ($got.reply.Length -gt 500) { $results.reply_excerpt = $got.reply.Substring(0, 500) } else { $results.reply_excerpt = $got.reply }
Write-Host ("tools: " + ($tools -join ", "))
Write-Host ("reply_len=" + $got.reply.Length)
Assert-True ($tools -notcontains "web_search") "timeline has no web_search"
Assert-True ($tools -notcontains "web_extract") "timeline has no web_extract"
Assert-True ($got.reply.Length -gt 0) "assistant reply non-empty"

$results.ok = $true
($results | ConvertTo-Json -Depth 6) | Set-Content -Path $outPath -Encoding utf8
Write-Host ("PASS -> " + $outPath)