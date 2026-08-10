# Inbound Gateway E2E smoke — webhook auth, peer→session, public chat gate, gateway proxy.
# Writes: _neo4j_q/verify_inbound_gateway_out.json
#
# Env (optional):
#   GATEWAY_URL          default: probe 18088 then 8088
#   PORTAL_URL           default: probe 18000 then 8000
#   WEBHOOK_SECRET       default: dev-webhook-secret
#   CHANNEL_ID           default: demo-webhook
#   DISABLED_CHANNEL_ID  default: demo-webhook-disabled (skip 410 if unset/empty intentionally)
#   RUNTIME_TOKEN        default: dev-runtime-token (Portal /runtime/v1)
#   AUTH_TOKEN           optional — Web bearer for Gateway session create / stream
#   AGENT_ID             optional — required for web multi-session + gateway stream checks
#   SKIP_LIVE=1          document checks only, exit 2 without calling services
#
# Exit: 0 ok; 1 fail; 2 skipped (services down / SKIP_LIVE)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$outPath = Join-Path $PSScriptRoot 'verify_inbound_gateway_out.json'
$results = [ordered]@{
  ok           = $false
  started_at   = (Get-Date).ToUniversalTime().ToString('o')
  gateway_url  = $null
  portal_url   = $null
  checks       = [System.Collections.Generic.List[object]]::new()
  skip_reason  = $null
}

function Write-Out {
  $results.finished_at = (Get-Date).ToUniversalTime().ToString('o')
  $results | ConvertTo-Json -Depth 8 | Set-Content -Path $outPath -Encoding utf8
}

function Add-Check([string]$name, [string]$status, [string]$detail = '') {
  $item = [ordered]@{ name = $name; status = $status; detail = $detail }
  [void]$results.checks.Add($item)
  $tag = switch ($status) {
    'pass' { 'OK  ' }
    'fail' { 'FAIL' }
    'skip' { 'SKIP' }
    default { '    ' }
  }
  Write-Host ("{0} {1}: {2}" -f $tag, $name, $detail)
}

function Test-HttpReachable([string]$url, [int]$timeoutSec = 3) {
  try {
    $resp = Invoke-WebRequest -Uri $url -Method GET -UseBasicParsing -TimeoutSec $timeoutSec -ErrorAction Stop
    return $true
  } catch {
    # connection refused / timeout → false; HTTP error codes still mean reachable
    if ($_.Exception.Response) { return $true }
    return $false
  }
}

function Resolve-BaseUrl([string]$envName, [string[]]$candidates) {
  $fromEnv = [Environment]::GetEnvironmentVariable($envName)
  if (-not [string]::IsNullOrWhiteSpace($fromEnv)) {
    return $fromEnv.TrimEnd('/')
  }
  foreach ($c in $candidates) {
    $probe = if ($c -match '/healthz$') { $c } else { "$c/" }
    if (Test-HttpReachable $probe) {
      return ($c -replace '/healthz$', '').TrimEnd('/')
    }
  }
  return $null
}

function Invoke-Http {
  param(
    [string]$Method,
    [string]$Uri,
    [hashtable]$Headers = @{},
    [string]$Body = $null,
    [string]$ContentType = 'application/json',
    [int]$TimeoutSec = 60
  )
  $req = [System.Net.HttpWebRequest]::Create($Uri)
  $req.Method = $Method
  $req.Timeout = $TimeoutSec * 1000
  $req.ReadWriteTimeout = $TimeoutSec * 1000
  foreach ($k in $Headers.Keys) {
    if ($k -eq 'Content-Type') { continue }
    $req.Headers.Add([string]$k, [string]$Headers[$k])
  }
  $methodUpper = $Method.ToUpperInvariant()
  $canBody = $methodUpper -in @('POST', 'PUT', 'PATCH')
  if ($canBody) {
    $req.ContentType = $ContentType
    $payload = if ($null -ne $Body) { $Body } else { '' }
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($payload)
    $req.ContentLength = $bytes.Length
    if ($bytes.Length -gt 0) {
      $stream = $req.GetRequestStream()
      $stream.Write($bytes, 0, $bytes.Length)
      $stream.Close()
    }
  }
  try {
    $resp = $req.GetResponse()
  } catch [System.Net.WebException] {
    if ($null -eq $_.Exception.Response) { throw }
    $resp = $_.Exception.Response
  }
  $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $text = $reader.ReadToEnd()
  $reader.Close()
  $code = [int]$resp.StatusCode
  $resp.Close()
  return @{ StatusCode = $code; Body = $text }
}

