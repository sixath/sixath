#!/usr/bin/env python3
import json
import re
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
    "SELECT content, metadata FROM chat_messages WHERE session_id=%s AND role=%s",
    (sid, "assistant"),
)
content, meta = cur.fetchone()
if isinstance(meta, (bytes, bytearray)):
    meta = meta.decode()
data = json.loads(meta) if isinstance(meta, str) else (meta or {})
print("content_len", len(content))
print("claim", re.findall(r"（(\d+)\s*条）", content))
print("468 mentions", content.count("468"))
print("370 mentions", content.count("370"))
print("355 mentions", content.count("355"))
sep_re = re.compile(r"^\|[\s:|-]+\|$")
rows = []
for ln in content.splitlines():
    t = ln.strip()
    if t.startswith("|") and t.endswith("|") and len(t) > 1 and not sep_re.match(t):
        rows.append(t)
print("pipe_rows_incl_header", len(rows))
print("first_pipe", rows[:3])
print("last_pipe", rows[-3:])
print("tail", repr(content[-500:]))
print("head")
print(content[:1200])
tl = data.get("timeline") or []
print("timeline_events", len(tl))
print("meta_keys", list(data.keys()) if isinstance(data, dict) else type(data))
for ev in tl:
    if not isinstance(ev, dict):
        continue
    k = ev.get("kind")
    name = ev.get("toolName") or ev.get("name")
    phase = ev.get("phase")
    trunc = ev.get("truncated")
    res = ev.get("result")
    if isinstance(res, str):
        rlen = len(res)
    elif res is not None:
        rlen = len(json.dumps(res, ensure_ascii=False))
    else:
        rlen = 0
    print(
        "kind=%s tool=%s phase=%s trunc=%s result_len=%s step=%s"
        % (k, name, phase, trunc, rlen, ev.get("step"))
    )
conn.close()
