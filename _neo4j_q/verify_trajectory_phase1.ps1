# Phase 1 trajectory utilization live smoke against local portal.
# Prerequisites: backend with Phase1 built, listening on :8000.
# NOTE: Keep this file ASCII-only so Windows PowerShell 5.1 parses it without UTF-8 BOM issues.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$tok = 'dev-bootstrap-token'
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79' # zone-4100-agent
$base = 'http://localhost:8000'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$report = [ordered]@{}

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
  $s = $req.GetRequestStream()
  $s.Write($bodyBytes, 0, $bodyBytes.Length)
  $s.Close()
  $resp = $req.GetResponse()
  $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $tools = New-Object System.Collections.Generic.List[string]
  $text = ''
  $etype = ''
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    # Portal SSE: "event: <name>\ndata: <json>\n\n" (type is NOT inside JSON).
    if ($line.StartsWith('event:')) {
      $etype = $line.Substring(6).Trim()
      continue
    }
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ($payload -eq '[DONE]' -or $payload -eq '') { continue }
    try {
      $j = $payload | ConvertFrom-Json
      if ($etype -eq 'chunk' -or $etype -eq 'text' -or $etype -eq 'delta' -or $etype -eq 'message') {
        if ($j.content) { $text += [string]$j.content }
        elseif ($j.delta) { $text += [string]$j.delta }
        elseif ($j.text) { $text += [string]$j.text }
      }
      if ($etype -eq 'tool_call' -or $etype -match 'tool') {
        $name = ''
        if ($j.tool_call) {
          $name = [string]$j.tool_call.tool_name
          if (-not $name) { $name = [string]$j.tool_call.name }
        } elseif ($j.toolCall) {
          $name = [string]$j.toolCall.tool_name
          if (-not $name) { $name = [string]$j.toolCall.name }
        } else {
          $name = [string]$j.tool_name
          if (-not $name) { $name = [string]$j.name }
        }
        if ($name) { $tools.Add($name) }
      }
      if ($etype -eq 'done') { break }
    } catch {}
    $etype = ''
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{ Text = $text; Tools = $tools }
}

Write-Host '=== probe agent ==='
$ag = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
$report.agent = $ag.name
Write-Host ("agent={0}" -f $ag.name)

Write-Host '=== create session ==='
$create = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "traj-phase1-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$sid = $create.id
if (-not $sid) { throw 'no session id' }
$report.session = $sid
Write-Host ("SESSION={0}" -f $sid)

# Unique token so FTS/transcript search is deterministic.
$marker = "phase1marker$(Get-Date -Format 'HHmmss')"
$prompt = @(
  'Call exactly one available local/terminal tool to run this command and do not invent the output:'
  "echo $marker"
  'Then reply in one short sentence with the command output. Do nothing else.'
) -join "`n"

Write-Host '=== stream chat (expect >=1 tool) ==='
$out = Send-SSE $sid $prompt 240
$report.tool_names = @($out.Tools | Select-Object -Unique)
$report.tool_events = $out.Tools.Count
$report.reply_len = $out.Text.Length
Write-Host ('tools_events={0} unique={1}' -f $out.Tools.Count, ($report.tool_names -join ','))
$snippetLen = [Math]::Min(160, $out.Text.Length)
if ($snippetLen -gt 0) {
  Write-Host ('reply_snippet={0}' -f $out.Text.Substring(0, $snippetLen))
} else {
  Write-Host 'reply_snippet=<empty>'
}

Start-Sleep -Seconds 2

Write-Host '=== transcript/search ==='
$q = [uri]::EscapeDataString($marker)
$searchUri = "$base/api/v1/agents/$agent/transcript/search?q=$q&include_tools=1&window=3"
try {
  $search = Invoke-RestMethod -Uri $searchUri -Headers @{ Authorization = "Bearer $tok" }
  $report.transcript_count = $search.count
  $report.transcript_hits = @()
  if ($search.hits) {
    foreach ($hit in $search.hits) {
      $anchorRole = $hit.anchor.role
      $anchorTool = $hit.anchor.tool_name
      if (-not $anchorTool) { $anchorTool = $hit.anchor.toolName }
      $content = [string]$hit.anchor.content
      $clen = [Math]::Min(120, $content.Length)
      $report.transcript_hits += [pscustomobject]@{
        session = $hit.session_id
        role    = $anchorRole
        tool    = $anchorTool
        content = $(if ($clen -gt 0) { $content.Substring(0, $clen) } else { '' })
      }
    }
  }
  Write-Host ('transcript count={0}' -f $search.count)
  $search | ConvertTo-Json -Depth 6 | Write-Host
} catch {
  $report.transcript_error = $_.Exception.Message
  Write-Host ('transcript/search FAILED: {0}' -f $_.Exception.Message)
}

Write-Host '=== log scan (turn_trace / bg_review) ==='
$logFiles = @(
  'D:\workspace\github\sixath\portal\bin\backend_phase2.out.log',
  'D:\workspace\github\sixath\portal\bin\backend_phase2.err.log',
  'D:\workspace\github\sixath\portal\bin\backend_phase1.out.log',
  'D:\workspace\github\sixath\portal\bin\backend_phase1.err.log'
)
$patterns = @('turn_trace', 'growth_bg_review', 'BackgroundReview', 'PersistTurnTrace', 'transcript', 'SpawnBackgroundReview')
$hits = @()
foreach ($f in $logFiles) {
  if (Test-Path $f) {
    $hits += Select-String -Path $f -Pattern ($patterns -join '|') -SimpleMatch:$false | Select-Object -Last 30
  }
}
$report.log_hits = @($hits | ForEach-Object {
  $line = $_.Line
  $llen = [Math]::Min(220, $line.Length)
  if ($llen -gt 0) { $line.Substring(0, $llen) } else { '' }
})
$hits | ForEach-Object {
  $line = $_.Line
  $llen = [Math]::Min(220, $line.Length)
  if ($llen -gt 0) { Write-Host $line.Substring(0, $llen) }
}

$outPath = Join-Path $PSScriptRoot 'trajectory_phase1_live_out.json'
($report | ConvertTo-Json -Depth 8) | Set-Content -Path $outPath -Encoding UTF8
Write-Host ('REPORT={0}' -f $outPath)

# Assertions: durable path (transcript tool projection) is the Phase1 success signal.
# SSE tool_events is secondary (parser can lag); transcript/search proves G1+G2.
$ok = $true
$toolHit = $false
foreach ($h in @($report.transcript_hits)) {
  if ($h.role -eq 'tool' -and ([string]$h.content).Contains($marker)) { $toolHit = $true }
}
if ($report.transcript_error) {
  Write-Host 'FAIL: transcript/search error'
  $ok = $false
} elseif (-not $toolHit) {
  Write-Host 'FAIL: transcript/search missing role=tool anchor containing marker'
  $ok = $false
} else {
  Write-Host 'PASS: transcript/search hit role=tool with marker (G1 persist + FTS + G2)'
}
if ($report.tool_events -lt 1) {
  Write-Host 'WARN: SSE tool_events=0 (check event: line parsing); durable path may still pass'
}
$bg = @($report.log_hits | Where-Object { $_ -match 'SpawnBackgroundReview|growth_bg_review' })
if ($bg.Count -gt 0) {
  Write-Host 'PASS: BackgroundReview spawn logged (G3)'
} else {
  Write-Host 'WARN: no SpawnBackgroundReview log line found'
}
if ($ok) { Write-Host 'SMOKE_OK'; exit 0 } else { Write-Host 'SMOKE_FAIL'; exit 1 }
