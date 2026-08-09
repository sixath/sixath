function Invoke-HarnessJudges {
    param(
        [Parameter(Mandatory)]
        [string]$CaseId,
        [Parameter(Mandatory)]
        [pscustomobject]$ExecutionResult,
        [Parameter(Mandatory)]
        [object[]]$Judges
    )

    $jsonCache = $null
    $results = New-Object System.Collections.Generic.List[object]

    function Get-JudgeTargetText {
        param(
            [Parameter(Mandatory)]
            [pscustomobject]$Judge,
            [Parameter(Mandatory)]
            [pscustomobject]$ExecutionResult
        )

        if ($Judge.target -eq 'stderr') {
            return $ExecutionResult.StdErr
        }

        return $ExecutionResult.StdOut
    }

    foreach ($judge in $Judges) {
        $passed = $false
        $message = ''

        switch ($judge.kind) {
            'exit_code' {
                $passed = ($ExecutionResult.ExitCode -eq $judge.equals)
                $message = "expected exit code $($judge.equals), actual $($ExecutionResult.ExitCode)"
            }
            'contains_text' {
                $targetText = Get-JudgeTargetText -Judge $judge -ExecutionResult $ExecutionResult
                $passed = ($targetText -match [regex]::Escape($judge.value))
                $message = "expected $($judge.target) to contain $($judge.value)"
            }
            'not_contains_text' {
                $targetText = Get-JudgeTargetText -Judge $judge -ExecutionResult $ExecutionResult
                $passed = ($targetText -notmatch [regex]::Escape($judge.value))
                $message = "expected $($judge.target) to not contain $($judge.value)"
            }
            'json_field_equals' {
                if ($null -eq $jsonCache) {
                    $jsonCache = ConvertFrom-HarnessJson -Text $ExecutionResult.StdOut
                }

                $actual = $jsonCache.PSObject.Properties[$judge.field].Value
                $passed = ($actual -eq $judge.equals)
                $message = "expected json field $($judge.field) to equal $($judge.equals), actual $actual"
            }
            'json_field_exists' {
                if ($null -eq $jsonCache) {
                    $jsonCache = ConvertFrom-HarnessJson -Text $ExecutionResult.StdOut
                }

                $passed = $jsonCache.PSObject.Properties.Name.Contains($judge.field)
                $message = "expected json field $($judge.field) to exist"
            }
            'report_section_exists' {
                $passed = ($ExecutionResult.StdOut -match [regex]::Escape($judge.section))
                $message = "expected report section $($judge.section) to exist"
            }
            default {
                throw "Unsupported judge kind: $($judge.kind)"
            }
        }

        $results.Add([pscustomobject]@{
            CaseId = $CaseId
            Kind = $judge.kind
            Passed = $passed
            Message = $message
        })
    }

    return $results
}
