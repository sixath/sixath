[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot '..\..\harness\lib.ps1')

$fixtureRoot = Join-Path $PSScriptRoot '..\fixtures\harness\cases'
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("archive-move-ops-load-cases-{0}" -f ([guid]::NewGuid().ToString('N')))
$null = New-Item -ItemType Directory -Path $tempRoot

try {
    $firstValidPath = Join-Path $tempRoot 'a-valid-second-id.json'
    $secondValidPath = Join-Path $tempRoot 'z-valid-first-id.json'
    $invalidPath = Join-Path $tempRoot 'm-invalid-missing-type.json'

    Copy-Item (Join-Path $fixtureRoot 'valid-minimal.json') $firstValidPath
    Copy-Item (Join-Path $fixtureRoot 'invalid-missing-type.json') $invalidPath

    @'
{
  "id": "fixture.alpha",
  "skill": "archive-move-ops",
  "type": "parse_dispatch_log",
  "description": "second valid fixture used to verify path ordering",
  "input": {
    "logLine": "Worker.startSyncDispatch(). flow_id(400_demo) uuid(42)"
  },
  "expect": {
    "judges": []
  }
}
'@ | Set-Content -Path $secondValidPath

    $failedDiscovery = $false
    try {
        $null = Get-HarnessCases -CaseRoot $tempRoot
    } catch {
        $failedDiscovery = $_.Exception.Message -like "*$invalidPath*" -and $_.Exception.Message -like '*type*'
    }

    if (-not $failedDiscovery) {
        throw 'Expected Get-HarnessCases to fail fast when a discovered case file is invalid.'
    }

    Remove-Item -LiteralPath $invalidPath -Force

    $cases = Get-HarnessCases -CaseRoot $tempRoot

    if ($cases.Count -ne 2) {
        throw "Expected exactly 2 valid discovered cases after removing the invalid file, got $($cases.Count)."
    }

    $firstCase = $cases[0]
    $secondCase = $cases[1]

    $expectedShape = @('Id', 'Skill', 'Type', 'Description', 'Input', 'Expect', 'CasePath')
    foreach ($case in $cases) {
        $actualShape = @($case.PSObject.Properties.Name)
        if (($actualShape -join ',') -ne ($expectedShape -join ',')) {
            throw "Expected normalized case shape '$($expectedShape -join ',')', got '$($actualShape -join ',')'."
        }
    }

    if ($firstCase.Id -ne 'fixture.valid-minimal') {
        throw "Expected path-sorted first case id fixture.valid-minimal, got $($firstCase.Id)."
    }

    if ($secondCase.Id -ne 'fixture.alpha') {
        throw "Expected path-sorted second case id fixture.alpha, got $($secondCase.Id)."
    }

    if ($firstCase.CasePath -ne $firstValidPath) {
        throw "Expected first case path $firstValidPath, got $($firstCase.CasePath)."
    }

    if ($secondCase.CasePath -ne $secondValidPath) {
        throw "Expected second case path $secondValidPath, got $($secondCase.CasePath)."
    }

    $failed = $false
    try {
        Test-HarnessCaseSchema -CaseObject (Get-Content (Join-Path $fixtureRoot 'invalid-missing-type.json') -Raw | ConvertFrom-Json)
    } catch {
        $failed = $_.Exception.Message -like '*type*'
    }

    if (-not $failed) {
        throw 'Expected schema validation to fail for missing type.'
    }
}
finally {
    if (Test-Path -Path $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}

Write-Host 'PASS load-cases.test.ps1'
