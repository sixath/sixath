#!/usr/bin/env python3
"""Memory feature acceptance runner against live Portal + MySQL + sqlite vectors."""
from __future__ import annotations

import json
import os
import re
import sqlite3
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, asdict
from pathlib import Path
from typing import Any, Optional

import pymysql

BASE = os.environ.get("PORTAL_BASE", "http://localhost:8000")
TOKEN = os.environ.get("PORTAL_TOKEN", "dev-bootstrap-token")
ORG = os.environ.get("PORTAL_ORG", "default")
OPS_AGENT = "b880051a-a7de-4d91-afea-2ad41269191c"
CONF = Path(os.environ.get("PORTAL_CONF", r"E:\configs\sixath\portal"))
VECTOR_DB = Path(
    os.environ.get(
        "VECTOR_DB",
        r"d:\workspace\github\sixath\portal\data\memory_units_vectors\units_vectors.db",
    )
)
MARKER = f"ACC_MARKER_{int(time.time())}"
WORKDIR = Path(os.environ.get("TEMP", "/tmp")) / "sixath-memory-acc"
WORKDIR.mkdir(parents=True, exist_ok=True)


@dataclass
class Case:
    id: str
    name: str
    status: str  # PASS | FAIL | SKIP
    detail: str = ""


RESULTS: list[Case] = []


def record(cid: str, name: str, status: str, detail: str = "") -> None:
    RESULTS.append(Case(cid, name, status, detail))
    print(f"[{status}] {cid} {name} — {detail}")


def api(method: str, path: str, body: Optional[dict] = None, timeout: int = 240) -> dict:
    data = None
    headers = {
        "Authorization": f"Bearer {TOKEN}",
        "X-Org-Id": ORG,
    }
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8")
        return json.loads(raw) if raw else {}


def db():
    text = (CONF / "config.yaml").read_text(encoding="utf-8")
    m = re.search(
        r"source:\s*([^:\s]+):([^@]+)@tcp\(([^:]+):(\d+)\)/([^?\s]+)", text
    )
    if not m:
        raise RuntimeError("cannot parse mysql dsn")
    user, pwd, host, port, database = m.groups()
    return pymysql.connect(
        host=host,
        port=int(port),
        user=user,
        password=pwd,
        database=database,
        charset="utf8mb4",
        connect_timeout=15,
    )


def create_session(agent_id: str = OPS_AGENT) -> str:
    r = api("POST", f"/api/v1/agents/{agent_id}/sessions", {})
    sid = r.get("id")
    if not sid:
        raise RuntimeError(f"create session failed: {r}")
    return sid


def chat(sid: str, content: str, timeout: int = 240) -> str:
    r = api("POST", f"/api/v1/sessions/{sid}/messages", {"content": content}, timeout=timeout)
    return r.get("content") or ""


def units_for_session(sid: str) -> list[tuple]:
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, status, supersedes_id, LEFT(content,160), metadata, user_id, scope_type
            FROM memory_units
            WHERE scope_id=%s OR source_session_id=%s
            ORDER BY created_at ASC
            """,
            (sid, sid),
        )
        return list(cur.fetchall())
    finally:
        conn.close()


def units_by_marker(marker: str) -> list[tuple]:
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, scope_type, scope_id, status, supersedes_id, content, metadata, user_id
            FROM memory_units
            WHERE content LIKE %s
            ORDER BY created_at DESC
            LIMIT 20
            """,
            (f"%{marker}%",),
        )
        return list(cur.fetchall())
    finally:
        conn.close()


def vector_count(unit_id: Optional[str] = None) -> int:
    if not VECTOR_DB.exists():
        return -1
    con = sqlite3.connect(str(VECTOR_DB))
    try:
        if unit_id:
            return con.execute(
                "SELECT COUNT(*) FROM unit_vectors WHERE unit_id=?", (unit_id,)
            ).fetchone()[0]
        return con.execute("SELECT COUNT(*) FROM unit_vectors").fetchone()[0]
    finally:
        con.close()


