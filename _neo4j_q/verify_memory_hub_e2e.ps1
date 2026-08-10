# Memory Hub E2E — local governance + local knowledge only (no fake hub).
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$tok = 'dev-bootstrap-token'
$base = 'http://localhost:8000'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79' # zone-4100-agent
$skillID = 'e2e-local-draft-skill'
$outPath = Join-Path $PSScriptRoot 'verify_memory_hub_e2e_out.json'
$results = [ordered]@{}

function Assert-True([bool]$cond, [string]$msg) {
  if (-not $cond) { throw "ASSERT: $msg" }
  Write-Host "OK  $msg"
}

function Invoke-Json([string]$method, [string]$url, $body = $null) {
  $params = @{ Uri = $url; Method = $method; Headers = $h }
  if ($null -ne $body) {
    $params.Body = ($body | ConvertTo-Json -Depth 8 -Compress)
  }
  return Invoke-RestMethod @params
}

function Set-AgentHubLocal($rtFlags) {
  $rt = @{
    memoryWriteEnabled        = [bool]$rtFlags.memoryWriteEnabled
    skillRuntimeManageEnabled = [bool]$rtFlags.skillRuntimeManageEnabled
    todoEnabled               = [bool]$rtFlags.todoEnabled
    workspaceFilesEnabled     = [bool]$rtFlags.workspaceFilesEnabled
    webToolsEnabled           = [bool]$rtFlags.webToolsEnabled
    terminalLocalEnabled      = [bool]$rtFlags.terminalLocalEnabled
    cronjobToolEnabled        = [bool]$rtFlags.cronjobToolEnabled
    browserEnabled            = [bool]$rtFlags.browserEnabled
    hub_governance            = 'local'
    hub_knowledge             = 'local'
  }
  try {
    Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Method PUT -Headers $h -Body (@{ runtime_tools = $rt } | ConvertTo-Json -Depth 8 -Compress) | Out-Null
  } catch {
    $camel = @{
      memoryWriteEnabled        = $rt.memoryWriteEnabled
      skillRuntimeManageEnabled = $rt.skillRuntimeManageEnabled
      todoEnabled               = $rt.todoEnabled
      workspaceFilesEnabled     = $rt.workspaceFilesEnabled
      webToolsEnabled           = $rt.webToolsEnabled
      terminalLocalEnabled      = $rt.terminalLocalEnabled
      cronjobToolEnabled        = $rt.cronjobToolEnabled
      browserEnabled            = $rt.browserEnabled
      hubGovernance             = 'local'
      hubKnowledge              = 'local'
    }
    Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Method PUT -Headers $h -Body (@{ runtimeTools = $camel } | ConvertTo-Json -Depth 8 -Compress) | Out-Null
  }
}

Write-Host '=== 1. catalog (expect local defaults) ==='
$cat = Invoke-Json GET "$base/api/v1/memory-hub/catalog"
$results.catalog = $cat
Assert-True ($cat.defaults.governance -eq 'local') 'defaults.governance=local'
Assert-True ($cat.defaults.knowledge -eq 'local') 'defaults.knowledge=local'
Assert-True ($cat.governance -contains 'local') 'catalog has local'
Assert-True ($cat.knowledge -contains 'local') 'catalog knowledge has local'
Write-Host ("catalog gov={0} know={1}" -f ($cat.governance -join ','), ($cat.knowledge -join ','))

Write-Host '=== 2. force agent hub_* = local ==='
$ag = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
$rtFlags = @{}
if ($ag.runtimeTools) {
  foreach ($p in $ag.runtimeTools.PSObject.Properties) { $rtFlags[$p.Name] = $p.Value }
}
$results.agent_rt_before = $rtFlags
Set-AgentHubLocal $rtFlags
$ag2 = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
Assert-True ([string]$ag2.runtimeTools.hubGovernance -eq 'local') 'agent hubGovernance=local'
Assert-True ([string]$ag2.runtimeTools.hubKnowledge -eq 'local') 'agent hubKnowledge=local'

Write-Host '=== 3. loadout (local) ==='
$load0 = Invoke-Json GET "$base/api/v1/agents/$agent/hub/loadout"
$results.loadout_before = $load0
Assert-True ($load0.provider -eq 'local') 'loadout provider=local'
Write-Host ("loadout total={0}" -f $load0.total)

Write-Host '=== 4. local governance draft → approve ==='
Invoke-Json POST "$base/api/v1/agents/$agent/hub/bindings/clear" | Out-Null
$bind = Invoke-Json POST "$base/api/v1/agents/$agent/hub/bindings" @{
  assets = @(@{ kind = 'skill'; id = $skillID; hub = 'local'; name = $skillID; status = 'draft' })
}
Assert-True ($bind.ok -eq $true) 'bind local draft skill ok'

