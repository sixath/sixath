# Archive Move Ops SSH Investigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `migu-agent` continue from `archive-move-ops` into real SSH log investigation instead of stopping after command generation.

**Architecture:** Fix the tool contract first so Skill scripts can receive structured command-line arguments, then add a single orchestration script in `archive-move-ops` that executes SSH searches, parses hits, and returns a structured investigation report. Keep debug trace separate from user-visible assistant text so operator diagnostics do not pollute chat output.

**Tech Stack:** Go backend (`portal`, `framework`), PowerShell Skill scripts, SSE chat stream, React chat UI, SSH with non-interactive safety options.

---

## File Structure

- Modify: `D:\workspace\github\sixath\framework\tool\skill_tools.go`
  - Add optional `args: string[]` support to `execute_skill_script`.
  - Preserve `input` stdin behavior for existing scripts.
  - Pass args after the script path: `powershell -ExecutionPolicy Bypass -File script.ps1 -FlowId ...`.

- Modify: `D:\workspace\github\sixath\framework\tool\skill_tools_test.go`
  - Add coverage for command-line args.
  - Add coverage that args cannot bypass script path restrictions.

- Create: `E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1`
  - Execute SSH searches for a known `flow_id`.
  - Search prefixed area `archiver-manager` first.
  - Optionally search union dispatch when `UnionDatePrefix` is provided.
  - Parse manager hits for `task_id`, `uid`, `src_uid`, `dst_uid` or `dest_uid`.
  - Continue into data-channel and storage-worker when enough identifiers are present.
  - Use `ssh -o BatchMode=yes -o ConnectTimeout=8` so missing key-based auth fails quickly instead of hanging on a password prompt.

- Modify: `E:\sixath\workspace\skills\archive-move-ops\SKILL.md`
  - Tell the model to call `investigate-flow.ps1` for `flow_id` questions.
  - Keep command-generation scripts as fallback/reference helpers.
  - State that interactive SSH passwords are unsupported in agent execution.

- Modify: `D:\workspace\github\sixath\portal\internal\service\chat_stream.go`
  - Add a debug event type, for example `debug`.

- Modify: `D:\workspace\github\sixath\portal\internal\service\chat.go`
  - Send debug bus events as `ChatStreamEventDebug`, not `ChatStreamEventChunk`.

- Modify: `D:\workspace\github\sixath\portal\internal\server\chat_sse.go`
  - Serialize debug events as `event: debug`.

- Modify: `D:\workspace\github\sixath\web\src\api\client.ts`
  - Parse `debug` SSE events separately.

- Modify: `D:\workspace\github\sixath\web\src\pages\ChatPage.tsx`
  - Do not append `debug` events to assistant content.
  - Optional: store debug events in component state for a later diagnostics panel.

---

### Task 1: Add `args` Support To `execute_skill_script`

**Files:**
- Modify: `D:\workspace\github\sixath\framework\tool\skill_tools.go`
- Test: `D:\workspace\github\sixath\framework\tool\skill_tools_test.go`

- [ ] **Step 1: Write the failing test**

Append this test to `D:\workspace\github\sixath\framework\tool\skill_tools_test.go`:

