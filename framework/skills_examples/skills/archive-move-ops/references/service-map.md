# Service Map

This file documents the machine and log layout used by `archive-move-ops`.

## SSH Access

- Default SSH user: `vrviu`
- Current scripts generate commands as `ssh vrviu@<ip> ...`
- Password is not stored in the skill. Enter it interactively when prompted unless your environment already has key-based login configured.

## Investigation Entry Point

### `union-archiver-dispatch`

- Hosts:
  - `10.19.240.104`
  - `10.19.240.105`
  - `10.19.240.106`
- Log directory:
  - `/data/union/logs/union_archiver_dispatch/`
- File rules:
  - Current log: `union-archiver-dispatch.log`
  - History logs: `union-archiver-dispatch-*.log.gz`

Use this service as the first search target when the user provides a `traceId`.

## Area-Scoped Service

### `archiver-manager`

- Log directories:
  - `/data/logs/archiver_manager/`
  - `/data/area/logs/archiver_manager/`
- File rules:
  - Current log: `archiver-manager.log`
  - History logs: `archiver-manager-*.log.gz`

Search this service only after extracting `flow_id`, `src_area_type`, and `dst_area_type` from the dispatch line.

## Storage Worker Routing

`StorageWorker.Export()` lines contain `uid`, `gid`, and `dscid`.

Use `area + dscid` to choose the storage-worker/cache host, then search storage-worker logs with both `uid` and `gid`.

The area+dscid-to-host mapping lives in `references/environment.psd1` under:

```powershell
Services['storage-worker'].HostsByAreaDscid
```

The mapping is sourced from `D:\各区域的cache服务器.xlsx` (columns `areaType`, `id`, `ip`). The table below mirrors that file for quick lookup; canonical copy for scripts is still `environment.psd1`.

| Area (`areaType`) | DSCID (`id`) | Cache host (`ip`) |
| --- | --- | --- |
| `2` | `1` | `172.30.101.240` |
| `2` | `2` | `172.30.102.240` |
| `2` | `4` | `172.30.104.240` |
| `2` | `7` | `172.30.107.240` |
| `2` | `11` | `172.30.111.240` |
| `2` | `13` | `172.30.113.240` |
| `2` | `15` | `172.30.115.240` |
| `2` | `16` | `172.30.116.240` |
| `2` | `17` | `172.30.117.240` |
| `2` | `18` | `172.30.118.240` |
| `2` | `19` | `172.30.119.240` |
| `4` | `1` | `10.18.101.240` |
| `4` | `2` | `10.18.102.240` |
| `4` | `10` | `10.18.110.240` |
| `10` | `1` | `10.76.240.149` |
| `10` | `2` | `10.76.240.150` |
| `20` | `1` | `10.79.240.149` |
| `20` | `2` | `10.79.240.150` |
| `100` | `1` | `172.26.66.240` |
| `100` | `2` | `172.26.67.240` |
| `100` | `4` | `172.26.69.240` |
| `100` | `5` | `172.26.70.240` |
| `101` | `1` | `172.24.101.240` |
| `101` | `2` | `172.24.102.240` |
| `101` | `3` | `172.24.103.240` |
| `101` | `4` | `172.24.104.240` |
| `101` | `6` | `172.24.106.240` |
| `101` | `8` | `172.24.108.240` |
| `101` | `12` | `172.24.112.240` |
| `101` | `13` | `172.24.113.240` |
| `101` | `14` | `172.24.114.240` |
| `101` | `15` | `172.24.115.240` |
| `101` | `16` | `172.24.116.240` |
| `200` | `1` | `172.28.101.240` |
| `200` | `8` | `172.28.108.240` |
| `200` | `10` | `172.28.110.240` |
| `200` | `15` | `172.28.115.240` |
| `200` | `16` | `172.28.116.240` |
| `200` | `17` | `172.28.117.240` |
| `201` | `1` | `172.29.101.240` |
| `201` | `2` | `172.29.102.240` |
| `201` | `3` | `172.29.103.240` |
| `201` | `4` | `172.29.104.240` |
| `201` | `6` | `172.29.106.240` |
| `201` | `8` | `172.29.108.240` |
| `201` | `10` | `172.29.110.240` |
| `201` | `12` | `172.29.112.240` |
| `201` | `16` | `172.29.116.240` |
| `201` | `17` | `172.29.117.240` |
| `300` | `1` | `10.10.101.240` |
| `300` | `2` | `10.10.102.240` |
| `300` | `3` | `10.10.103.240` |
| `300` | `4` | `10.10.104.240` |
| `300` | `8` | `10.10.108.240` |
| `300` | `10` | `10.10.110.240` |
| `300` | `12` | `10.10.112.240` |
| `300` | `14` | `10.10.114.240` |
| `300` | `15` | `10.10.115.240` |
| `300` | `16` | `10.10.116.240` |
| `301` | `1` | `10.11.101.240` |
| `301` | `2` | `10.11.102.240` |
| `301` | `3` | `10.11.103.240` |
| `301` | `4` | `10.11.104.240` |
| `301` | `8` | `10.11.108.240` |
| `301` | `10` | `10.11.110.240` |
| `400` | `1` | `10.77.240.149` |
| `400` | `2` | `10.77.240.150` |
| `500` | `1` | `10.78.240.149` |
| `500` | `2` | `10.78.240.150` |

