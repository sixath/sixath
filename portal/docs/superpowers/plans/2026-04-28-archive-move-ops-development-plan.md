# Archive Move Ops Development Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make archive migration troubleshooting run end-to-end from chat: the agent should receive a flow id, execute the correct Skill script, SSH into mapped hosts, inspect archive-manager and downstream logs, and return a user-facing conclusion without leaking debug trace.

**Architecture:** This work is split into three layers. First, fix the framework tool contract so Skill scripts can receive command-line arguments. Second, add a real archive investigation script that executes SSH searches and returns structured evidence. Third, separate debug stream events from assistant content so debugging remains available without polluting the chat answer.

**Tech Stack:** Go, PowerShell, SSH, Server-Sent Events, React, Vite, Superpowers planning workflow.

---

## Reference

Detailed implementation steps, code snippets, and verification commands are in:

`D:\workspace\github\sixath\portal\docs\superpowers\plans\2026-04-28-archive-move-ops-ssh-investigation.md`

This development plan is the execution tracker for that implementation plan.

---

## Delivery Milestones

### Milestone 1: Tool Contract Fix

**Outcome:** `execute_skill_script` can pass command-line args to Skill scripts while preserving existing stdin behavior.

**Files:**
- Modify: `D:\workspace\github\sixath\framework\tool\skill_tools.go`
- Modify: `D:\workspace\github\sixath\framework\tool\skill_tools_test.go`

- [ ] Add `args` schema to the `execute_skill_script` tool.
- [ ] Parse `args` as an array of strings.
- [ ] Append args after the script path when building `exec.CommandContext`.
- [ ] Keep existing `input` behavior as stdin.
- [ ] Add test coverage for `args`.
- [ ] Run `go test ./tool -count=1` from `D:\workspace\github\sixath\framework`.
- [ ] Commit with message: `feat(tool): pass args to skill scripts`.

**Acceptance Criteria:**
- A call with `args: ["-FlowId", "4_v0cag1d3guo8"]` reaches the script as command-line arguments.
- Existing scripts using stdin still work.
- Path traversal and extension allowlist protections remain unchanged.

---

### Milestone 2: Archive Investigation Script

**Outcome:** `archive-move-ops` has an executable path that performs SSH log investigation instead of only generating SSH commands.

**Files:**
- Create: `E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1`
- Modify: `E:\sixath\workspace\skills\archive-move-ops\SKILL.md`

- [ ] Create `investigate-flow.ps1`.
- [ ] Accept `-FlowId`, optional `-UnionDatePrefix`, `-Context`, and `-DryRun`.
- [ ] Infer prefixed area from `flow_id`.
- [ ] Search mapped `archiver-manager` hosts for the inferred area.
- [ ] Search both current and compressed historical logs.
- [ ] Use `ssh -o BatchMode=yes -o ConnectTimeout=8` to avoid password-prompt hangs.
- [ ] Extract `task_id`, `uid`, `src_uid`, and `dst_uid` or `dest_uid` from manager hits.
- [ ] Return JSON with searched hosts, commands, exit codes, output snippets, and extracted identifiers.
- [ ] Update `SKILL.md` so flow-id questions call `investigate-flow.ps1` first.
- [ ] Run dry-run verification for `4_v0cag1d3guo8`.
- [ ] Run real SSH verification in an environment with key-based SSH or confirm fast auth failure.
- [ ] Commit with message: `feat(archive): add ssh flow investigation script`.

**Acceptance Criteria:**
- The agent no longer stops after printing generated SSH commands.
- If SSH works, the script returns real manager log evidence.
- If SSH auth is unavailable, the script fails quickly and reports the auth blocker.
- The Skill instructions clearly prefer the executable investigation path over command builders.

---

### Milestone 3: Downstream Evidence Expansion

**Outcome:** The investigation script continues past the first manager hit when enough identifiers are available.

**Files:**
- Modify: `E:\sixath\workspace\skills\archive-move-ops\scripts\investigate-flow.ps1`
- Optionally reuse helpers from:
  - `E:\sixath\workspace\skills\archive-move-ops\scripts\build-data-channel-commands.ps1`
  - `E:\sixath\workspace\skills\archive-move-ops\scripts\build-storage-worker-commands.ps1`
  - `E:\sixath\workspace\skills\archive-move-ops\scripts\build-uid-error-commands.ps1`

- [ ] Add UID-based ERROR searches for source and destination manager logs when UIDs are present.
- [ ] Add data-channel searches when `task_id` and `uid` are present.
- [ ] Add storage-worker searches when `uid`, `gid`, and `dscid` are present.
- [ ] Include each downstream stage in the JSON report, even when skipped.
- [ ] Make skipped stages explain the missing prerequisite identifier.
- [ ] Test with dry-run output.
- [ ] Test with captured sample manager log text if live SSH data is unavailable.
- [ ] Commit with message: `feat(archive): continue flow investigation downstream`.

