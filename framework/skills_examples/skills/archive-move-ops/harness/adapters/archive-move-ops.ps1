function Resolve-ArchiveMoveOpsInvocation {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$CaseObject
    )

    $root = Join-Path $PSScriptRoot '..\..\scripts'

    switch ($CaseObject.Type) {
        'parse_dispatch_log' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'parse-dispatch-log.ps1')
                ArgumentList = @('-LogLine', $CaseObject.Input.logLine, '-AsJson')
            }
        }
        'build_followup_commands' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'build-followup-commands.ps1')
                ArgumentList = @('-LogLine', $CaseObject.Input.logLine)
            }
        }
        'build_entry_commands' {
            return [pscustomobject]@{
                FilePath = (Join-Path $root 'build-entry-commands.ps1')
                ArgumentList = @('-TraceId', $CaseObject.Input.traceId)
            }
        }
        'build_investigation_report' {
            $dispatchLogFile = $CaseObject.Input.dispatchLogFile
            if (-not [System.IO.Path]::IsPathRooted($dispatchLogFile)) {
                $caseDirectory = Split-Path -Path $CaseObject.CasePath -Parent
                $dispatchLogFile = [System.IO.Path]::GetFullPath((Join-Path $caseDirectory $dispatchLogFile))
            }

            return [pscustomobject]@{
                FilePath = (Join-Path $root 'build-investigation-report.ps1')
                ArgumentList = @('-TraceId', $CaseObject.Input.traceId, '-DispatchLogFile', $dispatchLogFile)
            }
        }
        default {
            throw "Unsupported archive-move-ops case type: $($CaseObject.Type)"
        }
    }
}
