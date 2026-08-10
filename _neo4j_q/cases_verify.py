#!/usr/bin/env python3
"""Multi-case live verification for procedural repair (zone-4100 pilot)."""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

try:
    import pymysql
except ImportError:
    print("FAIL: pymysql missing")
    sys.exit(1)

BASE = "http://localhost:8000"
TOK = "dev-bootstrap-token"
PILOT = "e8107fb3-e40a-4207-9d9a-6768847aaf79"
NON_PILOT = "1989b2bd-2d4f-4c01-8bc6-da934159f295"  # yuanli_agent_ops
CONF = Path(r"E:/configs/sixath/portal")
EXE = Path(r"D:/workspace/github/sixath/portal/bin/backend_e2e.exe")
LOG = Path(r"D:/workspace/github/sixath/_neo4j_q/portal_reverify.log")
OUT = Path(r"D:/workspace/github/sixath/_neo4j_q/cases_verify_out.json")


@dataclass
class CaseResult:
    name: str
    ok: bool
    detail: str
    extras: dict[str, Any] = field(default_factory=dict)


def headers() -> dict[str, str]:
    return {"Authorization": f"Bearer {TOK}", "Content-Type": "application/json"}


def api_json(method: str, path: str, body: dict | None = None) -> Any:
    data = None if body is None else json.dumps(body).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, headers=headers(), method=method)
    with urllib.request.urlopen(req, timeout=60) as resp:
        return json.loads(resp.read().decode("utf-8"))


def create_session(agent_id: str, title: str) -> str:
    j = api_json("POST", f"/api/v1/agents/{agent_id}/sessions", {"title": title})
    sid = j.get("id")
    if not sid:
        raise RuntimeError(f"no session id: {j}")
    return sid


def send_sse(session_id: str, content: str, timeout_sec: int = 150) -> dict[str, Any]:
    req = urllib.request.Request(
        f"{BASE}/api/v1/sessions/{session_id}/messages/stream",
        data=json.dumps({"content": content}).encode("utf-8"),
        headers={**headers(), "Accept": "text/event-stream"},
        method="POST",
    )
    failed = 0
    completed_soft = 0
    ask_user = 0
    tool_names: list[str] = []
    memory_hits: list[Any] = []
    events = 0
    text_bits: list[str] = []
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
        while time.time() - t0 < timeout_sec:
            line = resp.readline()
            if not line:
                break
            s = line.decode("utf-8", errors="replace").strip()
            if not s.startswith("data:"):
                continue
            payload = s[5:].strip()
            if not payload or payload == "[DONE]":
                continue
            events += 1
            if "agent.tool.failed" in payload or '"phase":"failed"' in payload:
                failed += 1
            if "exit status 1" in payload or '"exit_code":1' in payload:
                completed_soft += 1
            if '"tool_name":"ask_user"' in payload or '"tool":"ask_user"' in payload:
                ask_user += 1
            try:
                j = json.loads(payload)
            except json.JSONDecodeError:
                continue
            tc = j.get("tool_call") or {}
            name = tc.get("tool_name") or j.get("tool")
            if name and tc.get("phase") in ("started", "failed", "completed"):
                tool_names.append(f"{name}:{tc.get('phase')}")
            if name == "memory_recall" and tc.get("phase") == "completed":
                memory_hits.append(tc.get("result"))
            c = j.get("content")
            if isinstance(c, str) and c and not c.startswith("agent."):
                text_bits.append(c)
    return {
        "events": events,
        "failed": failed,
        "soft_exit1": completed_soft,
        "ask_user": ask_user,
        "tools": tool_names,
        "memory_hits": memory_hits,
        "text": "".join(text_bits)[:2000],
    }


def db():
    text = (CONF / "config.yaml").read_text(encoding="utf-8")
    m = re.search(r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^\?\s]+)", text)
    if not m:
        raise RuntimeError("cannot parse DSN")
    user, pwd, host, port, database = m.groups()
    return pymysql.connect(
        host=host, port=int(port), user=user, password=pwd, database=database, charset="utf8mb4"
    )