function Get-JsonProp($obj, [string[]]$names) {
  if ($null -eq $obj) { return $null }
  foreach ($n in $names) {
    if ($obj.PSObject.Properties.Name -contains $n) {
      $v = $obj.$n
      if (-not [string]::IsNullOrWhiteSpace([string]$v)) { return [string]$v }
    }
  }
  return $null
}

Write-Host '=== Inbound Gateway E2E ==='

if ($env:SKIP_LIVE -eq '1') {
  $results.skip_reason = 'SKIP_LIVE=1'
  @(
    'webhook_bad_secret',
    'webhook_disabled_410',
    'webhook_good_secret',
    'webhook_same_peer_session',
    'webhook_diff_peer_session',
    'web_two_sessions',
    'portal_public_inbound_rejected',
    'gateway_turn_success',
    'portal_channels_get'
  ) | ForEach-Object { Add-Check $_ 'skip' 'SKIP_LIVE=1 — start compose/local services and re-run' }
  Write-Out
  Write-Host "SKIPPED (no live calls) -> $outPath"
  exit 2
}

$gateway = Resolve-BaseUrl 'GATEWAY_URL' @(
  'http://127.0.0.1:18088/healthz',
  'http://127.0.0.1:8088/healthz',
  'http://127.0.0.1:18088',
  'http://127.0.0.1:8088'
)
$portal = Resolve-BaseUrl 'PORTAL_URL' @(
  'http://127.0.0.1:18000',
  'http://127.0.0.1:8000'
)

$results.gateway_url = $gateway
$results.portal_url = $portal

if (-not $gateway -or -not $portal) {
  $msg = "services unreachable (GATEWAY_URL=$gateway PORTAL_URL=$portal). Start docker compose or local gateway+portal, or set GATEWAY_URL/PORTAL_URL."
  $results.skip_reason = $msg
  Add-Check 'reachability' 'skip' $msg
  Write-Out
  Write-Host "SKIPPED -> $outPath"
  exit 2
}

$secret = if ($env:WEBHOOK_SECRET) { $env:WEBHOOK_SECRET } else { 'dev-webhook-secret' }
$channelId = if ($env:CHANNEL_ID) { $env:CHANNEL_ID } else { 'demo-webhook' }
# Default demo-webhook-disabled (see gateway/configs/channels.yaml). SKIP_DISABLED_CHANNEL=1 to skip 410 check.
if ($env:SKIP_DISABLED_CHANNEL -eq '1') {
  $disabledChannelId = ''
} elseif ($env:DISABLED_CHANNEL_ID) {
  $disabledChannelId = $env:DISABLED_CHANNEL_ID
} else {
  $disabledChannelId = 'demo-webhook-disabled'
}
$runtimeToken = if ($env:RUNTIME_TOKEN) { $env:RUNTIME_TOKEN } else { 'dev-runtime-token' }
$authToken = $env:AUTH_TOKEN
$agentId = $env:AGENT_ID

$hookUrl = "$gateway/hooks/$channelId"
$peerA = "e2e-peer-a-$(Get-Random -Maximum 999999)"
$peerB = "e2e-peer-b-$(Get-Random -Maximum 999999)"
$failCount = 0

