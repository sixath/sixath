[CmdletBinding()]
param()

function Write-HarnessLearningEntry {
    param(
        [Parameter(Mandatory)]
        [string]$LearningRoot,

        [Parameter(Mandatory)]
        [pscustomobject]$Result
    )

    if ($Result.Verdict -ne 'failed') {
        return
    }

    if ($Result.Classification -notin @('assertion_failed', 'execution_failed')) {
        return
    }

    if (-not (Test-Path $LearningRoot)) {
        New-Item -ItemType Directory -Force -Path $LearningRoot | Out-Null
    }

    $errorsPath = Join-Path $LearningRoot 'ERRORS.md'
    $timestamp = (Get-Date).ToString('o')

    @(
        ''
        "## Harness case failed: $($Result.CaseId)"
        "- Logged: $timestamp"
        "- Classification: $($Result.Classification)"
        "- ExitCode: $($Result.Execution.ExitCode)"
        ''
    ) | Add-Content -Path $errorsPath -Encoding utf8
}