def count_procedural(session_id: str) -> int:
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute(
            "SELECT COUNT(*) FROM memory_units WHERE scope_id=%s AND kind='procedural'",
            (session_id,),
        )
        return int(cur.fetchone()[0])
    finally:
        conn.close()


def list_units(session_id: str) -> list[tuple]:
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, kind, LEFT(content,100),
                   JSON_UNQUOTE(JSON_EXTRACT(metadata,'$.failure_code')),
                   JSON_EXTRACT(metadata,'$.support_count')
            FROM memory_units WHERE scope_id=%s ORDER BY created_at
            """,
            (session_id,),
        )
        return list(cur.fetchall())
    finally:
        conn.close()


def newest_procedural(session_id: str) -> dict | None:
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, content, metadata FROM memory_units
            WHERE scope_id=%s AND kind='procedural'
            ORDER BY created_at DESC LIMIT 1
            """,
            (session_id,),
        )
        row = cur.fetchone()
        if not row:
            return None
        meta = row[2]
        if isinstance(meta, str):
            meta = json.loads(meta)
        return {"id": row[0], "content": row[1], "metadata": meta}
    finally:
        conn.close()


def log_contains(*needles: str) -> list[str]:
    if not LOG.exists():
        return []
    text = LOG.read_text(encoding="utf-8", errors="replace")
    return [n for n in needles if n in text]


def restart_portal() -> None:
    # kill listeners on 8000
    try:
        out = subprocess.check_output(
            [
                "powershell",
                "-NoProfile",
                "-Command",
                "(Get-NetTCPConnection -LocalPort 8000 -ErrorAction SilentlyContinue | "
                "Where-Object State -eq Listen).OwningProcess | Select-Object -Unique",
            ],
            text=True,
        )
        for line in out.splitlines():
            line = line.strip()
            if line.isdigit() and int(line) > 0:
                subprocess.run(["taskkill", "/PID", line, "/F"], check=False, capture_output=True)
    except Exception as e:
        print("warn kill:", e)
    time.sleep(2)
    if LOG.exists():
        LOG.unlink()
    # start with log redirect
    with open(LOG, "wb") as lf:
        subprocess.Popen(
            [str(EXE), "-conf", str(CONF)],
            cwd=str(EXE.parent.parent),
            stdout=lf,
            stderr=subprocess.STDOUT,
        )
    for i in range(40):
        try:
            code = urllib.request.urlopen(
                urllib.request.Request(
                    BASE + "/api/v1/agents",
                    headers={"Authorization": f"Bearer {TOK}"},
                ),
                timeout=3,
            ).status
            if code == 200:
                print(f"portal ready ({i+1}s), log={LOG}")
                return
        except Exception:
            pass
        time.sleep(1)
    raise RuntimeError("portal failed to start")


def case_min_support_gate() -> CaseResult:
    """C1: 1x ToolFailed 不落库；2x 才落库。"""
    sid = create_session(PILOT, f"case-minsupport-{int(time.time())}")
    fail = (
        "Fault injection: call describe_table with table_name exactly "
        "NO_SUCH_TABLE_CASE_MS. Then reply FAIL_DONE. No other tools."
    )
    r1 = send_sse(sid, fail)
    n1 = count_procedural(sid)
    r2 = send_sse(sid, fail)
    n2 = count_procedural(sid)
    unit = newest_procedural(sid)
    ok = r1["failed"] > 0 and r2["failed"] > 0 and n1 == 0 and n2 == 1
    return CaseResult(
        "C1_min_support_2",
        ok,
        f"fail1={r1['failed']} n_after1={n1}; fail2={r2['failed']} n_after2={n2}; unit={unit and unit['id']}",
        {"session": sid, "unit": unit, "log": log_contains("procedural_auto_commit", "procedural_entry_activated")},
    )