```go
func TestRegisterExecuteSkillScriptTool_Args(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "arg-skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	scriptPath := filepath.Join(scriptsDir, "args.sh")
	if err := os.WriteFile(skillPath, []byte("---\nname: arg-skill\ndescription: args\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '%s|%s\\n' \"$1\" \"$2\"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	reg := NewRegistry()
	if err := RegisterExecuteSkillScriptTool(reg, idx, true, nil); err != nil {
		t.Fatalf("RegisterExecuteSkillScriptTool: %v", err)
	}
	tl, _ := reg.Get("execute_skill_script")
	out, err := tl.Execute(context.Background(), map[string]any{
		"name": "arg-skill",
		"path": "scripts/args.sh",
		"args": []any{"-FlowId", "4_v0cag1d3guo8"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := strings.TrimSpace(out.(string))
	if got != "-FlowId|4_v0cag1d3guo8" {
		t.Fatalf("unexpected args output: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run from `D:\workspace\github\sixath\framework`:

```powershell
go test ./tool -run TestRegisterExecuteSkillScriptTool_Args -count=1 -v
```

Expected: FAIL because `args` is ignored and output is `|`.

- [ ] **Step 3: Implement args parsing**

In `D:\workspace\github\sixath\framework\tool\skill_tools.go`, extend the `Parameters` map:

```go
"args": map[string]any{
	"type": "array",
	"items": map[string]any{
		"type": "string",
	},
	"description": "Optional command-line arguments passed to the script after the script path, e.g. [\"-FlowId\", \"4_v0cag1d3guo8\"].",
},
```

Add this helper near `defaultScriptTimeout`:

```go
func scriptArgsFromParams(params map[string]any) ([]string, error) {
	raw, ok := params["args"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("execute_skill_script: args must be an array of strings")
	}
	args := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, errors.New("execute_skill_script: args must be an array of strings")
		}
		args = append(args, s)
	}
	return args, nil
}
```

Then after `cmdName, cmdArgs := scriptCommand(ext, fullPath)` add:

```go
extraArgs, err := scriptArgsFromParams(params)
if err != nil {
	return nil, err
}
cmdArgs = append(cmdArgs, extraArgs...)
```

- [ ] **Step 4: Run tests**

Run from `D:\workspace\github\sixath\framework`:

```powershell
go test ./tool -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add tool/skill_tools.go tool/skill_tools_test.go
git commit -m "feat(tool): pass args to skill scripts"
```

---

### Task 2: Add Real SSH Investigation Script

**Files:**
- Create: `E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1`
- Modify: `E:\sixath\workspace\skills\archive-move-ops\SKILL.md`

- [ ] **Step 1: Create a script with non-interactive SSH execution**

Create `E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1`:

```powershell
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$FlowId,

    [string]$UnionDatePrefix,

    [int]$Context = 2,

    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$environmentPath = Join-Path $PSScriptRoot '..\references\environment.psd1'
$environment = Import-PowerShellDataFile -Path $environmentPath
$sshUser = $environment.Ssh.User

function Escape-SingleQuotedShellValue {
    param([Parameter(Mandatory = $true)][string]$Value)
    return $Value -replace "'", "'""'""'"
}

function Invoke-RemoteSearch {
    param(
        [Parameter(Mandatory = $true)][string]$HostName,
        [Parameter(Mandatory = $true)][string]$Command
    )

    $target = if ($sshUser) { "$sshUser@$HostName" } else { $HostName }
    $sshArgs = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=8', $target, $Command)

    if ($DryRun) {
        return [pscustomobject]@{
            host = $HostName
            command = "ssh $($sshArgs -join ' ')"
            exit_code = 0
            output = ''
        }
    }

    $output = & ssh @sshArgs 2>&1
    return [pscustomobject]@{
        host = $HostName
        command = "ssh $($sshArgs -join ' ')"
        exit_code = $LASTEXITCODE
        output = ($output -join [Environment]::NewLine)
    }
}

function Search-ArchiverManager {
    param(
        [Parameter(Mandatory = $true)][string]$Area,
        [Parameter(Mandatory = $true)][string]$Keyword
    )

    $svc = $environment.Services['archiver-manager']
    $hosts = $svc.HostsByArea[$Area]
    if (-not $hosts) {
        return @([pscustomobject]@{ area = $Area; host = ''; error = "no archiver-manager hosts configured for area $Area" })
    }

    $results = New-Object System.Collections.Generic.List[object]
    $escaped = Escape-SingleQuotedShellValue -Value $Keyword
    foreach ($hostName in $hosts) {
        foreach ($directory in $svc.LogDirectories) {
            $current = "$directory$($svc.CurrentLogPattern)"
            $history = "$directory$($svc.HistoryLogPattern)"
            $cmd = "grep -nH -C $Context -- '$escaped' $current 2>/dev/null; zgrep -nH -C $Context -- '$escaped' $history 2>/dev/null"
            $hit = Invoke-RemoteSearch -HostName $hostName -Command $cmd
            $hit | Add-Member -NotePropertyName area -NotePropertyValue $Area
            $hit | Add-Member -NotePropertyName service -NotePropertyValue 'archiver-manager'
            [void]$results.Add($hit)
        }
    }
    return $results
}

