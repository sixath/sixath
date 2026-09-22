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
    "SELECT metadata FROM chat_messages WHERE session_id=%s AND role=%s",
    (sid, "assistant"),
)
(meta,) = cur.fetchone()
if isinstance(meta, (bytes, bytearray)):
    meta = meta.decode()
data = json.loads(meta)
for ev in data.get("timeline") or []:
    if ev.get("kind") != "tool":
        continue
    name = ev.get("toolName")
    if name not in ("run_result_script", "result_stats", "es_log_query", "read_file"):
        continue
    res = ev.get("result")
    if isinstance(res, str):
        try:
            parsed = json.loads(res)
        except Exception:
            parsed = None
            preview = res[:500]
        else:
            preview = json.dumps(parsed, ensure_ascii=False)[:800]
    else:
        parsed = res
        preview = json.dumps(res, ensure_ascii=False)[:800] if res is not None else ""
    print("=" * 60)
    print("step", ev.get("step"), name, "trunc", ev.get("truncated"))
    print(preview)
conn.close()