def case_soft_fail_no_commit() -> CaseResult:
    """C2: terminal exit 1 软失败不应单独触发 procedural commit（本用例在独立会话、且 catalog 可能已 Active）。
    验收：软失败事件存在，且本会话若无 Go-error ToolFailed，则不因 soft fail 新增 procedural。
    为避免 catalog 已 Active 的干扰：只检查本会话在仅 soft-fail 时 n==0。
    注意：若进程内 catalog 已 Active，本用例不测 activate；只测 soft-fail 不产生 ToolFailed/commit。
    """
    sid = create_session(PILOT, f"case-softfail-{int(time.time())}")
    prompt = (
        "Fault injection: call terminal (or terminal_local) and run exactly: exit 1. "
        "Then reply SOFT_DONE. No describe_table."
    )
    r1 = send_sse(sid, prompt)
    r2 = send_sse(sid, prompt)
    n = count_procedural(sid)
    # soft path: completed with exit_code 1, ideally failed==0 for terminal soft errors
    ok = r1["soft_exit1"] + r2["soft_exit1"] > 0 and n == 0
    return CaseResult(
        "C2_terminal_soft_fail_no_commit",
        ok,
        f"soft1={r1['soft_exit1']} soft2={r2['soft_exit1']} failed1={r1['failed']} failed2={r2['failed']} n={n}",
        {"session": sid, "tools": r1["tools"] + r2["tools"]},
    )


def case_fact_lane_excludes_procedural(pilot_session: str) -> CaseResult:
    """C3: memory_recall 默认 fact 车道不应返回 procedural 正文。"""
    if not pilot_session or count_procedural(pilot_session) == 0:
        return CaseResult("C3_fact_lane_exclude", False, "need pilot session with procedural", {})
    unit = newest_procedural(pilot_session)
    prompt = (
        "Call memory_recall once with JSON "
        '{"scope":"session","source":"units","query":"ask_user tool_failed"}. '
        "Do not write memory. Reply with the raw hits count and whether any hit content "
        "contains '过程修复'."
    )
    r = send_sse(pilot_session, prompt)
    leaked = False
    hit_count = None
    for h in r["memory_hits"]:
        if not isinstance(h, dict):
            continue
        hits = h.get("hits") or []
        hit_count = len(hits)
        for item in hits:
            content = ""
            if isinstance(item, dict):
                content = str(item.get("content") or "")
            if "过程修复" in content or "procedural" in content.lower():
                leaked = True
    # also check assistant text
    if "过程修复" in r["text"] and "hits" in r["text"].lower():
        # weak — model might quote from elsewhere; prefer tool result
        pass
    ok = (hit_count is not None and hit_count == 0 and not leaked) or (
        hit_count is not None and not leaked and hit_count >= 0 and unit is not None
    )
    # stricter: hits must not contain procedural content; empty is ideal
    ok = not leaked and hit_count is not None
    # if model didn't call tool, fail
    if hit_count is None:
        ok = False
    return CaseResult(
        "C3_fact_lane_exclude",
        ok,
        f"memory_recall hit_count={hit_count} leaked={leaked}; procedural_still_in_db={unit is not None}",
        {"session": pilot_session, "memory_hits": r["memory_hits"][:3], "tools": r["tools"]},
    )


