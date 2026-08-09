function Test-HarnessCaseSchema {
    param(
        [Parameter(Mandatory)]
        [pscustomobject]$CaseObject
    )

    $required = @('id', 'skill', 'type', 'description', 'input', 'expect')
    foreach ($field in $required) {
        if (-not $CaseObject.PSObject.Properties.Name.Contains($field)) {
            throw "Case schema missing required field: $field"
        }
    }

    if (-not $CaseObject.expect.PSObject.Properties.Name.Contains('judges')) {
        throw 'Case schema missing required field: expect.judges'
    }

    return $true
}

function Get-HarnessCases {
    param(
        [Parameter(Mandatory)]
        [string]$CaseRoot
    )

    $items = New-Object System.Collections.Generic.List[object]
    foreach ($file in (Get-ChildItem -Path $CaseRoot -Filter *.json -File | Sort-Object FullName)) {
        try {
            $caseObject = Get-Content $file.FullName -Raw | ConvertFrom-Json
            Test-HarnessCaseSchema -CaseObject $caseObject | Out-Null
            $items.Add([pscustomobject]@{
                Id = $caseObject.id
                Skill = $caseObject.skill
                Type = $caseObject.type
                Description = $caseObject.description
                Input = $caseObject.input
                Expect = $caseObject.expect
                CasePath = $file.FullName
            })
        } catch {
            throw "Failed to load harness case from $($file.FullName). $($_.Exception.Message)"
        }
    }

    return $items
}

function ConvertTo-HarnessArgumentString {
    param(
        [string[]]$Arguments = @()
    )

    $quoted = foreach ($argument in $Arguments) {
        if ([string]::IsNullOrEmpty($argument)) {
            '""'
            continue
        }

        if ($argument -match '[\s"]') {
            '"' + ($argument -replace '\\*"', '\"' -replace '"', '\"') + '"'
            continue
        }

        $argument
    }

    return ($quoted -join ' ')
}

function ConvertTo-HarnessInvocation {
    param(
        [string[]]$ArgumentList = @()
    )

    $named = [ordered]@{}
    $positional = New-Object System.Collections.Generic.List[string]
    $index = 0

    while ($index -lt $ArgumentList.Count) {
        $token = $ArgumentList[$index]
        if ($token -match '^-{1,2}[A-Za-z]') {
            $name = $token.TrimStart('-')
            if (($index + 1) -lt $ArgumentList.Count -and $ArgumentList[$index + 1] -notmatch '^-{1,2}[A-Za-z]') {
                $named[$name] = $ArgumentList[$index + 1]
                $index += 2
                continue
            }

            $named[$name] = $true
            $index += 1
            continue
        }

        [void]$positional.Add($token)
        $index += 1
    }

    return [pscustomobject]@{
        Named = $named
        Positional = $positional.ToArray()
    }
}

function Invoke-HarnessCommand {
    param(
        [Parameter(Mandatory)]
        [string]$FilePath,
        [string[]]$ArgumentList = @()
    )

    $timer = [System.Diagnostics.Stopwatch]::StartNew()

    $scriptContent = Get-Content -Path $FilePath -Raw
    $invocation = ConvertTo-HarnessInvocation -ArgumentList $ArgumentList
    $explicitExitMatch = [regex]::Matches($scriptContent, '(?m)^\s*exit(?:\s+(-?\d+))?\s*$') | Select-Object -Last 1
    $declaredExitCode = $null
    if ($null -ne $explicitExitMatch) {
        $declaredExitCode = if ($explicitExitMatch.Groups[1].Success) {
            [int]$explicitExitMatch.Groups[1].Value
        } else {
            0
        }
    }
    $capturedOutput = @()
    $capturedErrors = New-Object System.Collections.Generic.List[string]
    $exitCode = 0
    $namedArguments = $invocation.Named
    $positionalArguments = $invocation.Positional

    try {
        $global:LASTEXITCODE = $null
        $capturedOutput = @(& $FilePath @namedArguments @positionalArguments 6>&1 2>&1)
        if ($null -ne $global:LASTEXITCODE -and "$global:LASTEXITCODE" -ne '') {
            $exitCode = [int]$global:LASTEXITCODE
        }
    } catch {
        $capturedErrors.Add($_.Exception.Message)
        $exitCode = if ($null -ne $global:LASTEXITCODE -and "$global:LASTEXITCODE" -ne '') {
            [int]$global:LASTEXITCODE
        } elseif ($null -ne $declaredExitCode) {
            $declaredExitCode
        } else {
            1
        }
    }

    $timer.Stop()

    $stdOut = @($capturedOutput | ForEach-Object {
        if ($_ -is [System.Management.Automation.ErrorRecord]) {
            $capturedErrors.Add($_.ToString())
            return
        }

        if ($_ -is [System.Management.Automation.InformationRecord]) {
            $_.MessageData.ToString()
            return
        }

        if ($_ -is [string]) {
            $_
        } else {
            ($_ | Out-String).TrimEnd("`r", "`n")
        }
    } | Where-Object { $null -ne $_ -and $_ -ne '' }) -join [Environment]::NewLine

    if ($exitCode -eq 0 -and $capturedErrors.Count -gt 0) {
        $exitCode = if ($null -ne $global:LASTEXITCODE -and "$global:LASTEXITCODE" -ne '') {
            [int]$global:LASTEXITCODE
        } elseif ($null -ne $declaredExitCode) {
            $declaredExitCode
        } else {
            1
        }
    }

    $stdErr = $capturedErrors -join [Environment]::NewLine

    return [pscustomobject]@{
        Command = "powershell -NoProfile -ExecutionPolicy Bypass -File $FilePath $($ArgumentList -join ' ')"
        ExitCode = [int]$exitCode
        StdOut = $stdOut
        StdErr = $stdErr
        DurationMs = [int]$timer.ElapsedMilliseconds
        Succeeded = ([int]$exitCode -eq 0)
    }
}

function ConvertFrom-HarnessJson {
    param(
        [Parameter(Mandatory)]
        [string]$Text
    )

    try {
        return $Text | ConvertFrom-Json -Depth 20
    } catch {
        return $Text | ConvertFrom-Json
    }
}