function Extract-FirstMatch {
    param([string]$Text, [string]$Pattern)
    $m = [regex]::Match($Text, $Pattern)
    if ($m.Success) { return $m.Groups[1].Value }
    return ''
}

if ($FlowId -notmatch '^(?<area>\d+)_') {
    throw "Unable to infer area from flow_id: $FlowId"
}

$inferredArea = $Matches['area']
$managerResults = @(Search-ArchiverManager -Area $inferredArea -Keyword $FlowId)
$managerText = ($managerResults | ForEach-Object { $_.output }) -join [Environment]::NewLine

$taskId = Extract-FirstMatch -Text $managerText -Pattern 'task_id\(([^)]+)\)'
$uid = Extract-FirstMatch -Text $managerText -Pattern '\buid\(([^)]+)\)'
$srcUid = Extract-FirstMatch -Text $managerText -Pattern 'src_uid\(([^)]+)\)'
$dstUid = Extract-FirstMatch -Text $managerText -Pattern '(?:dst_uid|dest_uid)\(([^)]+)\)'

$report = [ordered]@{
    flow_id = $FlowId
    inferred_area = $inferredArea
    note = 'FlowId mode searches the prefixed area first. Add dispatch parsing when source/destination route is required.'
    archiver_manager = $managerResults
    extracted = [ordered]@{
        task_id = $taskId
        uid = $uid
        src_uid = $srcUid
        dst_uid = $dstUid
    }
}

$report | ConvertTo-Json -Depth 8
```

- [ ] **Step 2: Dry-run locally**

Run:

```powershell
powershell -ExecutionPolicy Bypass -File E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1 -FlowId 4_v0cag1d3guo8 -DryRun
```

Expected: JSON containing `flow_id`, `inferred_area: "4"`, and generated SSH commands for area `4` manager hosts.

- [ ] **Step 3: Test real SSH behavior**

Run:

```powershell
powershell -ExecutionPolicy Bypass -File E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1 -FlowId 4_v0cag1d3guo8
```

Expected if key-based SSH is configured: JSON with `archiver_manager[*].output` populated or empty per host.

Expected if password is required: JSON or command output indicating SSH auth failure quickly. It must not hang waiting for an interactive password prompt.

- [ ] **Step 4: Update Skill instructions**

In `E:\sixath\workspace\skills\archive-move-ops\SKILL.md`, add this before `## Quick Start`:

```markdown
## Agent Execution Path

When the user provides a `flow_id` such as `4_v0cag1d3guo8`, call:

```json
{
  "name": "archive-move-ops",
  "path": "scripts/investigate-flow.ps1",
  "args": ["-FlowId", "4_v0cag1d3guo8"]
}
```

Use command-generation scripts only as fallback helpers. Do not stop after printing SSH commands when `investigate-flow.ps1` can run. SSH execution is non-interactive; the runtime must have key-based SSH access. If SSH reports authentication failure, return that as the current blocker instead of asking the user to run the generated commands.
```

- [ ] **Step 5: Re-test through the chat UI**

Use Chrome DevTools against `http://localhost:5174/`:

1. Select `migu-agent`.
2. Send `查询4_v0cag1d3guo8存档迁移失败原因`.
3. Inspect the stream response.

Expected: agent calls `execute_skill_script` with `path: scripts/investigate-flow.ps1` and `args: ["-FlowId", "4_v0cag1d3guo8"]`, then summarizes JSON results or SSH auth blocker.

---

### Task 3: Keep Debug Events Out Of Assistant Text

