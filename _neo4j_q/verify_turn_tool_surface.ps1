# E2E: Turn Tool Surface - GitLab-only must not invoke jaeger_trace / RCA tools.
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$tok = "dev-bootstrap-token"
$base = "http://localhost:8000"
$h = @{ Authorization = "Bearer $tok"; "Content-Type" = "application/json" }
$agent = "e8107fb3-e40a-4207-9d9a-6768847aaf79"
$outPath = Join-Path $PSScriptRoot "verify_turn_tool_surface_out.json"
$results = [ordered]@{
  ok = $false
  session = $null
  prompt = $null
  tool_names = @()
  forbidden = @()
  reply_excerpt = ""
  checks = @()
}

function Assert-True([bool]$cond, [string]$msg) {
  if (-not $cond) { throw "ASSERT: $msg" }
  Write-Host "OK  $msg"
  $script:results.checks += $msg
}

function Send-SSE([string]$sessionId, [string]$content, [int]$timeoutSec = 180) {
  $uri = "$base/api/v1/sessions/$sessionId/messages/stream"
  $req = [System.Net.HttpWebRequest]::Create($uri)
  $req.Method = "POST"
  $req.ContentType = "application/json"
  $req.Accept = "text/event-stream"
  $req.Headers.Add("Authorization", "Bearer $tok")
  $req.Timeout = $timeoutSec * 1000
  $req.ReadWriteTimeout = $timeoutSec * 1000
  $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes((@{ content = $content } | ConvertTo-Json -Compress))
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
  $ssePath = Join-Path $PSScriptRoot "verify_turn_tool_surface_sse.txt"
  $lines -join "`n" | Set-Content $ssePath -Encoding utf8
  $joined = $lines -join "`n"
  if ($joined -match "event: error") {
    $snip = $joined.Substring(0, [Math]::Min(500, $joined.Length))
    throw "SSE error: $snip"
  }
  return $ssePath
}

Write-Host "=== Turn Tool Surface E2E ==="
$health = Invoke-WebRequest -Uri "$base/api/v1/agents" -Headers @{ Authorization = "Bearer $tok" } -UseBasicParsing -TimeoutSec 10
Assert-True ($health.StatusCode -eq 200) "API reachable"

$sess = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent/sessions" -Method POST -Headers $h -Body (@{ title = "turn-surface-e2e" } | ConvertTo-Json)
$sid = $sess.id
if (-not $sid) { $sid = $sess.session_id }
Assert-True ([bool]$sid) "session created"
$results.session = $sid
Write-Host "SESSION=$sid"

$prompt = @"
Please answer using GitLab tools only: call whoami and/or list_projects.
Do NOT call jaeger_trace, es_log_query, rca_grep, rca_glob, rca_read, rca_symbol, or any observability tools.
If GitLab auth fails, explain the failure and stop.
"@
$results.prompt = $prompt
Write-Host "Sending prompt..."
Send-SSE $sid $prompt 200 | Out-Null

$msgs = Invoke-RestMethod -Uri "$base/api/v1/sessions/$sid/messages" -Headers @{ Authorization = "Bearer $tok" }
$asst = @($msgs.items | Where-Object { $_.role -eq "assistant" } | Select-Object -Last 1)
Assert-True ($asst.Count -ge 1) "assistant message present"
$content = [string]$asst[0].content
$results.reply_excerpt = $content.Substring(0, [Math]::Min(400, $content.Length))

$toolNames = New-Object System.Collections.Generic.List[string]
foreach ($t in @($asst[0].metadata.timeline)) {
  if ($t.toolName) { [void]$toolNames.Add([string]$t.toolName) }
}
$results.tool_names = @($toolNames)
Write-Host ("tools=" + ($toolNames -join ","))

$forbidden = @("jaeger_trace", "es_log_query", "rca_grep", "rca_glob", "rca_read", "rca_symbol")
$hit = @($toolNames | Where-Object { $forbidden -contains $_ })
$results.forbidden = $hit
Assert-True ($hit.Count -eq 0) ("no RCA/forbidden tools; forbidden=[" + ($hit -join ",") + "] tools=[" + ($toolNames -join ",") + "]")

if ($toolNames.Count -eq 0) {
  $mentionsAuth = $content -match "401|auth|token|GitLab|unauthorized|permission"
  Assert-True $mentionsAuth "no tools but reply mentions GitLab/auth"
} else {
  $hasGitlab = @($toolNames | Where-Object { $_ -match "gitlab|whoami|list_project|get_project|list_projects|get_commit" }).Count -gt 0
  if (-not $hasGitlab) {
    Write-Host "WARN: tools ran but none look like GitLab MCP"
    $results.checks += "WARN: no gitlab-named tools"
  } else {
    Assert-True $true "at least one GitLab-related tool invoked"
  }
}

$results.ok = $true
$results | ConvertTo-Json -Depth 6 | Set-Content $outPath -Encoding utf8
Write-Host "PASSED -> $outPath"
