import json
import sys
from collections import Counter

import pymysql

sys.stdout.reconfigure(encoding="utf-8")

SID = "2fcc2c95-3b00-4f94-a721-8beaaff843fa"
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
    "SELECT id, role, content, metadata, created_at FROM chat_messages WHERE session_id=%s ORDER BY created_at",
    (SID,),
)
for mid, role, content, meta, created in cur.fetchall():
    if isinstance(meta, (bytes, bytearray)):
        meta = meta.decode("utf-8")
    data = json.loads(meta) if isinstance(meta, str) else (meta or {})
    tl = data.get("timeline") if isinstance(data, dict) else None
    tools = Counter()
    n = 0
    if isinstance(tl, list):
        n = len(tl)
        for ev in tl:
            if isinstance(ev, dict) and ev.get("kind") == "tool":
                tools[str(ev.get("toolName"))] += 1
    print(f"\n==== {role} {created} clen={len(content or '')} tl={n} tools={dict(tools)}")
    print((content or "")[:400].replace("\n", " | "))
    if role == "assistant" and isinstance(tl, list) and n <= 8:
        for ev in tl:
            if not isinstance(ev, dict):
                continue
            print(" ", ev.get("kind"), ev.get("phase"), ev.get("toolName"), ev.get("error"), "keys", list(ev.keys())[:10])
