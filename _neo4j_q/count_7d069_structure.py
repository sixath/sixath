#!/usr/bin/env python3
import json
import pymysql

sid = "7d069a05-0699-42ea-a3af-2407e0a1aa20"
conn = pymysql.connect(
    host="172.26.50.161",
    port=23306,
    user="viu_root",
    password="myviu_4359",
    database="sath",
    charset="utf8mb4",
)
cur = conn.cursor()
cur.execute(
    "SELECT id, role, CHAR_LENGTH(content), CHAR_LENGTH(metadata), metadata FROM chat_messages WHERE session_id=%s ORDER BY created_at",
    (sid,),
)
for mid, role, clen, mlen, meta in cur.fetchall():
    print(f"id={mid} role={role} content_len={clen} metadata_chars={mlen}")
    if not meta:
        print("  no metadata")
        continue
    if isinstance(meta, (bytes, bytearray)):
        meta = meta.decode("utf-8")
    data = json.loads(meta) if isinstance(meta, str) else meta
    print("  keys", list(data.keys()) if isinstance(data, dict) else type(data))
    tl = data.get("timeline") if isinstance(data, dict) else None
    if not isinstance(tl, list):
        print("  no timeline")
        continue
    print("  timeline_len", len(tl))
    kinds = {}
    phases = {}
    dropped = 0
    for ev in tl:
        if not isinstance(ev, dict):
            dropped += 1
            continue
        k = ev.get("kind") or "?"
        kinds[k] = kinds.get(k, 0) + 1
        ph = ev.get("phase") or "?"
        phases[f"{k}:{ph}"] = phases.get(f"{k}:{ph}", 0) + 1
    print("  kinds", kinds)
    print("  phases", phases)
    print("  non-dict", dropped)
    # sample last 5
    print("  last5", [{k: ev.get(k) for k in ("kind", "phase", "toolName", "tool_name", "id", "step")} for ev in tl[-5:]])
    print("  first5", [{k: ev.get(k) for k in ("kind", "phase", "toolName", "tool_name", "id", "step")} for ev in tl[:5]])

print("=== memory_units schema ===")
cur.execute("DESCRIBE memory_units")
for r in cur.fetchall():
    print(r)
cur.execute("SHOW COLUMNS FROM memory_units")
# try scope
try:
    cur.execute("SELECT COUNT(*) FROM memory_units WHERE scope_id=%s", (sid,))
    print("memory_units by scope_id", cur.fetchone())
except Exception as e:
    print("scope_id", e)

conn.close()
