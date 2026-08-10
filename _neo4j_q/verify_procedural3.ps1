$ErrorActionPreference = 'Stop'
$tok = 'dev-bootstrap-token'
$sid = '9cc73ef0-e288-469e-a264-92b7f8d17f41'

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
  $s = $req.GetRequestStream(); $s.Write($bodyBytes, 0, $bodyBytes.Length); $s.Close()
  $resp = $req.GetResponse()
  $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
  $failed = 0
  $hits = New-Object System.Collections.Generic.List[string]
  $t0 = [Environment]::TickCount
  while (([Environment]::TickCount - $t0) -lt ($timeoutSec * 1000)) {
    $line = $reader.ReadLine()
    if ($null -eq $line) { break }
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ($payload -eq '' -or $payload -eq '[DONE]') { continue }
    if ($payload -match 'agent\.tool\.failed|"phase":"failed"') { $failed++ }
    if ($payload -match 'procedural|ask_user|过程修复|Prefetch') {
      $hits.Add($payload.Substring(0, [Math]::Min(280, $payload.Length)))
    }
  }
  $reader.Close(); $resp.Close()
  return [pscustomobject]@{ Failed = $failed; Hits = $hits }
}

$failPrompt = 'Fault injection only: call describe_table with table_name exactly equal to NO_SUCH_TABLE_PROC_E2E. Then reply FAIL_DONE.'
Write-Host '=== another ToolFailed ==='
$r = Send-SSE $sid $failPrompt 150
Write-Host ("failedBus={0}" -f $r.Failed)
$r.Hits | Select-Object -First 5 | ForEach-Object { Write-Host ("HIT: {0}" -f $_) }

Write-Host '=== ssh after 2nd fail ==='
$r2 = Send-SSE $sid 'ssh jump-host next. If any procedural repair tip exists (ask_user), quote it verbatim.' 150
Write-Host ("failedBus2={0} hits={1}" -f $r2.Failed, $r2.Hits.Count)
$r2.Hits | Select-Object -First 15 | ForEach-Object { Write-Host ("HIT2: {0}" -f $_) }

Write-Host "SESSION=$sid"
