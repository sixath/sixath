param([switch]$Project)
$ErrorActionPreference = "Stop"
$SkillSrc = Join-Path $PSScriptRoot "sixath-framework"
$SkillName = "sixath-framework"

if (-not (Test-Path (Join-Path $SkillSrc "SKILL.md"))) {
    Write-Error "SKILL.md not found in $SkillSrc"
}

function Install-SkillTo {
    param([string]$Dest)
    $parent = Split-Path $Dest -Parent
    if (-not (Test-Path $parent)) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
    if (Test-Path $Dest) { Remove-Item -Recurse -Force $Dest }
    Copy-Item -Recurse -Force $SkillSrc $Dest
    Write-Host "Installed -> $Dest"
}

$targets = @(
    (Join-Path $env:USERPROFILE ".cursor\skills\$SkillName"),
    (Join-Path $env:USERPROFILE ".claude\skills\$SkillName"),
    (Join-Path $env:USERPROFILE ".codex\skills\$SkillName"),
    (Join-Path $env:USERPROFILE ".agents\skills\$SkillName")
)
if ($Project) {
    $fwRoot = Split-Path $PSScriptRoot -Parent
    $targets += (Join-Path $fwRoot ".cursor\skills\$SkillName")
}
foreach ($t in $targets) { Install-SkillTo $t }
Write-Host "`nDone. Use @sixath-framework. Add skillpack to skills.skills_dirs for runtime."