Pass `-StorageHosts` to `scripts/build-storage-worker-commands.ps1` only for ad hoc checks when a mapping is missing.

## Data Channel Routing

Use this after an `archiver-manager` hit exposes both `task_id` and `uid`.

- `task_id` log targets:
  - `/opt/deploy_agent/log/deploy_agent*.log*`
  - `/opt/deploy_server/log/deploy_server*.log*`
- Same-host `uid` log target:
  - `/data/storage_worker/logs/storage-worker*.log*`
- The data-channel area mapping lives in `references/environment.psd1` under:

```powershell
Services['data-channel'].HostsByArea
```

The mapping is sourced from `D:\workspace\golang\demos\各区域的data-channel服务器.xlsx`.

| Area | Data-channel Host | Server ID |
| --- | --- | --- |
| `2` | `172.30.240.64` | `2000` |
| `4` | `10.18.240.64` | `2000` |
| `10` | `10.76.240.64` | `2000` |
| `20` | `10.79.240.64` | `2000` |
| `100` | `172.26.240.64` | `23` |
| `101` | `172.24.240.64` | `24` |
| `200` | `172.28.240.64` | `2000` |
| `201` | `172.29.240.64` | `2000` |
| `300` | `10.10.240.64` | `2000` |
| `301` | `10.11.240.64` | `2000` |
| `400` | `10.77.240.64` | `2000` |
| `500` | `10.78.240.64` | `2000` |

## Area to Machine Mapping

| Area | Hosts |
| --- | --- |
| `2` | `172.30.240.104`, `172.30.240.105`, `172.30.240.106` |
| `4` | `10.18.240.104`, `10.18.240.105`, `10.18.240.106` |
| `10` | `10.76.240.104`, `10.76.240.105`, `10.76.240.106` |
| `20` | `10.79.240.104`, `10.79.240.105`, `10.79.240.106` |
| `100` | `172.26.240.104`, `172.26.240.105`, `172.26.240.106` |
| `101` | `172.24.240.104`, `172.24.240.105`, `172.24.240.106` |
| `200` | `172.29.240.104`, `172.29.240.105`, `172.29.240.106` |
| `201` | `172.28.240.104`, `172.28.240.105`, `172.28.240.106` |
| `300` | `10.10.240.104`, `10.10.240.105`, `10.10.240.106` |
| `301` | `10.11.240.104`, `10.11.240.105`, `10.11.240.106` |
| `400` | `10.77.240.104`, `10.77.240.105`, `10.77.240.106` |
| `500` | `10.78.240.104`, `10.78.240.105`, `10.78.240.106` |

## Example Route

Dispatch log:

```text
Worker.startSyncDispatch(). flow_id(301_rqkkw0snhnmt) uuid(154880308) ugid(1189) src_area_type(400) dst_area_type(301) done_union_version(27)
```

Interpretation:

- `flow_id`: `301_rqkkw0snhnmt`
- Source area: `400`
- Destination area: `301`
- Route: `400 -> 301`

Next action:

1. Search `union-archiver-dispatch` with the original `traceId`.
2. Parse the matching dispatch line.
3. Search `archiver-manager`: destination area `301` with `flow_id=301_rqkkw0snhnmt`; source area `400` with `src_uid` from the destination hit (do not use `flow_id` on the source side).
4. If you already have the dispatch line, run `scripts/build-followup-commands.ps1` to generate the manager search commands directly.
5. If you have a local dispatch log text file plus `traceId`, run `scripts/build-investigation-report.ps1` to generate a full investigation report template.