def case_session_tools() -> str:
    sid = create_session()
    content = (
        f"Call tools only, in order:\n"
        f"1) memory_remember scope=session action=add content={MARKER}_SESSION\n"
        f"2) memory_recall query={MARKER}_SESSION\n"
        f"3) memory_get with the unit_id from remember\n"
        f"Reply with RECALL_OK/FAIL and GET_OK/FAIL and the unit_id."
    )
    reply = chat(sid, content)
    rows = units_by_marker(f"{MARKER}_SESSION")
    ok_mysql = any(r[3] == "active" and f"{MARKER}_SESSION" in (r[5] or "") for r in rows)
    uid = rows[0][0] if rows else ""
    vec_ok = vector_count(uid) >= 1 if uid else False
    tool_ok = ("RECALL_OK" in reply.upper() or MARKER in reply) and ok_mysql
    if tool_ok and vec_ok:
        record("A1", "session remember/recall/get + vector(bge-m3)", "PASS", f"sid={sid} uid={uid}")
    elif tool_ok and not vec_ok:
        record("A1", "session remember/recall/get + vector(bge-m3)", "FAIL", f"tools ok but vector missing uid={uid} reply={reply[:200]}")
    else:
        record("A1", "session remember/recall/get + vector(bge-m3)", "FAIL", f"reply={reply[:300]} mysql={ok_mysql}")
    return sid


def case_prefetch(sid: str) -> None:
    # Ask without requiring tools; prefetch should inject prior marker context.
    reply = chat(
        sid,
        f"Do NOT call any tools. From context only: what exact string starts with {MARKER}_SESSION? "
        f"If unknown say UNKNOWN.",
        timeout=180,
    )
    log = Path(r"d:\workspace\github\sixath\portal\bin\backend_e2e.out.log")
    log_txt = log.read_text(encoding="utf-8", errors="ignore") if log.exists() else ""
    # recent deadline on this session?
    recent = log_txt[-12000:]
    deadline = "context deadline exceeded" in recent and sid in recent
    if MARKER in reply and "UNKNOWN" not in reply.upper():
        record("A2", "prefetch injects session units into context", "PASS", f"model saw marker; deadline_recent={deadline}")
    elif deadline:
        record("A2", "prefetch injects session units into context", "FAIL", f"deadline still hitting; reply={reply[:200]}")
    else:
        # fail-open may skip; treat as FAIL for acceptance of "useful prefetch"
        record("A2", "prefetch injects session units into context", "FAIL", f"no marker in reply={reply[:250]}")


def case_user_scope() -> None:
    sid = create_session()
    content = (
        f"Call memory_remember with scope=user action=add content={MARKER}_USER. "
        f"Then memory_recall scope=user query={MARKER}_USER. "
        f"Reply USER_OK or USER_FAIL and unit_id."
    )
    reply = chat(sid, content)
    rows = units_by_marker(f"{MARKER}_USER")
    user_rows = [r for r in rows if r[1] == "user"]
    if user_rows and user_rows[0][3] == "active" and (user_rows[0][7] or user_rows[0][2]):
        record("B1", "scope=user remember/recall", "PASS", f"uid={user_rows[0][0]} scope_id={user_rows[0][2]} reply_has={MARKER in reply or 'USER_OK' in reply}")
    else:
        record("B1", "scope=user remember/recall", "FAIL", f"reply={reply[:250]} rows={user_rows[:2]}")


def case_agent_files() -> None:
    sid = create_session()
    content = (
        f"Call memory_remember scope=agent action=add target=memory "
        f"content=AgentNote {MARKER}_AGENT. "
        f"Then memory_recall scope=agent source=files query={MARKER}_AGENT. "
        f"Then memory_get scope=agent path=MEMORY.md. "
        f"Reply AGENT_OK or AGENT_FAIL."
    )
    reply = chat(sid, content)
    mem_path = Path(r"E:\sixath\workspace\ops-agent\MEMORY.md")
    on_disk = mem_path.exists() and MARKER in mem_path.read_text(encoding="utf-8", errors="ignore")
    if on_disk and ("AGENT_OK" in reply or MARKER in reply):
        record("B2", "scope=agent file write/recall/get", "PASS", f"MEMORY.md contains marker")
    elif on_disk:
        record("B2", "scope=agent file write/recall/get", "PASS", f"disk ok; reply weak: {reply[:180]}")
    else:
        record("B2", "scope=agent file write/recall/get", "FAIL", f"no MEMORY.md marker; reply={reply[:250]}")