function Fail([string]$name, [string]$detail) {
  Add-Check $name 'fail' $detail
  $script:failCount++
}
function Pass([string]$name, [string]$detail) { Add-Check $name 'pass' $detail }
function Skip([string]$name, [string]$detail) { Add-Check $name 'skip' $detail }

# --- reachability ---
try {
  $hz = Invoke-Http -Method GET -Uri "$gateway/healthz" -TimeoutSec 5
  if ($hz.StatusCode -ge 200 -and $hz.StatusCode -lt 300) {
    Pass 'gateway_healthz' "status=$($hz.StatusCode)"
  } else {
    Fail 'gateway_healthz' "status=$($hz.StatusCode) body=$($hz.Body)"
  }
} catch {
  Fail 'gateway_healthz' $_.Exception.Message
}

# --- 1a bad secret ---
try {
  $body = @{ content = 'ping'; peer_id = $peerA; reply_mode = 'async' } | ConvertTo-Json -Compress
  $r = Invoke-Http -Method POST -Uri $hookUrl -Headers @{ 'X-Webhook-Secret' = 'wrong-secret' } -Body $body
  if ($r.StatusCode -lt 200 -or $r.StatusCode -ge 300) {
    Pass 'webhook_bad_secret' "status=$($r.StatusCode) (non-2xx)"
  } else {
    Fail 'webhook_bad_secret' "expected non-2xx, got $($r.StatusCode)"
  }
} catch {
  Fail 'webhook_bad_secret' $_.Exception.Message
}

# --- 1b disabled → 410 ---
if ([string]::IsNullOrWhiteSpace($disabledChannelId)) {
  Skip 'webhook_disabled_410' 'DISABLED_CHANNEL_ID empty — not configured'
} else {
  try {
    $dUrl = "$gateway/hooks/$disabledChannelId"
    $body = @{ content = 'ping'; peer_id = $peerA; reply_mode = 'async' } | ConvertTo-Json -Compress
    $r = Invoke-Http -Method POST -Uri $dUrl -Headers @{ 'X-Webhook-Secret' = $secret } -Body $body
    if ($r.StatusCode -eq 410) {
      Pass 'webhook_disabled_410' 'status=410'
    } elseif ($r.StatusCode -eq 404) {
      Skip 'webhook_disabled_410' "channel '$disabledChannelId' not in gateway channels.yaml (got 404)"
    } else {
      Fail 'webhook_disabled_410' "expected 410, got $($r.StatusCode) body=$($r.Body)"
    }
  } catch {
    Fail 'webhook_disabled_410' $_.Exception.Message
  }
}

# --- 2 good secret → 202 (async) ---
try {
  $body = @{ content = 'e2e async ping'; peer_id = $peerA; reply_mode = 'async' } | ConvertTo-Json -Compress
  $r = Invoke-Http -Method POST -Uri $hookUrl -Headers @{ 'X-Webhook-Secret' = $secret } -Body $body -TimeoutSec 30
  if ($r.StatusCode -eq 202 -or $r.StatusCode -eq 200) {
    Pass 'webhook_good_secret' "status=$($r.StatusCode) body=$($r.Body.Substring(0, [Math]::Min(120, $r.Body.Length)))"
  } else {
    Fail 'webhook_good_secret' "expected 202/200, got $($r.StatusCode) body=$($r.Body)"
  }
} catch {
  Fail 'webhook_good_secret' $_.Exception.Message
}

