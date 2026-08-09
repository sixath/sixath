---
name: archive-move-ops
description: Use when investigating archive migration problems through ssh_exec tool calls, especially when starting from a traceId in union-archiver-dispatch, extracting flow_id/src_area_type/dst_area_type from Worker.startSyncDispatch() lines, then archiver-manager on the destination with flow_id and on the source with src_uid, plus data-channel and storage-worker logs.
---

# Archive Move Ops

Use this skill to investigate archive migration chains without ES. Start from `union-archiver-dispatch`, determine the migration route from `Worker.startSyncDispatch()` logs, then continue in **destination** `archiver-manager` logs with `flow_id`, and in **source** `archiver-manager` logs with **`src_uid` only** (do not grep `flow_id` on the source side).

All remote log queries must be executed through the built-in `ssh_exec` tool. Do not issue raw `ssh ...` shell commands directly.

## Available Resources

- `references/service-map.md`: Human-readable host, area, and log path mapping.
- `references/environment.psd1`: Structured environment data for scripts.
- `scripts/parse-dispatch-log.ps1`: Parse a `Worker.startSyncDispatch()` log line.
- `scripts/build-search-commands.ps1`: Generate copy-ready SSH search commands.
- `scripts/build-entry-commands.ps1`: One-click entry commands for `traceId` or `flow_id`.
- `scripts/build-flow-investigation-template.ps1`: Build a ready-to-fill investigation template from `flow_id + date`.
- `scripts/build-followup-commands.ps1`: Turn one dispatch log line into the next `archiver-manager` search commands.
- `scripts/build-investigation-report.ps1`: Build a report template from `traceId + dispatch log file`.
- `scripts/build-uid-error-commands.ps1`: Extract `src_uid` or `dst_uid`/`dest_uid` from an `archiver-manager` hit and generate UID-based ERROR searches.
- `scripts/build-storage-worker-commands.ps1`: Extract `uid/gid/dscid` from a `StorageWorker.Export()` log line and generate storage-worker searches.
- `scripts/build-data-channel-commands.ps1`: Extract `task_id` and `uid` from an `archiver-manager` hit and generate data-channel plus same-host storage-worker searches.

## Standard Workflow

1. Start from `union-archiver-dispatch`.
2. Search the fixed dispatch hosts for the given `traceId`.
3. Find a `Worker.startSyncDispatch()` line and extract:
   - `flow_id`
   - `src_area_type`
   - `dst_area_type`
4. Treat the migration route as `src_area_type -> dst_area_type`.
   - The destination area is always defined by `dst_area_type`.
5. Search `archiver-manager`:
   - **Destination area:** use the extracted `flow_id`.
   - **Source area:** use **`src_uid`** as the grep keyword (try the numeric id and `src_uid=<id>`). Do **not** use `flow_id` on the source side. If `src_uid` is not on the dispatch line, take it from a destination-side manager hit (`param` JSON) first, then search the source area.
6. From the matching `archiver-manager` log line, extract `task_id` and `uid`, then search the same area's data-channel host:
   - Use `task_id` for `/opt/deploy_agent/log/deploy_agent*.log*`.
   - Use `task_id` for `/opt/deploy_server/log/deploy_server*.log*`.
   - Use `uid` for `/data/storage_worker/logs/storage-worker*.log*` on the data-channel host.
7. From the matching `archiver-manager` log line, extract UID by side:
   - Source-side manager: extract `src_uid`.
   - Destination-side manager: extract `dst_uid`; if the log uses `dest_uid`, treat it as the destination UID.
8. Search ERROR logs with the extracted UID in the corresponding manager area.
9. If the UID search finds `StorageWorker.Export()`, extract `uid`, `gid`, and `dscid`.
10. Use `area + dscid` to choose the storage-worker/cache machine, then search storage-worker logs by `uid` and `gid`.
11. Summarize the evidence from dispatch, source-area manager, destination-area manager, data-channel, UID error searches, and storage-worker logs.

## Agent Execution Path