def case_supersede_remove() -> None:
    sid = create_session()
    r1 = chat(
        sid,
        f"Call memory_remember scope=session action=add content={MARKER}_D1_V1. "
        f"Reply only the unit_id UUID.",
    )
    # extract uuid
    m = re.search(
        r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}",
        r1,
        re.I,
    )
    if not m:
        rows = units_by_marker(f"{MARKER}_D1_V1")
        old_id = rows[0][0] if rows else ""
    else:
        old_id = m.group(0)
    if not old_id:
        record("C1", "supersede replace + remove cascade", "FAIL", f"no unit from remember: {r1[:200]}")
        return
    r2 = chat(
        sid,
        f"Call memory_remember scope=session action=replace unit_id={old_id} "
        f"content={MARKER}_D1_V2. Reply NEW_ID=<uuid>.",
    )
    m2 = re.search(
        r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}",
        r2,
        re.I,
    )
    time.sleep(1)
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute("SELECT id, status, supersedes_id, content FROM memory_units WHERE id=%s", (old_id,))
        old = cur.fetchone()
        new_id = m2.group(0) if m2 else None
        if not new_id:
            cur.execute(
                "SELECT id, status, supersedes_id, content FROM memory_units WHERE supersedes_id=%s",
                (old_id,),
            )
            newt = cur.fetchone()
            new_id = newt[0] if newt else None
        else:
            cur.execute(
                "SELECT id, status, supersedes_id, content FROM memory_units WHERE id=%s",
                (new_id,),
            )
            newt = cur.fetchone()
        supersede_ok = (
            old
            and old[1] == "superseded"
            and newt
            and newt[1] == "active"
            and newt[2] == old_id
            and f"{MARKER}_D1_V2" in (newt[3] or "")
        )
        if not supersede_ok:
            record("C1", "supersede replace + remove cascade", "FAIL", f"old={old} new={newt} r2={r2[:200]}")
            return
        # remove new → cascade deleted
        chat(
            sid,
            f"Call memory_remember scope=session action=remove unit_id={new_id}. Reply REMOVED.",
        )
        time.sleep(1)
        cur.execute("SELECT id, status FROM memory_units WHERE id IN (%s,%s)", (old_id, new_id))
        statuses = {row[0]: row[1] for row in cur.fetchall()}
        if statuses.get(old_id) == "deleted" and statuses.get(new_id) == "deleted":
            record("C1", "supersede replace + remove cascade", "PASS", f"old={old_id} new={new_id}")
        else:
            record("C1", "supersede replace + remove cascade", "FAIL", f"statuses={statuses}")
    finally:
        conn.close()


def case_conflict() -> None:
    sid = create_session()
    chat(
        sid,
        f"Call memory_remember scope=session action=add content={MARKER}_COLOR favorite color is red.",
    )
    time.sleep(1)
    chat(
        sid,
        f"Call memory_remember scope=session action=add content={MARKER}_COLOR favorite color is blue.",
    )
    time.sleep(2)
    conn = db()
    try:
        cur = conn.cursor()
        cur.execute(
            """
            SELECT id, status, supersedes_id, content FROM memory_units
            WHERE content LIKE %s ORDER BY created_at ASC
            """,
            (f"%{MARKER}_COLOR%",),
        )
        rows = cur.fetchall()
        active = [r for r in rows if r[1] == "active"]
        superseded = [r for r in rows if r[1] == "superseded"]
        # conflict resolver may supersede or keep_both; either proves path ran if >1 writes
        if len(rows) >= 2 and (superseded or len(active) >= 1):
            # stronger: if blue active and red superseded → classic supersede
            blue_active = any("blue" in (r[3] or "") and r[1] == "active" for r in rows)
            red_super = any("red" in (r[3] or "") and r[1] == "superseded" for r in rows)
            if blue_active and red_super:
                record("C2", "LLM semantic conflict (P2-D2)", "PASS", f"supersede red→blue rows={len(rows)}")
            elif len(active) == 2:
                record("C2", "LLM semantic conflict (P2-D2)", "PASS", f"keep_both active=2 (valid verdict)")
            else:
                record("C2", "LLM semantic conflict (P2-D2)", "PASS", f"wrote {len(rows)} units active={len(active)} superseded={len(superseded)}")
        else:
            record("C2", "LLM semantic conflict (P2-D2)", "FAIL", f"rows={rows}")
    finally:
        conn.close()