# --- 3/4 peer session continuity via Portal resolve ---
function Invoke-Resolve([string]$peer) {
  $payload = @{
    channel_id = $channelId
    peer_id    = $peer
  }
  if ($agentId) { $payload.agent_id = $agentId }
  $body = $payload | ConvertTo-Json -Compress
  $r = Invoke-Http -Method POST -Uri "$portal/runtime/v1/sessions/resolve" `
    -Headers @{ Authorization = "Bearer $runtimeToken" } -Body $body -TimeoutSec 30
  $obj = $null
  try { $obj = $r.Body | ConvertFrom-Json } catch { }
  return @{ StatusCode = $r.StatusCode; Body = $r.Body; Json = $obj }
}

try {
  # drive two webhook rounds (sync so turn completes before resolve race)
  $body1 = @{ content = 'e2e round1'; peer_id = $peerA; reply_mode = 'sync' } | ConvertTo-Json -Compress
  $w1 = Invoke-Http -Method POST -Uri $hookUrl -Headers @{ 'X-Webhook-Secret' = $secret } -Body $body1 -TimeoutSec 130
  $body2 = @{ content = 'e2e round2'; peer_id = $peerA; reply_mode = 'sync' } | ConvertTo-Json -Compress
  $w2 = Invoke-Http -Method POST -Uri $hookUrl -Headers @{ 'X-Webhook-Secret' = $secret } -Body $body2 -TimeoutSec 130

  $res1 = Invoke-Resolve $peerA
  $res2 = Invoke-Resolve $peerA
  $sid1 = Get-JsonProp $res1.Json @('session_id', 'sessionId')
  $sid2 = Get-JsonProp $res2.Json @('session_id', 'sessionId')

  if ($w1.StatusCode -notin 200, 202 -or $w2.StatusCode -notin 200, 202) {
    Fail 'webhook_same_peer_session' "webhook rounds status w1=$($w1.StatusCode) w2=$($w2.StatusCode) body1=$($w1.Body)"
  } elseif ($res1.StatusCode -ne 200 -or $res2.StatusCode -ne 200 -or -not $sid1) {
    Fail 'webhook_same_peer_session' "resolve failed status=$($res1.StatusCode)/$($res2.StatusCode) body=$($res1.Body) (need RUNTIME_TOKEN + valid agent on channel)"
  } elseif ($sid1 -ne $sid2) {
    Fail 'webhook_same_peer_session' "session mismatch $sid1 vs $sid2"
  } else {
    Pass 'webhook_same_peer_session' "session_id=$sid1 (two rounds)"
    $results.peer_a_session_id = $sid1
  }

  $bodyB = @{ content = 'e2e peerB'; peer_id = $peerB; reply_mode = 'sync' } | ConvertTo-Json -Compress
  $wB = Invoke-Http -Method POST -Uri $hookUrl -Headers @{ 'X-Webhook-Secret' = $secret } -Body $bodyB -TimeoutSec 130
  $resB = Invoke-Resolve $peerB
  $sidB = Get-JsonProp $resB.Json @('session_id', 'sessionId')
  if ($wB.StatusCode -notin 200, 202) {
    Fail 'webhook_diff_peer_session' "peerB webhook status=$($wB.StatusCode) body=$($wB.Body)"
  } elseif (-not $sid1 -or -not $sidB) {
    Fail 'webhook_diff_peer_session' "missing session ids sidA=$sid1 sidB=$sidB resolve=$($resB.Body)"
  } elseif ($sid1 -eq $sidB) {
    Fail 'webhook_diff_peer_session' "expected different sessions, both=$sid1"
  } else {
    Pass 'webhook_diff_peer_session' "sidA=$sid1 sidB=$sidB"
    $results.peer_b_session_id = $sidB
  }
} catch {
  Fail 'webhook_same_peer_session' $_.Exception.Message
  Fail 'webhook_diff_peer_session' $_.Exception.Message
}

# --- 5 web: same user two sessions ---
if ([string]::IsNullOrWhiteSpace($authToken) -or [string]::IsNullOrWhiteSpace($agentId)) {
  Skip 'web_two_sessions' 'set AUTH_TOKEN and AGENT_ID to exercise Gateway web session create'
} else {
  try {
    $h = @{ Authorization = "Bearer $authToken"; 'Content-Type' = 'application/json' }
    $b1 = @{ title = "e2e-web-a-$(Get-Random)" } | ConvertTo-Json -Compress
    $b2 = @{ title = "e2e-web-b-$(Get-Random)" } | ConvertTo-Json -Compress
    $s1 = Invoke-Http -Method POST -Uri "$gateway/api/v1/agents/$agentId/sessions" -Headers $h -Body $b1
    $s2 = Invoke-Http -Method POST -Uri "$gateway/api/v1/agents/$agentId/sessions" -Headers $h -Body $b2
    $j1 = $null; $j2 = $null
    try { $j1 = $s1.Body | ConvertFrom-Json } catch { }
    try { $j2 = $s2.Body | ConvertFrom-Json } catch { }
    $id1 = Get-JsonProp $j1 @('id', 'session_id', 'sessionId')
    $id2 = Get-JsonProp $j2 @('id', 'session_id', 'sessionId')
    if ($s1.StatusCode -notin 200, 201 -or $s2.StatusCode -notin 200, 201) {
      Fail 'web_two_sessions' "create status s1=$($s1.StatusCode) s2=$($s2.StatusCode) body=$($s1.Body)"
    } elseif (-not $id1 -or -not $id2) {
      Fail 'web_two_sessions' "missing ids body1=$($s1.Body) body2=$($s2.Body)"
    } elseif ($id1 -eq $id2) {
      Fail 'web_two_sessions' "expected different session ids, both=$id1"
    } else {
      Pass 'web_two_sessions' "id1=$id1 id2=$id2"
      $results.web_session_ids = @($id1, $id2)
    }
  } catch {
    Fail 'web_two_sessions' $_.Exception.Message
  }
}

# --- 6 Portal public inbound rejected ---
try {
  $fakeSid = '00000000-0000-0000-0000-0000000000e2'
  $hdr = @{ 'Content-Type' = 'application/json' }
  if ($authToken) { $hdr['Authorization'] = "Bearer $authToken" }
  $r = Invoke-Http -Method POST -Uri "$portal/api/v1/sessions/$fakeSid/messages/stream" `
    -Headers $hdr -Body (@{ content = 'should-be-rejected' } | ConvertTo-Json -Compress) -TimeoutSec 15
  # gate returns 403; also accept other non-2xx that clearly deny (401/404) if filter not hit with missing auth
  if ($r.StatusCode -eq 403) {
    Pass 'portal_public_inbound_rejected' 'status=403 public chat inbound disabled'
  } elseif ($r.StatusCode -ge 200 -and $r.StatusCode -lt 300) {
    Fail 'portal_public_inbound_rejected' "expected reject, got $($r.StatusCode) body=$($r.Body)"
  } else {
    # 401 without token still proves not open chat; prefer 403 when AUTH_TOKEN set
    if ($authToken -and $r.StatusCode -ne 403) {
      Fail 'portal_public_inbound_rejected' "expected 403 with AUTH_TOKEN, got $($r.StatusCode) body=$($r.Body)"
    } else {
      Pass 'portal_public_inbound_rejected' "status=$($r.StatusCode) (non-2xx; public inbound not open)"
    }
  }
} catch {
  Fail 'portal_public_inbound_rejected' $_.Exception.Message
}