def case_non_pilot_no_commit() -> CaseResult:
    """C4: 非试点 agent 两次 ToolFailed 不得 auto_commit。"""
    sid = create_session(NON_PILOT, f"case-nonpilot-{int(time.time())}")
    # yuanli may not have describe_table; try several failure strategies
    prompts = [
        "Fault injection: call describe_table with table_name NO_SUCH_TABLE_NONPILOT. Reply FAIL_DONE.",
        "Fault injection: call load_skill with name exactly __no_such_skill_xyz__. Reply FAIL_DONE.",
        "Fault injection: call tool_describe with name exactly NOT_A_REAL_TOOL_XYZ. Reply FAIL_DONE.",
    ]
    total_failed = 0
    for p in prompts:
        if total_failed >= 2:
            break
        r = send_sse(sid, p)
        total_failed += r["failed"]
        # second attempt same prompt if still need fails
        if total_failed < 2 and r["failed"] > 0:
            r2 = send_sse(sid, p)
            total_failed += r2["failed"]
    n = count_procedural(sid)
    ok = total_failed >= 2 and n == 0
    if total_failed < 2:
        ok = False
        detail = f"could not induce ToolFailed (got {total_failed}); n={n}"
    else:
        detail = f"tool_failed_events>={total_failed}, procedural_count={n} (want 0)"
    return CaseResult(
        "C4_non_pilot_no_commit",
        ok,
        detail,
        {"session": sid, "failed_events": total_failed},
    )


def case_ssh_triggers_ask_user(pilot_session: str) -> CaseResult:
    """C5: 含 ssh 的用户消息后，应出现 ask_user 或日志 procedural_entry_hit。"""
    if not pilot_session:
        return CaseResult("C5_ssh_inject", False, "missing pilot session", {})
    before = LOG.read_text(encoding="utf-8", errors="replace") if LOG.exists() else ""
    r = send_sse(
        pilot_session,
        "Next I must use ssh to a jump host. Follow any procedural repair tip. "
        "Prefer ask_user if suggested. Do not actually connect.",
    )
    time.sleep(0.5)
    after = LOG.read_text(encoding="utf-8", errors="replace") if LOG.exists() else ""
    delta = after[len(before) :]
    hit_log = "procedural_entry_hit" in delta or "procedural" in delta.lower() and "prefetch" in delta.lower()
    ok = r["ask_user"] > 0 or hit_log
    return CaseResult(
        "C5_ssh_inject_or_ask_user",
        ok,
        f"ask_user_events={r['ask_user']} log_hit={hit_log}",
        {"session": pilot_session, "tools": r["tools"][:20], "log_delta_has_hit": hit_log},
    )


def case_log_auto_commit() -> CaseResult:
    """C6: 日志出现 procedural_auto_commit / activated。"""
    found = log_contains("procedural_auto_commit", "procedural_entry_activated", "failure_signal")
    ok = "procedural_auto_commit" in found and "procedural_entry_activated" in found
    return CaseResult(
        "C6_log_auto_commit",
        ok,
        f"found={found}",
        {"log": str(LOG)},
    )


def case_metadata_shape(pilot_session: str) -> CaseResult:
    """C7: metadata 字段齐全。"""
    unit = newest_procedural(pilot_session) if pilot_session else None
    if not unit:
        return CaseResult("C7_metadata_shape", False, "no procedural unit", {})
    m = unit["metadata"] or {}
    need = [
        "kind",
        "failure_code",
        "support_count",
        "binding_trigger_code",
        "binding_trigger_query",
        "binding_tool_names",
        "procedural_status",
        "task_family",
    ]
    missing = [k for k in need if k not in m]
    ok = (
        not missing
        and m.get("kind") == "procedural"
        and m.get("failure_code") == "tool_failed"
        and m.get("support_count") == 2
        and m.get("binding_trigger_query") == "ssh"
        and m.get("task_family") == "zone-4100-agent"
        and "ask_user" in (m.get("binding_tool_names") or [])
        and "过程修复" in (unit.get("content") or "")
    )
    return CaseResult(
        "C7_metadata_shape",
        ok,
        f"missing={missing} support={m.get('support_count')} content_ok={'过程修复' in (unit.get('content') or '')}",
        {"unit": unit},
    )