When the user provides a `flow_id` such as `4_v0cag1d3guo8`, run the investigation by calling `ssh_exec` directly across the required hosts/stages (dispatch -> manager -> data-channel -> storage-worker).

Use `execute_skill_script` only as fallback helper when you need to generate templates or parse local text samples.

Use command-generation scripts only as fallback helpers. Do not stop after printing commands when `investigate-flow.ps1` can run.

When remote execution is needed, call `ssh_exec` for each host/command pair:

```json
{
  "host": "10.19.240.104",
  "command": "grep -nH -C 2 -- 'trace-demo-123' /data/union/logs/union_archiver_dispatch/union-archiver-dispatch.log 2>/dev/null",
  "strict_host_key_checking": "accept-new"
}
```

The tool is non-interactive. If `ssh_exec` returns `auth_failed`, `network_failed`, `host_key_failed`, or `timeout`, return that blocker with the failed stage and host instead of asking the user to run commands manually.

`investigate-flow.ps1` accepts `-StrictHostKeyChecking accept-new|yes|no`. Prefer `accept-new` for controlled internal troubleshooting when first connecting to known internal hosts. Use `yes` when host keys are pre-provisioned. Avoid `no` unless a human explicitly approves it for a throwaway diagnostic environment.

## Quick Start

### 1. Search dispatch logs by traceId

```powershell
.\scripts\build-search-commands.ps1 `
  -Service union-archiver-dispatch `
  -Keyword 'your-trace-id'
```

Run the generated commands via `ssh_exec` on the target machines. Search both:

- Current log: `.log`
- Historical logs: `*.log.gz`

### 1a. One-click entry commands for traceId or flow_id

```powershell
.\scripts\build-entry-commands.ps1 -TraceId 'trace-demo-123'
.\scripts\build-entry-commands.ps1 -FlowId '4_107g41jgxu3s'
.\scripts\build-entry-commands.ps1 -TraceId 'trace-demo-123' -UnionDatePrefix '2026-04-20'
.\scripts\build-entry-commands.ps1 -FlowId '4_107g41jgxu3s' -UnionDatePrefix '2026-04-20'
.\\scripts\\build-flow-investigation-template.ps1 -FlowId '4_107g41jgxu3s' -UnionDatePrefix '2026-04-20'
```

- `TraceId` mode generates `union-archiver-dispatch` search commands.
- `FlowId` mode infers the prefixed area, then generates `archiver-manager` search commands for that area.
- `TraceId + UnionDatePrefix` generates date-scoped compressed-log commands and filters `Worker.startSyncDispatch()` first.
- `FlowId + UnionDatePrefix` keeps the inferred-area manager commands and also generates date-scoped union commands filtered by `Worker.startSyncDispatch()`.
- `build-flow-investigation-template.ps1` wraps those commands into a single troubleshooting template when you want a report skeleton immediately.

### 2. Parse the dispatch line

```powershell
.\scripts\parse-dispatch-log.ps1 `
  -LogLine 'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)' `
  -AsJson
```

The parser returns the route as `400 -> 301`. Always trust `dst_area_type` for the destination area.

### 3. Search manager logs (destination: `flow_id`, source: `src_uid`)

Destination area example:

```powershell
.\scripts\build-search-commands.ps1 `
  -Service archiver-manager `
  -Keyword '301_rqkkw0snhnmt' `
  -Areas 301
```

Source area example (after you know `src_uid`, e.g. from a destination manager line):

```powershell
.\scripts\build-search-commands.ps1 `
  -Service archiver-manager `
  -Keyword '154880308' `
  -Areas 400
```

Run destination commands on destination hosts and source commands on source hosts. Do not use `flow_id` as the keyword for the source area.

### 4. Generate follow-up commands directly from one dispatch line

```powershell
.\scripts\build-followup-commands.ps1 `
  -LogLine 'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)'
.\scripts\build-followup-commands.ps1 `
  -LogLine 'Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)' `
  -SrcUid '154880308'
