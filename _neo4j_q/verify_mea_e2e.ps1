# MEA M0.5 end-to-end verify (UI flag + stream + rules audit)
# Expect: event:mea started/finished, TaskState completed when marker file pre-exists.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$base = 'http://localhost:8000'
$tok = 'dev-bootstrap-token'
$agentId = 'b880051a-a7de-4d91-afea-2ad41269191c' # ops-agent
$dataRoot = 'E:\sixath\portal-data'
$h = @{ Authorization = "Bearer $tok"; 'Content-Type' = 'application/json' }
$outPath = Join-Path $PSScriptRoot 'verify_mea_e2e_out.json'

function Wait-Portal([int]$sec = 60) {
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($sec * 1000)) {
    try {
      $null = Invoke-RestMethod -Uri "$base/api/v1/agents" -Headers @{ Authorization = "Bearer $tok" } -TimeoutSec 3
      return
    } catch { Start-Sleep -Milliseconds 800 }
  }
  throw 'portal not ready'
}

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
  $meaEvents = New-Object System.Collections.Generic.List[object]
  $chunks = New-Object System.Collections.Generic.List[string]
  $errors = New-Object System.Collections.Generic.List[string]
  $raw = New-Object System.Text.StringBuilder
  $curEvent = ''
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    [void]$raw.AppendLine($line)
    if ($line.StartsWith('event:')) {
      $curEvent = $line.Substring(6).Trim()
      continue
    }
    if ($line.StartsWith('data:')) {
      $payload = $line.Substring(5).Trim()
      if ($payload -eq '' -or $payload -eq '[DONE]') { continue }
      if ($curEvent -eq 'mea') {
        try { $meaEvents.Add(($payload | ConvertFrom-Json)) } catch { $meaEvents.Add(@{ raw = $payload }) }
      } elseif ($curEvent -eq 'chunk') {
        try {
          $j = $payload | ConvertFrom-Json
          if ($j.content) { $chunks.Add([string]$j.content) }
        } catch {}
      } elseif ($curEvent -eq 'error') {
        $errors.Add($payload)
      } elseif ($curEvent -eq 'done') {
        break
      }
      $curEvent = ''
    }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{
    Mea      = $meaEvents
    Chunks   = ($chunks -join '')
    Errors   = $errors
    Raw      = $raw.ToString()
  }
}

Wait-Portal
Write-Host '=== portal ready ==='

# 1) Enable mea_enabled on agent (keep other runtime tools)
$ag = Invoke-RestMethod -Uri "$base/api/v1/agents/$agentId" -Headers @{ Authorization = "Bearer $tok" }
$ws = [string]$ag.workspace
if (-not $ws) { throw 'agent workspace empty' }
New-Item -ItemType Directory -Force -Path $ws | Out-Null
$marker = Join-Path $ws 'mea_e2e_marker.txt'
Set-Content -Path $marker -Value 'mea-e2e-ok' -Encoding UTF8

$rt = $ag.runtimeTools
if (-not $rt) { $rt = @{} }
$rtObj = [ordered]@{
  memoryWriteEnabled        = [bool]$rt.memoryWriteEnabled
  skillRuntimeManageEnabled = [bool]$rt.skillRuntimeManageEnabled
  todoEnabled               = [bool]$rt.todoEnabled
  workspaceFilesEnabled     = $true
  webToolsEnabled           = [bool]$rt.webToolsEnabled
  terminalLocalEnabled      = [bool]$rt.terminalLocalEnabled
  cronjobToolEnabled        = [bool]$rt.cronjobToolEnabled
  browserEnabled            = [bool]$rt.browserEnabled
  meaEnabled                = $true
}
if ($rt.hubGovernance) { $rtObj.hubGovernance = [string]$rt.hubGovernance }
if ($rt.hubKnowledge) { $rtObj.hubKnowledge = [string]$rt.hubKnowledge }

$updateBody = @{
  name          = $ag.name
  description   = $ag.description
  systemPrompt  = $ag.systemPrompt
  workspace     = $ag.workspace
  debugRun      = [bool]$ag.debugRun
  modelConfig   = @{
    provider        = $ag.modelConfig.provider
    model           = $ag.modelConfig.model
    apiKey          = $ag.modelConfig.apiKey
    baseUrl         = $ag.modelConfig.baseUrl
    maxOutputTokens = [int]$ag.modelConfig.maxOutputTokens
  }
  runtimeTools  = $rtObj
  wecomChannelId = [string]$ag.wecomChannelId
} | ConvertTo-Json -Depth 6
Invoke-RestMethod -Uri "$base/api/v1/agents/$agentId" -Method PUT -Headers $h -Body $updateBody | Out-Null
$ag2 = Invoke-RestMethod -Uri "$base/api/v1/agents/$agentId" -Headers @{ Authorization = "Bearer $tok" }
if (-not $ag2.runtimeTools.meaEnabled) { throw 'PUT meaEnabled failed' }
Write-Host '=== agent meaEnabled=true ==='