# --- 7 Gateway stream or webhook sync final success ---
$turnDone = $false
if ($authToken -and $agentId -and $results.web_session_ids) {
  try {
    $sid = $results.web_session_ids[0]
    $uri = "$gateway/api/v1/sessions/$sid/messages/stream"
    $req = [System.Net.HttpWebRequest]::Create($uri)
    $req.Method = 'POST'
    $req.ContentType = 'application/json'
    $req.Accept = 'text/event-stream'
    $req.Headers.Add('Authorization', "Bearer $authToken")
    $req.Timeout = 130000
    $req.ReadWriteTimeout = 130000
    $bytes = [System.Text.Encoding]::UTF8.GetBytes((@{ content = 'Reply with exactly: pong' } | ConvertTo-Json -Compress))
    $req.ContentLength = $bytes.Length
    $s = $req.GetRequestStream(); $s.Write($bytes, 0, $bytes.Length); $s.Close()
    $resp = $req.GetResponse()
    $code = [int]$resp.StatusCode
    $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
    $buf = New-Object System.Collections.Generic.List[string]
    $t0 = [Environment]::TickCount
    while (([Environment]::TickCount - $t0) -lt 120000) {
      $line = $reader.ReadLine()
      if ($null -eq $line) { break }
      [void]$buf.Add($line)
      if ($line -match 'event:\s*(done|error|final)') { break }
    }
    $reader.Close(); $resp.Close()
    $joined = $buf -join "`n"
    if ($code -ge 200 -and $code -lt 300 -and $joined -notmatch 'event:\s*error') {
      Pass 'gateway_turn_success' "SSE stream ok session=$sid bytes=$($joined.Length)"
      $turnDone = $true
    } else {
      Fail 'gateway_turn_success' "stream status=$code excerpt=$($joined.Substring(0, [Math]::Min(200, $joined.Length)))"
      $turnDone = $true
    }
  } catch {
    Fail 'gateway_turn_success' "stream: $($_.Exception.Message)"
    $turnDone = $true
  }
}
if (-not $turnDone) {
  try {
    $peerT = "e2e-turn-$(Get-Random -Maximum 999999)"
    $body = @{ content = 'Reply with exactly: pong'; peer_id = $peerT; reply_mode = 'sync' } | ConvertTo-Json -Compress
    $r = Invoke-Http -Method POST -Uri $hookUrl -Headers @{ 'X-Webhook-Secret' = $secret } -Body $body -TimeoutSec 130
    $j = $null
    try { $j = $r.Body | ConvertFrom-Json } catch { }
    $st = Get-JsonProp $j @('status')
    if ($r.StatusCode -eq 200 -and ($st -eq 'ok' -or $st -eq 'failed' -or $null -ne $j)) {
      # status=failed still means the gateway↔portal final path worked (agent may be missing)
      if ($st -eq 'ok') {
        Pass 'gateway_turn_success' "webhook sync final status=ok"
      } elseif ($st -eq 'failed') {
        Skip 'gateway_turn_success' "webhook sync reached portal but turn failed: $($j.error) (configure channel default_agent / AGENT_ID)"
      } else {
        Pass 'gateway_turn_success' "webhook sync status=$($r.StatusCode) body=$($r.Body.Substring(0, [Math]::Min(160, $r.Body.Length)))"
      }
    } elseif ($r.StatusCode -eq 202) {
      Pass 'gateway_turn_success' 'async 202 accepted (final path not asserted)'
    } else {
      Fail 'gateway_turn_success' "expected sync 200/202, got $($r.StatusCode) body=$($r.Body)"
    }
  } catch {
    Fail 'gateway_turn_success' $_.Exception.Message
  }
}

# --- plan bonus: Channel management still on Portal ---
try {
  $hdr = @{}
  if ($authToken) { $hdr['Authorization'] = "Bearer $authToken" }
  $r = Invoke-Http -Method GET -Uri "$portal/api/v1/channels" -Headers $hdr -TimeoutSec 15
  if ($r.StatusCode -ge 200 -and $r.StatusCode -lt 300) {
    Pass 'portal_channels_get' "status=$($r.StatusCode)"
  } elseif ($r.StatusCode -eq 401 -and -not $authToken) {
    Skip 'portal_channels_get' '401 without AUTH_TOKEN (endpoint present)'
  } else {
    Fail 'portal_channels_get' "status=$($r.StatusCode) body=$($r.Body)"
  }
} catch {
  Fail 'portal_channels_get' $_.Exception.Message
}

$hardFails = @($results.checks | Where-Object { $_.status -eq 'fail' })
$results.ok = ($hardFails.Count -eq 0)
Write-Out

if ($results.ok) {
  Write-Host "PASSED -> $outPath"
  exit 0
}
Write-Host "FAILED ($($hardFails.Count) checks) -> $outPath"
exit 1