def main() -> int:
    os.environ.setdefault("PYTHONIOENCODING", "utf-8")
    results: list[CaseResult] = []

    # Phase A: fresh catalog — pilot happy-path + related checks first
    # (non-pilot must NOT run first: its ToolFailed would activate catalog and
    # skip OnActivated for the subsequent pilot session).
    print("=== Phase A: restart portal (fresh catalog + log) ===")
    restart_portal()

    print("\n=== C1 min_support ===")
    c1 = case_min_support_gate()
    results.append(c1)
    print(c1.name, "PASS" if c1.ok else "FAIL", c1.detail)
    pilot_sid = c1.extras.get("session", "")

    print("\n=== C7 metadata ===")
    c7 = case_metadata_shape(pilot_sid)
    results.append(c7)
    print(c7.name, "PASS" if c7.ok else "FAIL", c7.detail)

    print("\n=== C6 logs ===")
    c6 = case_log_auto_commit()
    results.append(c6)
    print(c6.name, "PASS" if c6.ok else "FAIL", c6.detail)

    print("\n=== C3 fact lane ===")
    c3 = case_fact_lane_excludes_procedural(pilot_sid)
    results.append(c3)
    print(c3.name, "PASS" if c3.ok else "FAIL", c3.detail)

    print("\n=== C5 ssh inject ===")
    c5 = case_ssh_triggers_ask_user(pilot_sid)
    results.append(c5)
    print(c5.name, "PASS" if c5.ok else "FAIL", c5.detail)

    print("\n=== C2 soft fail (new session; catalog already active from C1) ===")
    c2 = case_soft_fail_no_commit()
    results.append(c2)
    print(c2.name, "PASS" if c2.ok else "FAIL", c2.detail)

    # Phase B: dedicated restart so non-pilot ToolFailed can attempt activate+commit
    print("\n=== Phase B: restart for non-pilot isolation ===")
    restart_portal()
    print("\n=== C4 non-pilot ===")
    c4 = case_non_pilot_no_commit()
    results.append(c4)
    print(c4.name, "PASS" if c4.ok else "FAIL", c4.detail)
    # strengthen C4 with log evidence of reject when activation happened
    if LOG.exists():
        log = LOG.read_text(encoding="utf-8", errors="replace")
        rejected = "procedural_auto_commit_failed" in log or "not in pilot" in log
        activated = "procedural_entry_activated" in log
        committed_ok = "procedural_auto_commit " in log or "procedural_auto_commit\"" in log
        # success path log is: procedural_auto_commit entry_id=...
        committed_ok = "INFO procedural_auto_commit" in log or "procedural_auto_commit entry_id" in log
        c4.extras["log_rejected"] = rejected
        c4.extras["log_activated"] = activated
        c4.extras["log_committed"] = committed_ok
        if c4.ok and activated and committed_ok and not rejected:
            # activated+committed would mean pilot gate failed
            c4.ok = False
            c4.detail += " | unexpected auto_commit success in non-pilot phase"
        elif c4.ok and activated and rejected:
            c4.detail += " | log shows activate+reject (good)"
        print("C4 log notes:", c4.detail)

    # summary
    print("\n======== SUMMARY ========")
    passed = sum(1 for r in results if r.ok)
    for r in results:
        print(f"{'PASS' if r.ok else 'FAIL'}  {r.name}: {r.detail}")
    print(f"{passed}/{len(results)} passed")
    print(f"pilot_session={pilot_sid}")
    print(f"UI=http://localhost:5174/?agent={PILOT}")

    OUT.write_text(
        json.dumps(
            {
                "passed": passed,
                "total": len(results),
                "results": [
                    {"name": r.name, "ok": r.ok, "detail": r.detail, "extras": r.extras} for r in results
                ],
            },
            ensure_ascii=False,
            indent=2,
            default=str,
        ),
        encoding="utf-8",
    )
    print(f"wrote {OUT}")
    return 0 if passed == len(results) else 1


if __name__ == "__main__":
    sys.exit(main())