**Acceptance Criteria:**
- Reports include `archiver_manager`, `uid_error_search`, `data_channel`, `storage_worker`, and `conclusion` sections.
- A missing identifier does not crash the investigation.
- The final answer can explain exactly where the chain stopped.

---

### Milestone 4: Debug Stream Separation

**Outcome:** Debug traces remain inspectable in DevTools but do not appear as assistant-visible chat content.

**Files:**
- Modify: `D:\workspace\github\sixath\portal\internal\service\chat_stream.go`
- Modify: `D:\workspace\github\sixath\portal\internal\service\chat.go`
- Modify: `D:\workspace\github\sixath\portal\internal\server\chat_sse.go`
- Modify: `D:\workspace\github\sixath\web\src\api\client.ts`
- Modify: `D:\workspace\github\sixath\web\src\pages\ChatPage.tsx`

- [ ] Add `ChatStreamEventDebug`.
- [ ] Emit debug bus events as `debug`, not `chunk`.
- [ ] Serialize debug SSE events as `event: debug`.
- [ ] Parse `debug` events in frontend client code.
- [ ] Drop debug events from assistant message content.
- [ ] Keep user-visible `chunk`, `confirm_required`, and `error` behavior unchanged.
- [ ] Run backend tests.
- [ ] Run frontend tests and build.
- [ ] Commit with message: `fix(chat): separate debug stream events`.

**Acceptance Criteria:**
- With `debugRun=true`, DevTools Network still shows debug events.
- The chat UI no longer displays `agent.run.started[...]`, tool execution payloads, or full Skill text.
- Normal assistant answers still stream correctly.

---

### Milestone 5: End-To-End Browser Verification

**Outcome:** The original user scenario succeeds or returns a precise external blocker.

**Files:**
- No planned source edits.

- [ ] Start backend and frontend services.
- [ ] Open `http://localhost:5174/`.
- [ ] Select `migu-agent`.
- [ ] Send `查询4_v0cag1d3guo8存档迁移失败原因`.
- [ ] Verify the stream calls `execute_skill_script` with `path: scripts/investigate-flow.ps1`.
- [ ] Verify the stream passes `args: ["-FlowId", "4_v0cag1d3guo8"]`.
- [ ] Verify assistant-visible output is a concise investigation summary.
- [ ] Verify debug output is visible only as debug events.
- [ ] Record the final status in the task notes.

**Acceptance Criteria:**
- Success path: assistant reports archive-manager evidence and downstream findings.
- Blocked path: assistant clearly reports SSH auth/network blocker with host and stage.
- No raw debug trace leaks into the chat answer.

---

## Suggested Execution Order

1. Milestone 1: Tool Contract Fix
2. Milestone 2: Archive Investigation Script
3. Milestone 3: Downstream Evidence Expansion
4. Milestone 4: Debug Stream Separation
5. Milestone 5: End-To-End Browser Verification

The first two milestones are the critical path. Milestone 4 can be developed in parallel after Milestone 1 because it touches a separate stream/UI path.

---

## Risks

- SSH may require interactive password input. The script must use batch mode and report auth failure rather than hanging.
- Live log hosts may be unreachable from the dev machine. Dry-run and sample-log parsing should cover local validation.
- The model may still choose command-builder scripts if `SKILL.md` remains ambiguous. The Skill instructions must explicitly prefer `investigate-flow.ps1`.
- Debug event separation changes the SSE contract. Frontend and backend must be updated together.

---

## Verification Checklist

- [ ] `go test ./tool -count=1` passes in `D:\workspace\github\sixath\framework`.
- [ ] Portal backend service tests pass.
- [ ] Web tests pass.
- [ ] Web build passes.
- [ ] `investigate-flow.ps1 -FlowId 4_v0cag1d3guo8 -DryRun` returns JSON with area `4`.
- [ ] Browser query no longer stops at generated SSH commands.
- [ ] Browser query does not show raw debug trace in assistant text.

---

## Execution Options

Plan complete and saved to:

`D:\workspace\github\sixath\portal\docs\superpowers\plans\2026-04-28-archive-move-ops-development-plan.md`

Two execution options:

1. **Subagent-Driven (recommended)** - Dispatch a fresh subagent per milestone, review between milestones, fast iteration.
2. **Inline Execution** - Execute milestones in this session using executing-plans, with checkpoints after each milestone.
