# Knowledge-surface E2E (local only): wiki + codegraph + neo4j graph via chat tools.
# Each source uses a fresh session so the model reliably calls knowledge_search.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$tok = 'dev-bootstrap-token'
$base = 'http://localhost:8000'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$agent = 'e8107fb3-e40a-4207-9d9a-6768847aaf79'
$outPath = Join-Path $PSScriptRoot 'verify_knowledge_e2e_out.json'
$results = [ordered]@{ ok = $false; checks = @() }

function Assert-True([bool]$cond, [string]$msg) {
  if (-not $cond) { throw "ASSERT: $msg" }
  Write-Host "OK  $msg"
  $script:results.checks += $msg
}

function Invoke-Json([string]$method, [string]$url, $body = $null) {
  $params = @{ Uri = $url; Method = $method; Headers = $h }
  if ($null -ne $body) { $params.Body = ($body | ConvertTo-Json -Depth 8 -Compress) }
  return Invoke-RestMethod @params
}

function Ensure-AgentLocal {
  $ag = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
  $rt = @{
    memoryWriteEnabled        = [bool]$ag.runtimeTools.memoryWriteEnabled
    skillRuntimeManageEnabled = [bool]$ag.runtimeTools.skillRuntimeManageEnabled
    todoEnabled               = [bool]$ag.runtimeTools.todoEnabled
    workspaceFilesEnabled     = [bool]$ag.runtimeTools.workspaceFilesEnabled
    webToolsEnabled           = [bool]$ag.runtimeTools.webToolsEnabled
    terminalLocalEnabled      = [bool]$ag.runtimeTools.terminalLocalEnabled
    cronjobToolEnabled        = [bool]$ag.runtimeTools.cronjobToolEnabled
    browserEnabled            = [bool]$ag.runtimeTools.browserEnabled
    hub_governance            = 'local'
    hub_knowledge             = 'local'
  }
  Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Method PUT -Headers $h -Body (@{ runtime_tools = $rt } | ConvertTo-Json -Depth 8 -Compress) | Out-Null
  $ag2 = Invoke-RestMethod -Uri "$base/api/v1/agents/$agent" -Headers @{ Authorization = "Bearer $tok" }
  Assert-True ([string]$ag2.runtimeTools.hubKnowledge -eq 'local') 'agent hubKnowledge=local'
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
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
  }
  $reader.Close(); $resp.Close()
}

function Get-AllKnowledgeTools([string]$sessionId) {
  $msgs = Invoke-RestMethod -Uri "$base/api/v1/sessions/$sessionId/messages" -Headers @{ Authorization = "Bearer $tok" }
  $out = @()
  foreach ($m in @($msgs.items)) {
    if ($m.role -ne 'assistant' -or -not $m.metadata -or -not $m.metadata.timeline) { continue }
    foreach ($t in @($m.metadata.timeline)) {
      if ($t.toolName -eq 'knowledge_search' -or $t.toolName -eq 'knowledge_read') {
        $out += $t
      }
    }
  }
  return $out
}

function New-KnowSession([string]$title) {
  $session = Invoke-Json POST "$base/api/v1/agents/$agent/sessions" @{ title = $title }
  $sid = $session.id
  if ([string]::IsNullOrWhiteSpace($sid)) { throw "no session id for $title" }
  return $sid
}

function Test-KnowSource([string]$name, [string]$sid, [string]$prompt, [string]$wantSource, [string]$wantPattern) {
  Write-Host "=== $name (session=$sid) ==="
  Send-SSE $sid $prompt 150
  $tl = Get-AllKnowledgeTools $sid
  $results["${name}_timeline"] = @($tl | ForEach-Object {
    [ordered]@{ tool = $_.toolName; args = $_.arguments; result = $_.result; phase = $_.phase }
  })
  $hit = $false
  foreach ($t in $tl) {
    if ($t.toolName -ne 'knowledge_search') { continue }
    if ([string]$t.arguments.source -ne $wantSource) { continue }
    if ($null -eq $t.result) { continue }
    $res = ($t.result | ConvertTo-Json -Depth 8 -Compress)
    if (-not $res -or $res -eq 'null') { continue }
    if ($res -match [regex]::Escape('"source":"' + $wantSource + '"') -or $res -match ("source.=.$wantSource")) {
      if ($res -match $wantPattern) { $hit = $true }
    }
    # also accept array hits without nested escape quirks
    if ($res -match $wantPattern -and $res -match $wantSource) { $hit = $true }
  }
  Assert-True $hit "$name knowledge_search hit ($wantPattern)"
}

Write-Host '=== 0. catalog / agent local ==='
$cat = Invoke-Json GET "$base/api/v1/memory-hub/catalog"
Assert-True ($cat.defaults.knowledge -eq 'local') 'catalog defaults.knowledge=local'
Ensure-AgentLocal
$results.catalog = $cat

Write-Host '=== wiki ==='
$wikiSid = New-KnowSession "know-wiki-$(Get-Date -Format HHmmss)"
$results.wiki_session = $wikiSid
Test-KnowSource 'wiki' $wikiSid `
  'ONLY call knowledge_search once. EXACT args: query=MEMORY_HUB_WIKI_MARKER, source=wiki, limit=3. Paste raw tool JSON. No other tools.' `
  'wiki' 'hub-e2e\.md'

Write-Host '=== codegraph ==='
$cgSid = New-KnowSession "know-cg-$(Get-Date -Format HHmmss)"
$results.codegraph_session = $cgSid
Test-KnowSource 'codegraph' $cgSid `
  'ONLY call knowledge_search once. EXACT args: query=NewNeo4jGraphStore, source=codegraph, limit=3. Paste raw tool JSON. No other tools.' `
  'codegraph' 'NewNeo4jGraphStore'

Write-Host '=== graph (neo4j) ==='
$graphSid = New-KnowSession "know-graph-$(Get-Date -Format HHmmss)"
$results.graph_session = $graphSid
Push-Location 'd:\workspace\github\sixath\portal'
try {
  $env:NEO4J_PASSWORD = if ($env:NEO4J_PASSWORD) { $env:NEO4J_PASSWORD } else { 'jw123456' }
  $seedOut = & go run .\scripts\seed_hub_graph.go $graphSid 2>&1 | Out-String
  Write-Host $seedOut.Trim()
  Assert-True ($seedOut -match 'SEEDED') 'neo4j graph seeded'
  $results.graph_seed = $seedOut.Trim()
} finally {
  Pop-Location
}
Test-KnowSource 'graph' $graphSid `
  'ONLY call knowledge_search once. EXACT args: query=KnowledgeE2EAlpha, source=graph, limit=5. Paste raw tool JSON. No other tools.' `
  'graph' 'KnowledgeE2E'

$results.ok = $true
$results | ConvertTo-Json -Depth 10 | Set-Content -Path $outPath -Encoding UTF8
Write-Host "WROTE $outPath"
Write-Host '=== KNOWLEDGE E2E PASSED ==='
