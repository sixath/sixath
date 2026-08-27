$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location (Join-Path $root "portal")
go test ./internal/chat -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Set-Location (Join-Path $root "framework")
go test ./tool -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./tool/data -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./executor -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./agent -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test ./mea -count=1 -run TestEvalGolden_ -v
exit $LASTEXITCODE