```

Use this when you already have the dispatch hit. Without `-SrcUid`, the script prints destination `flow_id` searches and **instructions** for the source side (source must use `src_uid`). After you read `src_uid` from a destination manager hit, re-run with `-SrcUid`.

### 5. Generate an investigation report template from traceId and a dispatch log file

```powershell
.\scripts\build-investigation-report.ps1 `
  -TraceId 'trace-demo-123' `
  -DispatchLogFile '.\tests\fixtures\dispatch-log-sample.txt'
.\scripts\build-investigation-report.ps1 `
  -TraceId 'trace-demo-123' `
  -DispatchLogFile '.\tests\fixtures\dispatch-log-sample.txt' `
  -SrcUid '154880308'
```

Use this when you already exported or copied dispatch logs into a local text file. Pass `-SrcUid` once you have it from the destination manager so the report includes runnable source-area commands.

### 6. Generate a report template from flow_id and date

```powershell
.\scripts\build-flow-investigation-template.ps1 `
  -FlowId '4_107g41jgxu3s' `
  -UnionDatePrefix '2026-04-20'
```

Use this when you only have `flow_id` plus an approximate event date and want one template that contains both manager commands and union dispatch commands.

### 7. Generate UID-based ERROR searches from a manager hit

```powershell
.\scripts\build-uid-error-commands.ps1 `
  -Side Destination `
  -LogLine 'recieve request. param({"src_area_type":301,"src_uid":6364047,"dest_uid":8980218,"flow_id":"4_107g41jgxu3s"})'
```

Use `-Side Source` for source-area manager hits and `-Side Destination` for destination-area manager hits. Pass `-Area` explicitly if the script cannot infer the correct area from `src_area_type` or `flow_id`.

### 8. Generate storage-worker searches from StorageWorker.Export

```powershell
.\scripts\build-storage-worker-commands.ps1 `
  -LogLine 'StorageWorker.Export(). uid(9664537) gid(1506) rpid(2000) dscid(1) dir(/data/volume_resource_point/2048249069968441344) errcode(0) err(<nil>)' `
  -Area 4
```

The script resolves the machine from `area + dscid` using `references/environment.psd1`. Pass `-StorageHosts` only for ad hoc checks or when the mapping is missing.

### 9. Generate data-channel searches from a manager hit

```powershell
.\scripts\build-data-channel-commands.ps1 `
  -LogLine 'archive task created. task_id(archive-task-123) uid(9664537) flow_id(4_107g41jgxu3s)' `
  -Area 4
```

Use this after an `archiver-manager` hit exposes both `task_id` and `uid`.

- `task_id` searches data-channel deploy logs: `/opt/deploy_agent/log/deploy_agent*.log*` and `/opt/deploy_server/log/deploy_server*.log*`.
- `uid` searches same-host storage-worker logs: `/data/storage_worker/logs/storage-worker*.log*`.
- Pass `-Area` for the manager side being investigated. Do not infer it blindly from `flow_id` when checking the source side.

## Investigation Rules