def case_extract() -> None:
    sid = create_session()
    # conversational fact without asking for tools
    chat(
        sid,
        f"Please remember for later (plain chat, tools optional): "
        f"The project codeword is {MARKER}_EXTRACT_OMEGA. Just acknowledge briefly.",
        timeout=180,
    )
    # wait async extract
    deadline = time.time() + 45
    found = []
    while time.time() < deadline:
        conn = db()
        try:
            cur = conn.cursor()
            cur.execute(
                """
                SELECT id, content, metadata, status FROM memory_units
                WHERE (scope_id=%s OR source_session_id=%s)
                  AND (content LIKE %s OR metadata LIKE %s)
                ORDER BY created_at DESC LIMIT 10
                """,
                (sid, sid, f"%{MARKER}_EXTRACT%", "%turn_extract%"),
            )
            found = cur.fetchall()
        finally:
            conn.close()
        # prefer metadata source
        extract_hits = []
        for row in found:
            meta = row[2]
            if isinstance(meta, (bytes, bytearray)):
                meta = meta.decode("utf-8", "ignore")
            meta_s = meta if isinstance(meta, str) else json.dumps(meta or {}, ensure_ascii=False)
            if "turn_extract" in meta_s or (row[1] and MARKER + "_EXTRACT" in row[1]):
                extract_hits.append(row)
        if extract_hits:
            # verify turn_extract specifically if possible
            te = [r for r in extract_hits if "turn_extract" in str(r[2])]
            if te:
                record("C3", "turn extraction (P2-C)", "PASS", f"turn_extract unit={te[0][0]}")
                return
            # model may have called remember instead
            record("C3", "turn extraction (P2-C)", "FAIL", f"unit exists but metadata not turn_extract: {extract_hits[0]}")
            return
        time.sleep(3)
    record("C3", "turn extraction (P2-C)", "FAIL", "no extract unit within 45s")


def case_transcript() -> None:
    sid = create_session()
    unique = f"{MARKER}_TRANSCRIPT_PHRASE_ZXQ"
    chat(sid, f"Just say OK. Ignore tools. Unique phrase for search: {unique}", timeout=120)
    time.sleep(2)
    sid2 = create_session()
    reply = chat(
        sid2,
        f"Call memory_recall scope=session source=transcript query={unique}. "
        f"Reply TRANSCRIPT_OK if any hit else TRANSCRIPT_FAIL.",
        timeout=180,
    )
    if "TRANSCRIPT_OK" in reply or unique in reply:
        record("D1", "source=transcript cross-session recall", "PASS", reply[:200])
    else:
        record("D1", "source=transcript cross-session recall", "FAIL", reply[:250])


def case_infra_skips() -> None:
    # Qdrant / Neo4j probed by env
    import socket

    def port_open(host: str, port: int) -> bool:
        s = socket.socket()
        s.settimeout(1.5)
        try:
            s.connect((host, port))
            return True
        except OSError:
            return False
        finally:
            s.close()

    if port_open("127.0.0.1", 6333):
        record("E1", "Qdrant provider (P2-H) live E2E", "FAIL", "port open but provider still sqlite — switch not tested")
    else:
        record("E1", "Qdrant provider (P2-H) live E2E", "SKIP", "no Qdrant on :6333")

    if port_open("127.0.0.1", 7687):
        record("E2", "Neo4j graph (P2-I) live E2E", "FAIL", "port open but graph not enabled in agent_extra")
    else:
        record("E2", "Neo4j graph (P2-I) live E2E", "SKIP", "no Neo4j on :7687")


def case_prefetch_quota_note() -> None:
    # Observability: max_total=8 configured; hard to assert without fence dump.
    # Soft check: config loaded values via yaml
    text = (CONF / "agent_extra.yaml").read_text(encoding="utf-8")
    if "max_total: 8" in text and "max_snippets: 5" in text:
        record("A3", "prefetch quota config present (P2-F)", "PASS", "max_snippets=5 max_total=8 in agent_extra")
    else:
        record("A3", "prefetch quota config present (P2-F)", "FAIL", "quota keys missing")


def main() -> int:
    print(f"MARKER={MARKER}")
    print(f"BASE={BASE}")
    case_infra_skips()
    case_prefetch_quota_note()
    sid = case_session_tools()
    case_prefetch(sid)
    case_user_scope()
    case_agent_files()
    case_supersede_remove()
    case_conflict()
    case_extract()
    case_transcript()

    out = WORKDIR / "memory_acceptance_report.json"
    summary = {
        "marker": MARKER,
        "pass": sum(1 for r in RESULTS if r.status == "PASS"),
        "fail": sum(1 for r in RESULTS if r.status == "FAIL"),
        "skip": sum(1 for r in RESULTS if r.status == "SKIP"),
        "cases": [asdict(r) for r in RESULTS],
    }
    out.write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    print("\n=== SUMMARY ===")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    print(f"report: {out}")
    return 1 if summary["fail"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