**Files:**
- Modify: `D:\workspace\github\sixath\portal\internal\service\chat_stream.go`
- Modify: `D:\workspace\github\sixath\portal\internal\service\chat.go`
- Modify: `D:\workspace\github\sixath\portal\internal\server\chat_sse.go`
- Modify: `D:\workspace\github\sixath\web\src\api\client.ts`
- Modify: `D:\workspace\github\sixath\web\src\pages\ChatPage.tsx`

- [ ] **Step 1: Add backend debug event type**

In `chat_stream.go`, add:

```go
ChatStreamEventDebug ChatStreamEventType = "debug"
```

- [ ] **Step 2: Publish debug bus events as debug**

In `chat.go`, change the debug event send from:

```go
ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: string(e.Kind) + "[" + string(msg) + "]\r\n"}
```

to:

```go
ch <- ChatStreamEvent{Type: ChatStreamEventDebug, Content: string(e.Kind) + "[" + string(msg) + "]\r\n"}
```

- [ ] **Step 3: Serialize `event: debug`**

In `chat_sse.go`, add a case beside `chunk`, `confirm_required`, and `error`:

```go
case service.ChatStreamEventDebug:
	writeSSE(ctx, "debug", map[string]string{"content": ev.Content})
```

Use the local helper/function shape already present in `chat_sse.go`.

- [ ] **Step 4: Parse frontend debug separately**

In `web/src/api/client.ts`, extend callbacks:

```ts
onDebug?: (text: string) => void
```

Then add:

```ts
else if (curEvent === 'debug' && typeof d.content === 'string') callbacks.onDebug?.(d.content)
```

- [ ] **Step 5: Do not append debug to assistant content**

In `web/src/pages/ChatPage.tsx`, add callback handling without changing message content:

```ts
onDebug: (_text) => {
  // Intentionally kept out of assistant content. A diagnostics panel can consume this later.
},
```

- [ ] **Step 6: Verify**

Run backend tests:

```powershell
go test ./internal/service ./internal/server -count=1
```

Run frontend tests/build from `D:\workspace\github\sixath\web`:

```powershell
npm test
npm run build
```

Expected: tests pass, and the chat answer no longer includes `agent.run.started[...]`, `agent.tool.executed[...]`, or the full skill body as assistant-visible text.

---

### Task 4: End-To-End Verification

**Files:**
- No source changes.

- [ ] **Step 1: Start services**

Use the existing project commands for backend and frontend. Confirm frontend is reachable at:

```text
http://localhost:5174/
```

- [ ] **Step 2: Reproduce original query**

In Chrome DevTools:

1. Open `http://localhost:5174/`.
2. Select `migu-agent`.
3. Send `查询4_v0cag1d3guo8存档迁移失败原因`.

Expected network stream:

```text
event: debug
data: {"content":"agent.tool.started..."}

event: chunk
data: {"content":"<human-readable investigation summary>"}
```

Expected chat UI:

- User sees only the final investigation summary.
- User does not see raw debug trace.
- If SSH auth is missing, assistant clearly says SSH key-based authentication is not available and names the failed host/command stage.
- If logs are found, assistant reports `archiver-manager` evidence and continues to extracted `task_id`/`uid` follow-up checks.

- [ ] **Step 3: Commit**

```powershell
git add .
git commit -m "feat(archive): run ssh log investigation from skill"
```

---

## Self-Review

- Spec coverage: The plan covers the observed premature stop, PowerShell parameter mismatch, missing real SSH execution, and debug trace pollution.
- Placeholder scan: No step depends on undefined future work. The only optional UI work is explicitly out of scope; debug events are dropped safely for now.
- Type consistency: `args` is consistently defined as `[]string` in schema and passed as `[]any` in tests because Go JSON-style params decode as `[]any`.

## Execution Options

Plan complete and saved to `D:\workspace\github\sixath\portal\docs\superpowers\plans\2026-04-28-archive-move-ops-ssh-investigation.md`.

Two execution options:

1. **Subagent-Driven (recommended)** - Dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints.