$sess = Invoke-RestMethod -Uri "$base/api/v1/agents/$agentId/sessions" -Method POST -Headers $h -Body (@{ title = "mea-e2e-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$sid = $sess.id
if (-not $sid) { throw 'no session id' }
Write-Host ("SESSION={0}" -f $sid)

$fence = [string]::new([char]96, 3)
$content = @(
  'Confirm workspace file mea_e2e_marker.txt exists and contains mea-e2e-ok. Brief reply only; do not modify the file.',
  '',
  ($fence + 'mea-checks'),
  '[',
  '  {"type":"path_exists","path":"mea_e2e_marker.txt"},',
  '  {"type":"file_contains","path":"mea_e2e_marker.txt","pattern":"mea-e2e-ok"}',
  ']',
  $fence
) -join "`n"

Write-Host '=== stream MEA turn ==='
$r = Send-SSE -sessionId $sid -content $content -timeoutSec 240

$phases = @()
foreach ($m in $r.Mea) {
  if ($m.mea -and $m.mea.phase) { $phases += [string]$m.mea.phase }
  elseif ($m.phase) { $phases += [string]$m.phase }
}

$statePath = Join-Path $dataRoot "mea\$sid.json"
$stateOk = $false
$stateStatus = ''
$stateReasonHint = ''
if (Test-Path $statePath) {
  $st = Get-Content $statePath -Raw | ConvertFrom-Json
  if ($st.records -and $st.records.Count -gt 0) {
    $stateStatus = [string]$st.records[0].status
    $stateOk = ($stateStatus -eq 'completed')
  }
  $stateReasonHint = "audits=$($st.audits.Count)"
}

# Negative control: disable mea and ensure no mea events
$rtObj.meaEnabled = $false
$updateBody2 = @{
  name = $ag.name; description = $ag.description; systemPrompt = $ag.systemPrompt
  workspace = $ag.workspace; debugRun = [bool]$ag.debugRun
  modelConfig = @{
    provider = $ag.modelConfig.provider; model = $ag.modelConfig.model
    apiKey = $ag.modelConfig.apiKey; baseUrl = $ag.modelConfig.baseUrl
    maxOutputTokens = [int]$ag.modelConfig.maxOutputTokens
  }
  runtimeTools = $rtObj; wecomChannelId = [string]$ag.wecomChannelId
} | ConvertTo-Json -Depth 6
Invoke-RestMethod -Uri "$base/api/v1/agents/$agentId" -Method PUT -Headers $h -Body $updateBody2 | Out-Null
$sess2 = Invoke-RestMethod -Uri "$base/api/v1/agents/$agentId/sessions" -Method POST -Headers $h -Body (@{ title = "mea-neg-$(Get-Date -Format HHmmss)" } | ConvertTo-Json)
$r2 = Send-SSE -sessionId $sess2.id -content $content -timeoutSec 180
$negMeaCount = $r2.Mea.Count

$result = [ordered]@{
  ok                 = ($phases -contains 'started') -and ($phases -contains 'finished') -and $stateOk -and ($negMeaCount -eq 0) -and ($r.Errors.Count -eq 0)
  agentId            = $agentId
  sessionId          = $sid
  meaPhases          = $phases
  statePath          = $statePath
  stateExists        = (Test-Path $statePath)
  stateStatus        = $stateStatus
  stateHint          = $stateReasonHint
  chunkPreview       = if ($r.Chunks.Length -gt 200) { $r.Chunks.Substring(0, 200) } else { $r.Chunks }
  streamErrors       = @($r.Errors)
  negativeSessionId  = $sess2.id
  negativeMeaEvents  = $negMeaCount
  expectations       = @(
    'UI mea_enabled + mea-checks → SSE event:mea started/finished',
    'TaskState JSON under data_root/mea/<session>.json status=completed (pre-seeded marker)',
    'mea_enabled=false → zero mea SSE events'
  )
}

$result | ConvertTo-Json -Depth 6 | Set-Content -Path $outPath -Encoding UTF8
$result | ConvertTo-Json -Depth 6
if (-not $result.ok) { exit 1 }
Write-Host 'MEA E2E PASS'
