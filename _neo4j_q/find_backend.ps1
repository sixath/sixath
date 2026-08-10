Get-CimInstance Win32_Process | Where-Object { $_.Name -match 'backend|portal' -or $_.CommandLine -match 'sixath\\portal' } | Select-Object ProcessId, Name, CommandLine | Format-List
Get-NetTCPConnection -LocalPort 8000 -ErrorAction SilentlyContinue | Select-Object OwningProcess, State | Format-Table