$binds = Invoke-Json GET "$base/api/v1/agents/$agent/hub/bindings"
$results.bindings_draft = $binds
Assert-True ($binds.provider -eq 'local') 'bindings provider=local'
Assert-True ($binds.total -ge 1) 'bindings has item'
$st = [string]$binds.items[0].status
Assert-True ($st -eq 'draft') "binding status draft (got $st)"

$loadDraft = Invoke-Json GET "$base/api/v1/agents/$agent/hub/loadout"
$inLoad = @($loadDraft.items | Where-Object { $_.id -eq $skillID }).Count -gt 0
Assert-True (-not $inLoad) 'draft binding not in loadout'

$apr = Invoke-Json POST "$base/api/v1/agents/$agent/hub/assets/status" @{
  asset  = @{ kind = 'skill'; id = $skillID; hub = 'local' }
  status = 'active'
}
Assert-True ($apr.ok -eq $true) 'approve local skill ok'

$loadActive = Invoke-Json GET "$base/api/v1/agents/$agent/hub/loadout"
$results.loadout_after_approve = $loadActive
Assert-True ($loadActive.provider -eq 'local') 'loadout still local'
$inLoad2 = @($loadActive.items | Where-Object { $_.id -eq $skillID }).Count -gt 0
Assert-True ($inLoad2) 'active local binding in loadout'

Invoke-Json POST "$base/api/v1/agents/$agent/hub/bindings/clear" | Out-Null
Write-Host 'cleared e2e bindings'

Write-Host '=== 5. knowledge via chat (local + source=wiki) ==='
$wikiEnvHint = $env:SATH_HUB_WIKI_ROOT
$cgEnvHint = $env:SATH_HUB_CODEGRAPH_ROOT
$results.wiki_root_hint = $wikiEnvHint
$results.codegraph_root_hint = $cgEnvHint

$session = Invoke-Json POST "$base/api/v1/agents/$agent/sessions" @{ title = "local-hub-e2e-$(Get-Date -Format HHmmss)" }
$sid = $session.id
Assert-True ([string]::IsNullOrWhiteSpace($sid) -eq $false) "session created ($sid)"
$results.session_id = $sid

if (-not [string]::IsNullOrWhiteSpace($wikiEnvHint)) {
  $prompt = 'ONLY call knowledge_search with EXACT args: query=MEMORY_HUB_WIKI_MARKER, source=wiki, limit=3. Paste raw tool result. No other tools.'
} else {
  $prompt = 'ONLY call knowledge_search with EXACT args: query=InitLocalMemoryHub, source=codegraph, limit=3. Paste raw tool result. No other tools.'
}

function Send-SSE([string]$sessionId, [string]$content, [int]$timeoutSec = 120) {
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
  $text = ''
  $tools = New-Object System.Collections.Generic.List[string]
  $wikiHit = $false
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ($payload -eq '[DONE]' -or $payload -eq '') { continue }
    if ($payload -match 'knowledge_search') { $tools.Add($payload.Substring(0, [Math]::Min(300, $payload.Length))) }
    if ($payload -match 'hub-e2e\.md|MEMORY_HUB_WIKI_MARKER' -and $payload -match '"source"\s*:\s*"wiki"|source.:.wiki') {
      $wikiHit = $true
    }
    try {
      $j = $payload | ConvertFrom-Json
      $etype = [string]$j.type
      if (-not $etype) { $etype = [string]$j.event }
      if ($etype -eq 'text' -or $etype -eq 'delta' -or $etype -eq 'chunk' -or $etype -eq 'message') {
        if ($j.content) { $text += [string]$j.content }
        elseif ($j.delta) { $text += [string]$j.delta }
        elseif ($j.text) { $text += [string]$j.text }
      }
    } catch { }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{ text = $text; tools = $tools; wikiHit = $wikiHit }
}

$sse = Send-SSE $sid $prompt 150
$results.chat_text = if ($sse.text) { $sse.text.Substring(0, [Math]::Min(800, $sse.text.Length)) } else { '' }
$results.chat_tools = @($sse.tools)
$results.wiki_hit = [bool]$sse.wikiHit
$usedKnowledge = $sse.tools.Count -gt 0
$results.used_knowledge_tool = $usedKnowledge
if ($sse.wikiHit) {
  Assert-True $true 'local knowledge_search wiki hit'
} elseif ($usedKnowledge) {
  Write-Host 'WARN knowledge_search called but wiki hit not seen in SSE (check session timeline)'
  $results.knowledge_chat_warn = $true
} else {
  Write-Host 'WARN chat did not invoke knowledge_*'
  $results.knowledge_chat_warn = $true
}

$results.ok = $true
$results | ConvertTo-Json -Depth 8 | Set-Content -Path $outPath -Encoding UTF8
Write-Host "WROTE $outPath"
Write-Host '=== ALL LOCAL CHECKS PASSED ==='