- Search `union-archiver-dispatch` first. Do not start with `archiver-manager` unless the route is already known.
- Use `traceId` to enter the chain, then switch to `flow_id` for **union** and **destination** manager stages after the dispatch line is found.
- **Source-area `archiver-manager`:** always search by **`src_uid`** (numeric token and/or `src_uid=<id>`). **Never** use `flow_id` as the sole keyword on the source side. If exhaustive `src_uid` searches (current + `*.log.gz`, all mapped hosts, both log directories) return nothing, treat that as evidence the flow may not have been recorded under that uid on source manager (wrong uid, different hosts/paths, retention gap, or request never reached source manager)—narrow using dispatch time window, other ids from destination logs, or platform owners.
- On the union side, prefer date-scoped compressed-log searches when the event day is roughly known. Filter `Worker.startSyncDispatch()` before parsing the route.
- If you only have `flow_id`, you may start with `build-entry-commands.ps1 -FlowId ...`, but treat the inferred area as a hint until dispatch logs confirm the full route.
- If you only have `flow_id` but also know the event date, use `build-entry-commands.ps1 -FlowId ... -UnionDatePrefix ...` so you can search manager logs and union dispatch logs in one pass.
- After **destination** manager logs are found, read `src_uid` from `param` if you still need source-side greps. After any manager hit on the relevant side, extract `src_uid` (source) and `dst_uid`/`dest_uid` (destination) for ERROR greps.
- When a manager log contains `task_id` and `uid`, also run `build-data-channel-commands.ps1` with the current manager area so data-channel deploy logs and same-host storage-worker logs are checked.
- If UID searches surface `StorageWorker.Export()`, extract `dscid` and continue into storage-worker logs with `area + uid + gid`.
- Keep `references/environment.psd1` updated with `Services['storage-worker'].HostsByAreaDscid` so cache/storage routing can be generated without manual host input.
- Keep `Services['data-channel'].HostsByArea` updated from the data-channel server mapping file when data-channel hosts change.
- Search current `.log` and historical `*.log.gz` files every time.
- If the same service runs on multiple machines, search all mapped hosts for that service or area.
- If multiple dispatch lines match the same `traceId`, prefer the one with the clearest `Worker.startSyncDispatch()` payload and keep the other hits as supporting evidence.
- Generated command payloads can omit `user` to use the tool default. If needed, pass `user` explicitly per target.

## Host Resolution Guardrails

- Reading `references/service-map.md` is only the source step. You must also perform an explicit extraction step: convert `Area -> Hosts` into a concrete host list before calling `ssh_exec`.
- Never call `ssh_exec` with missing host fields. Ensure every call payload contains a non-empty `host` value.
- For `flow_id` in the form `<area>_<suffix>` (for example `4_xxx`), treat the prefix area as a hint and resolve hosts from the area table first.
- If `src_area_type`/`dst_area_type` have been parsed from `Worker.startSyncDispatch()`, those parsed areas override any guess from the `flow_id` prefix.
- If a resolved area has three hosts, execute the same query on all three hosts instead of a single host.
- If area-to-host resolution fails, report `area resolution failed` with the area value and stop the run. Do not emit `ssh_exec` with empty or guessed host fields.

Example (`area=4`) from `references/service-map.md`:

- Area `4` hosts: `10.18.240.104`, `10.18.240.105`, `10.18.240.106`
- Valid `ssh_exec` call shape:

Destination-side example (`flow_id`):

```json
{
  "host": "10.18.240.104",
  "command": "grep -nH -C 2 -- '4_v0cag1d3guo8' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null",
  "strict_host_key_checking": "accept-new"
}
```

Source-side example (`src_uid` only):

```json
{
  "host": "10.79.240.104",
  "command": "grep -nH -C 2 -- '1347353' /data/logs/archiver_manager/archiver-manager.log 2>/dev/null",
  "strict_host_key_checking": "accept-new"
}
```

Also try `src_uid=1347353` (or the form your deployment logs use) in the same files and compressed history.

## Output Format

Return results in this shape:

```text
Dispatch hit:
- host:
- log file:
- traceId:
- flow_id:
- route: <src> -> <dst>

Source area manager:
- area:
- hosts searched:
- src_uid used (not flow_id):
- key findings:

Destination area manager:
- area:
- hosts searched:
- key findings:

UID error search:
- source uid:
- destination uid:
- key error findings:

Data-channel search:
- area:
- task_id:
- uid:
- host:
- key findings:

Storage-worker search:
- dscid:
- uid:
- gid:
- key findings:

Conclusion:
- current status:
- next step:
```

## Common Mistakes

- Treating `dst_area_type` as optional. It is the destination area of record.
- Searching only the current `.log` file and missing the hit in `*.log.gz`.
- Searching only one manager area. Search both source and destination areas; use **`flow_id` on the destination** and **`src_uid` on the source**, not the same keyword for both.
- Confusing the textual guess of a route with the actual route parsed from the log line. The parsed fields win.
- Inferring data-channel area from `flow_id` while reviewing source-side manager logs. Use the actual manager area or pass `-Area` explicitly.
